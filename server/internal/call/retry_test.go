package call

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// memStore is an in-memory Store: these tests need no database.
type memStore struct {
	mu       sync.Mutex
	phone    string
	statuses []string // calls.status by call id - 1
	convs    []string
}

func (s *memStore) Begin(_ context.Context, incidentID string) (int64, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if incidentID != sampleIncident {
		return 0, "", ErrUnknownIncident
	}
	s.statuses = append(s.statuses, StatusRequested)
	s.convs = append(s.convs, "")
	return int64(len(s.statuses)), s.phone, nil
}

func (s *memStore) Placed(_ context.Context, id int64, p Placed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.convs[id-1] = p.ConversationID
	if s.statuses[id-1] == StatusRequested {
		s.statuses[id-1] = StatusInProgress
	}
	return nil
}

func (s *memStore) Failed(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.statuses[id-1]; st == StatusRequested || st == StatusInProgress {
		s.statuses[id-1] = StatusFailed
	}
	return nil
}

func (s *memStore) FailedConversation(_ context.Context, conv, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.convs {
		if c == conv {
			s.statuses[i] = StatusFailed
		}
	}
	return nil
}

func (s *memStore) SaveTranscript(context.Context, contract.CallWebhook) error { return nil }

func (s *memStore) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.statuses...)
}

// scriptPlacer answers each Place with the next error (nil = placed as
// conv_<n> / CA<n>), and runs onPlace after a successful one.
type scriptPlacer struct {
	mu      sync.Mutex
	errs    []error
	calls   int
	onPlace func(n int, p Placed)
}

func (p *scriptPlacer) Place(context.Context, string, string) (Placed, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	var err error
	if n <= len(p.errs) {
		err = p.errs[n-1]
	}
	p.mu.Unlock()
	if err != nil {
		return Placed{}, err
	}
	placed := Placed{ConversationID: fmt.Sprintf("conv_%d", n), CallSID: fmt.Sprintf("CA%d", n)}
	if p.onPlace != nil {
		p.onPlace(n, placed)
	}
	return placed, nil
}

// scriptStatus answers Status for each call SID from its list, one entry
// per poll, repeating the last; each poll advances the fake clock by step.
type scriptStatus struct {
	mu    sync.Mutex
	bySID map[string][]string
	clock *fakeClock
	step  time.Duration
}

func (s *scriptStatus) Status(_ context.Context, sid string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clock != nil {
		s.clock.advance(s.step)
	}
	list := s.bySID[sid]
	if len(list) == 0 {
		return "", errors.New("unknown call")
	}
	st := list[0]
	if len(list) > 1 {
		s.bySID[sid] = list[1:]
	}
	return st, nil
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// captureLog sends the standard logger to a buffer for the test.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return buf
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// timingLine returns the one "call timing:" line in out.
func timingLine(t *testing.T, out string) string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "call timing: ") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("want one call timing line, got %d:\n%s", len(lines), out)
	}
	return lines[0]
}

func testEngine(store Store, placer Placer, status StatusReader, clock *fakeClock) *Engine {
	e := &Engine{
		Store: store, Placer: placer, Status: status,
		Fallback:       NewFallback("agent_test1", "https://mama.example"),
		OutcomeTimeout: 2 * time.Second,
		PollEvery:      time.Millisecond,
	}
	if clock != nil {
		e.Now = clock.now
	}
	return e
}

