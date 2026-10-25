package call

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHanger records the calls it was asked to hang up.
type fakeHanger struct {
	mu   sync.Mutex
	sids []string
	err  error
}

func (h *fakeHanger) Hangup(_ context.Context, sid string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sids = append(h.sids, sid)
	return h.err
}

func (h *fakeHanger) hung() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.sids...)
}

func TestCancelWhileRingingHangsUpAndDoesNotRetryOrFallBack(t *testing.T) {
	out := captureLog(t)
	store := &memStore{phone: victimPhone, sids: []string{"CA1"}}
	placer := &scriptPlacer{}
	// The phone keeps ringing: only the cancel ends the attempt.
	status := &scriptStatus{bySID: map[string][]string{"CA1": {TwilioRinging}}}
	hanger := &fakeHanger{}
	e := testEngine(store, placer, status, nil)
	e.Hangup = hanger
	e.OutcomeTimeout = 5 * time.Second

	done := make(chan struct{})
	go func() {
		e.StartCall(context.Background(), sampleIncidentInput())
		close(done)
	}()
	waitFor(t, func() bool { return placer.count() == 1 && e.waitingOn(sampleIncident) })
	e.CancelCall(context.Background(), sampleIncident)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartCall did not end after the cancel")
	}

	if placer.count() != 1 {
		t.Errorf("Place calls = %d, want 1 (no retry)", placer.count())
	}
	// Both the cancel and the attempt may hang up the call when they cross;
	// Twilio ignores the second, so more than one is fine.
	if got := hanger.hung(); len(got) == 0 || got[0] != "CA1" || got[len(got)-1] != "CA1" {
		t.Errorf("hung up %v, want CA1", got)
	}
	if len(e.Fallback.pending()) != 0 {
		t.Error("fallback opened for a cancelled incident")
	}
	if line := timingLine(t, out.String()); !strings.Contains(line, "outcome=canceled") {
		t.Errorf("timing line %q lacks outcome=canceled", line)
	}
}

func TestCancelBeforeTheCallPlacesNothing(t *testing.T) {
	out := captureLog(t)
	store := &memStore{phone: victimPhone}
	placer := &scriptPlacer{}
	e := testEngine(store, placer, nil, nil)

	e.CancelCall(context.Background(), sampleIncident)
	e.StartCall(context.Background(), sampleIncidentInput())

	if placer.count() != 0 || len(store.snapshot()) != 0 {
		t.Errorf("placed %d calls and stored %d rows for a cancelled incident", placer.count(), len(store.snapshot()))
	}
	if len(e.Fallback.pending()) != 0 {
		t.Error("fallback opened for a cancelled incident")
	}
	if line := timingLine(t, out.String()); !strings.Contains(line, "outcome=canceled") {
		t.Errorf("timing line %q lacks outcome=canceled", line)
	}
}

// A cancel that arrives while the call is being placed finds no CallSID to
// hang up; the engine hangs the fresh call up itself.
func TestCancelWhileTheCallIsBeingPlacedHangsItUp(t *testing.T) {
	captureLog(t)
	store := &memStore{phone: victimPhone}
	hanger := &fakeHanger{}
	placer := &scriptPlacer{}
	var e *Engine
	placer.onPlace = func(int, Placed) { e.CancelCall(context.Background(), sampleIncident) }
	e = testEngine(store, placer, nil, nil)
	e.Hangup = hanger

	e.StartCall(context.Background(), sampleIncidentInput())

	if got := hanger.hung(); len(got) != 1 || got[0] != "CA1" {
		t.Errorf("hung up %v, want [CA1]", got)
	}
	if placer.count() != 1 || len(e.Fallback.pending()) != 0 {
		t.Errorf("Place calls = %d, fallback = %+v; want one call and no fallback", placer.count(), e.Fallback.pending())
	}
}

func TestCancelWithoutTwilioStopsRetriesAndSaysSo(t *testing.T) {
	out := captureLog(t)
	store := &memStore{phone: victimPhone, sids: []string{"CA1"}}
	e := testEngine(store, &scriptPlacer{}, nil, nil)

	e.CancelCall(context.Background(), sampleIncident)

	if !e.isCancelled(sampleIncident) {
		t.Error("incident not marked cancelled")
	}
	if !strings.Contains(out.String(), "no Twilio account is set to hang up call CA1") {
		t.Errorf("log does not say the hang-up was skipped:\n%s", out)
	}
}