func TestForcedFailureRetriesOnceThenOpensTheFallback(t *testing.T) {
	out := captureLog(t)
	store := &memStore{phone: victimPhone}
	placer := &scriptPlacer{errs: []error{errors.New("boom"), errors.New("boom")}}
	e := testEngine(store, placer, nil, nil)

	e.StartCall(context.Background(), sampleIncidentInput())

	if placer.calls != 2 {
		t.Errorf("Place calls = %d, want 2 (one retry)", placer.calls)
	}
	if got := store.snapshot(); len(got) != 2 || got[0] != StatusFailed || got[1] != StatusFailed {
		t.Errorf("calls = %v, want two failed rows", got)
	}
	if p := e.Fallback.pending(); len(p) != 1 || p[0].ID != sampleIncident {
		t.Errorf("fallback pending = %+v, want the incident", p)
	}
	if !strings.Contains(out.String(), "open https://mama.example/call/fallback") {
		t.Errorf("log does not point at the fallback page:\n%s", out)
	}
	line := timingLine(t, out.String())
	for _, want := range []string{"incident=" + sampleIncident, "outcome=fallback", "attempts=2", "event_to_ring_ms=-"} {
		if !strings.Contains(line, want) {
			t.Errorf("timing line %q lacks %q", line, want)
		}
	}
}

func TestRetryThatRingsDoesNotOpenTheFallback(t *testing.T) {
	out := captureLog(t)
	clock := &fakeClock{t: time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC)}
	store := &memStore{phone: victimPhone}
	placer := &scriptPlacer{errs: []error{errors.New("boom")}}
	// Each poll is 1 s on the fake clock: queued, initiated, ringing (3 s),
	// in-progress.
	status := &scriptStatus{clock: clock, step: time.Second, bySID: map[string][]string{
		"CA2": {TwilioQueued, TwilioInitiated, TwilioRinging, TwilioInProgress},
	}}
	e := testEngine(store, placer, status, clock)

	e.StartCall(context.Background(), sampleIncidentInput())

	if placer.calls != 2 {
		t.Errorf("Place calls = %d, want 2", placer.calls)
	}
	if got := store.snapshot(); len(got) != 2 || got[0] != StatusFailed || got[1] != StatusInProgress {
		t.Errorf("calls = %v, want [failed in_progress]", got)
	}
	if p := e.Fallback.pending(); len(p) != 0 {
		t.Errorf("fallback opened for %+v", p)
	}
	line := timingLine(t, out.String())
	for _, want := range []string{"outcome=connected", "attempts=2", "event_to_ring_ms=3000", "ring_within_15s=true"} {
		if !strings.Contains(line, want) {
			t.Errorf("timing line %q lacks %q", line, want)
		}
	}
}

func TestEventToRingTimeIsLoggedForEveryIncident(t *testing.T) {
	cases := []struct {
		name     string
		polls    []string
		triggerS int // triggered_at, seconds before the server got the alert
		want     []string
	}{
		{"fast ring", []string{TwilioRinging, TwilioInProgress}, 1,
			[]string{"outcome=connected", "attempts=1", "event_to_ring_ms=2000", "trigger_to_ring_ms=3000", "ring_within_15s=true"}},
		{"slow ring", append(repeat(TwilioQueued, 19), TwilioRinging, TwilioCompleted), 0,
			[]string{"event_to_ring_ms=21000", "ring_within_15s=false"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureLog(t)
			start := time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC)
			clock := &fakeClock{t: start}
			// Placing takes 1 s on the fake clock; each poll 1 s more.
			placer := &scriptPlacer{onPlace: func(int, Placed) { clock.advance(time.Second) }}
			status := &scriptStatus{clock: clock, step: time.Second, bySID: map[string][]string{"CA1": tc.polls}}
			e := testEngine(&memStore{phone: victimPhone}, placer, status, clock)
			inc := sampleIncidentInput()
			inc.TriggeredAt = start.Add(-time.Duration(tc.triggerS) * time.Second)

			e.StartCall(context.Background(), inc)

			line := timingLine(t, out.String())
			for _, want := range append(tc.want, "event_to_placed_ms=1000") {
				if !strings.Contains(line, want) {
					t.Errorf("timing line %q lacks %q", line, want)
				}
			}
		})
	}
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestUnansweredCallsRetryThenFallBack(t *testing.T) {
	for _, final := range []string{TwilioNoAnswer, TwilioBusy, TwilioFailed, TwilioCanceled} {
		t.Run(final, func(t *testing.T) {
			out := captureLog(t)
			store := &memStore{phone: victimPhone}
			placer := &scriptPlacer{}
			status := &scriptStatus{bySID: map[string][]string{
				"CA1": {TwilioRinging, final},
				"CA2": {TwilioRinging, final},
			}}
			e := testEngine(store, placer, status, nil)

			e.StartCall(context.Background(), sampleIncidentInput())

			if placer.calls != 2 {
				t.Errorf("Place calls = %d, want 2", placer.calls)
			}
			if got := store.snapshot(); len(got) != 2 || got[0] != StatusFailed || got[1] != StatusFailed {
				t.Errorf("calls = %v, want two failed rows", got)
			}
			if len(e.Fallback.pending()) != 1 {
				t.Error("fallback not opened")
			}
			// It rang, so the ring time is still reported.
			line := timingLine(t, out.String())
			if !strings.Contains(line, "outcome=fallback") || strings.Contains(line, "event_to_ring_ms=-") {
				t.Errorf("timing line %q, want a fallback with a ring time", line)
			}
		})
	}
}

// initiationFailure is a call_initiation_failure webhook for conv.
func initiationFailure(conv string) contract.CallWebhook {
	var hook contract.CallWebhook
	hook.Type = TypeCallInitiationFailure
	hook.Data.ConversationID = conv
	hook.Data.ClientData.DynamicVariables = map[string]any{contract.DynamicVarIncidentID: sampleIncident}
	return hook
}

func TestInitiationFailureWebhookTriggersTheRetry(t *testing.T) {
	captureLog(t)
	store := &memStore{phone: victimPhone}
	var e *Engine
	placer := &scriptPlacer{}
	placer.onPlace = func(n int, p Placed) {
		if n == 1 {
			// ElevenLabs reports the first call busy, even before it
			// answered the outbound-call request.
			go func() { _ = e.InitiationFailed(context.Background(), initiationFailure(p.ConversationID)) }()
		}
	}
	// No Twilio status: only the webhooks tell the outcome. The second call
	// ends with a transcript.
	e = testEngine(store, placer, nil, nil)
	e.OutcomeTimeout = 5 * time.Second
	done := make(chan struct{})
	go func() {
		e.StartCall(context.Background(), sampleIncidentInput())
		close(done)
	}()
	waitFor(t, func() bool { return placer.count() == 2 && e.waitingOn(sampleIncident) })
	var hook contract.CallWebhook
	hook.Type = TypeTranscription
	hook.Data.ConversationID = "conv_2"
	hook.Data.ClientData.DynamicVariables = map[string]any{contract.DynamicVarIncidentID: sampleIncident}
	if err := e.Received(context.Background(), hook); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartCall did not end after the transcript webhook")
	}
	if got := store.snapshot(); len(got) != 2 || got[0] != StatusFailed || got[1] != StatusInProgress {
		t.Errorf("calls = %v, want [failed in_progress]", got)
	}
	if len(e.Fallback.pending()) != 0 {
		t.Error("fallback opened although the retry connected")
	}
}

func TestLateFailureOfAnEarlierCallIsIgnored(t *testing.T) {
	captureLog(t)
	store := &memStore{phone: victimPhone}
	var e *Engine
	placer := &scriptPlacer{errs: []error{errors.New("boom")}}
	placer.onPlace = func(int, Placed) {
		// A stale failure for another conversation arrives during call 2.
		go func() { _ = e.InitiationFailed(context.Background(), initiationFailure("conv_other")) }()
	}
	status := &scriptStatus{bySID: map[string][]string{"CA2": {TwilioQueued, TwilioQueued, TwilioQueued, TwilioRinging, TwilioInProgress}}}
	e = testEngine(store, placer, status, nil)
	e.PollEvery = 20 * time.Millisecond

	e.StartCall(context.Background(), sampleIncidentInput())

	if len(e.Fallback.pending()) != 0 || placer.calls != 2 {
		t.Errorf("calls = %d, fallback = %+v; want the second call to connect", placer.calls, e.Fallback.pending())
	}
}