func TestCancelKeepsGoingWhenAHangupFails(t *testing.T) {
	captureLog(t)
	store := &memStore{phone: victimPhone, sids: []string{"CA1", "CA2"}}
	hanger := &fakeHanger{err: errors.New("twilio down")}
	e := testEngine(store, &scriptPlacer{}, nil, nil)
	e.Hangup = hanger

	e.CancelCall(context.Background(), sampleIncident)

	if got := hanger.hung(); len(got) != 2 {
		t.Errorf("hung up %v, want both calls tried", got)
	}
}

// twilioCalls serves the call resource: GET answers status, POST records the
// form it got.
func twilioCalls(t *testing.T, status string, posted *[]string) *TwilioStatus {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2010-04-01/Accounts/AC1/Calls/CA9.json" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
				t.Errorf("Content-Type = %q", ct)
			}
			*posted = append(*posted, string(raw))
		}
		_, _ = w.Write([]byte(`{"sid":"CA9","status":"` + status + `"}`))
	}))
	t.Cleanup(srv.Close)
	return &TwilioStatus{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "t"}
}

func TestTwilioHangupCancelsARingingCallAndCompletesALiveOne(t *testing.T) {
	for _, c := range []struct{ status, want string }{
		{TwilioQueued, "Status=canceled"},
		{TwilioRinging, "Status=canceled"},
		{TwilioInProgress, "Status=completed"},
	} {
		var posted []string
		if err := twilioCalls(t, c.status, &posted).Hangup(context.Background(), "CA9"); err != nil {
			t.Fatalf("%s: %v", c.status, err)
		}
		if len(posted) != 1 || posted[0] != c.want {
			t.Errorf("%s: posted %v, want [%s]", c.status, posted, c.want)
		}
	}
}

func TestTwilioHangupOfAnEndedCallSendsNothing(t *testing.T) {
	for _, status := range []string{TwilioCompleted, TwilioBusy, TwilioNoAnswer, TwilioFailed, TwilioCanceled} {
		var posted []string
		if err := twilioCalls(t, status, &posted).Hangup(context.Background(), "CA9"); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if len(posted) != 0 {
			t.Errorf("%s: posted %v, want nothing", status, posted)
		}
	}
}

func TestTwilioHangupReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Error(w, `{"code":21220}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ringing"}`))
	}))
	t.Cleanup(srv.Close)
	ts := &TwilioStatus{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "t"}
	if err := ts.Hangup(context.Background(), "CA9"); err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want a 400 error", err)
	}
}

// On the real database: a cancelled incident gets no new call row, and
// ActiveCallSIDs lists only the calls a hang-up can still reach.
func TestDBStoreCancelledIncidentGetsNoCallAndListsActiveSIDs(t *testing.T) {
	pool := callDB(t, victimPhone)
	store := NewDBStore(pool)
	ctx := context.Background()

	live, _, err := store.Begin(ctx, sampleIncident)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Placed(ctx, live, Placed{ConversationID: "conv_a", CallSID: "CAa"}); err != nil {
		t.Fatal(err)
	}
	over, _, err := store.Begin(ctx, sampleIncident)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Placed(ctx, over, Placed{ConversationID: "conv_b", CallSID: "CAb"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Failed(ctx, over); err != nil {
		t.Fatal(err)
	}

	sids, err := store.ActiveCallSIDs(ctx, sampleIncident)
	if err != nil || len(sids) != 1 || sids[0] != "CAa" {
		t.Fatalf("ActiveCallSIDs = %v, %v; want [CAa]", sids, err)
	}

	if _, err := pool.ExecContext(ctx, `UPDATE incidents SET cancelled_at = now() WHERE id = $1`, sampleIncident); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Begin(ctx, sampleIncident); !errors.Is(err, ErrUnknownIncident) {
		t.Errorf("Begin on a cancelled incident = %v, want ErrUnknownIncident", err)
	}
	if rows := callRows(t, pool); len(rows) != 2 {
		t.Errorf("call rows = %d, want the 2 from before the cancel", len(rows))
	}
}