func TestPostCallWebhookForARungOutCallStillRetries(t *testing.T) {
	captureLog(t)
	store := &memStore{phone: victimPhone}
	var e *Engine
	placer := &scriptPlacer{}
	placer.onPlace = func(n int, p Placed) {
		if n == 1 {
			// The post-call webhook comes before a poll sees no-answer.
			go func() {
				var hook contract.CallWebhook
				hook.Type = TypeTranscription
				hook.Data.ConversationID = p.ConversationID
				hook.Data.ClientData.DynamicVariables = map[string]any{contract.DynamicVarIncidentID: sampleIncident}
				_ = e.Received(context.Background(), hook)
			}()
		}
	}
	status := &scriptStatus{bySID: map[string][]string{
		"CA1": {TwilioNoAnswer},
		"CA2": {TwilioRinging, TwilioInProgress},
	}}
	e = testEngine(store, placer, status, nil)
	e.PollEvery = time.Hour // only the webhook path reads Twilio

	done := make(chan struct{})
	go func() { e.StartCall(context.Background(), sampleIncidentInput()); close(done) }()
	// Attempt 2 ends with a webhook while Twilio says ringing: connected.
	waitFor(t, func() bool { return placer.count() == 2 && e.waitingOn(sampleIncident) })
	var hook contract.CallWebhook
	hook.Type = TypeTranscription
	hook.Data.ConversationID = "conv_2"
	hook.Data.ClientData.DynamicVariables = map[string]any{contract.DynamicVarIncidentID: sampleIncident}
	_ = e.Received(context.Background(), hook)
	<-done

	if got := store.snapshot(); len(got) != 2 || got[0] != StatusFailed {
		t.Errorf("calls = %v, want the rung-out call failed and a retry", got)
	}
	if len(e.Fallback.pending()) != 0 {
		t.Error("fallback opened although the retry connected")
	}
}

func TestNoOutcomeIsNotRetried(t *testing.T) {
	out := captureLog(t)
	placer := &scriptPlacer{}
	e := testEngine(&memStore{phone: victimPhone}, placer, nil, nil)
	e.OutcomeTimeout = 10 * time.Millisecond

	e.StartCall(context.Background(), sampleIncidentInput())

	if placer.calls != 1 {
		t.Errorf("Place calls = %d, want 1: a call that may be live is not placed again", placer.calls)
	}
	if line := timingLine(t, out.String()); !strings.Contains(line, "outcome=unconfirmed") || !strings.Contains(line, "ring_within_15s=unknown") {
		t.Errorf("timing line %q, want an unconfirmed outcome and an unknown ring", line)
	}
}

func TestUnknownIncidentLogsNotCalled(t *testing.T) {
	out := captureLog(t)
	placer := &scriptPlacer{}
	e := testEngine(&memStore{phone: victimPhone}, placer, nil, nil)

	e.StartCall(context.Background(), intake.Incident{ID: "00000000-0000-0000-0000-000000000000", Branch: contract.BranchCall})

	if placer.calls != 0 || len(e.Fallback.pending()) != 0 {
		t.Errorf("calls = %d, fallback = %+v; want nothing", placer.calls, e.Fallback.pending())
	}
	if line := timingLine(t, out.String()); !strings.Contains(line, "outcome=not_called") {
		t.Errorf("timing line %q", line)
	}
}

func TestCallForceFailureEnvOpensTheFallback(t *testing.T) {
	out := captureLog(t)
	cfg := config.LoadFrom(func(k string) string {
		return map[string]string{
			"CALL_FORCE_FAILURE":  "true",
			"ELEVENLABS_AGENT_ID": "agent_stage1",
			"RENDER_EXTERNAL_URL": "https://mama.onrender.com",
		}[k]
	})
	store := &memStore{phone: victimPhone}
	rt := NewRoutes(cfg, store)
	rt.engine.RetryDelay = 0
	mux := http.NewServeMux()
	rt.Register(mux)

	rt.Starter().StartCall(context.Background(), sampleIncidentInput())

	if got := store.snapshot(); len(got) != 2 || got[0] != StatusFailed || got[1] != StatusFailed {
		t.Errorf("calls = %v, want two failed rows", got)
	}
	if !strings.Contains(out.String(), "forced failure (CALL_FORCE_FAILURE)") ||
		!strings.Contains(out.String(), "open https://mama.onrender.com/call/fallback") {
		t.Errorf("log:\n%s", out)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, FallbackPath, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `agent-id="agent_stage1"`) || !strings.Contains(rec.Body.String(), sampleIncident) {
		t.Errorf("fallback page %d:\n%s", rec.Code, rec.Body)
	}
}

func TestFallbackPageShowsTheAgentOnlyWhileAnIncidentIsOpen(t *testing.T) {
	captureLog(t)
	clock := &fakeClock{t: time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC)}
	f := NewFallback("agent_stage1", "")
	f.now = clock.now
	get := func() string {
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, FallbackPath, nil))
		return rec.Body.String()
	}

	if body := get(); strings.Contains(body, "agent_stage1") || strings.Contains(body, "convai") || !strings.Contains(body, `http-equiv="refresh"`) {
		t.Errorf("idle page exposes the agent or does not reload:\n%s", body)
	}
	f.Open(sampleIncident, 2)
	body := get()
	if !strings.Contains(body, `agent-id="agent_stage1"`) || !strings.Contains(body, sampleIncident) || strings.Contains(body, `http-equiv="refresh"`) {
		t.Errorf("open page:\n%s", body)
	}
	// One open incident: the browser call carries its incident_id.
	if want := `dynamic-variables="{&#34;incident_id&#34;:&#34;` + sampleIncident + `&#34;}"`; !strings.Contains(body, want) {
		t.Errorf("open page lacks %s:\n%s", want, body)
	}
	f.Open("1b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", 2)
	if body := get(); strings.Contains(body, "dynamic-variables") {
		t.Errorf("two open incidents: the widget must not pick one:\n%s", body)
	}
	clock.advance(fallbackWindow + time.Second)
	if body := get(); strings.Contains(body, "agent_stage1") {
		t.Errorf("page still open after the window:\n%s", body)
	}
}

func TestFallbackPageRefusesAMalformedAgentID(t *testing.T) {
	captureLog(t)
	f := NewFallback(`agent_x"><script>`, "")
	f.Open(sampleIncident, 2)
	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, FallbackPath, nil))
	if body := rec.Body.String(); strings.Contains(body, "<script>alert") || strings.Contains(body, "convai") || !strings.Contains(body, "ELEVENLABS_AGENT_ID") {
		t.Errorf("page:\n%s", body)
	}
}

func TestTwilioStatusReadsTheCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodGet || r.URL.Path != "/2010-04-01/Accounts/AC123/Calls/CA9.json" || !ok || user != "AC123" || pass != "tok" {
			t.Errorf("request %s %s auth %q %q %v", r.Method, r.URL.Path, user, pass, ok)
		}
		_, _ = w.Write([]byte(`{"sid":"CA9","status":"ringing"}`))
	}))
	t.Cleanup(srv.Close)
	ts := &TwilioStatus{BaseURL: srv.URL, AccountSID: "AC123", AuthToken: "tok"}

	got, err := ts.Status(context.Background(), "CA9")
	if err != nil || got != TwilioRinging {
		t.Errorf("Status = %q, %v", got, err)
	}
}

func TestTwilioStatusReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":20404}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	ts := &TwilioStatus{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "t"}
	if _, err := ts.Status(context.Background(), "CA1"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want a 404 error", err)
	}
}

// waitingOn reports whether an attempt of the incident waits for webhooks.
func (e *Engine) waitingOn(incidentID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.waiting[incidentID] != nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}

func (p *scriptPlacer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
