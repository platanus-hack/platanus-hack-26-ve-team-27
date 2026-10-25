package call

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// recordTranscriber records each Transcribe call and answers err.
type recordTranscriber struct {
	mu  sync.Mutex
	got []string // "callSID callbackURL"
	err error
}

func (r *recordTranscriber) Transcribe(_ context.Context, callSID, callbackURL string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, callSID+" "+callbackURL)
	return r.err
}

const testTranscriptionURL = "https://mama.example/webhooks/twilio/transcription"

// #143: the live transcription starts once, when Twilio says the call was
// answered, not while it rings.
func TestAnsweredCallStartsTheLiveTranscription(t *testing.T) {
	captureLog(t)
	status := &scriptStatus{bySID: map[string][]string{"CA1": {TwilioQueued, TwilioRinging, TwilioRinging, TwilioInProgress}}}
	e := testEngine(&memStore{phone: victimPhone}, &scriptPlacer{}, status, nil)
	tr := &recordTranscriber{}
	e.Transcriber, e.TranscriptionURL = tr, testTranscriptionURL

	e.StartCall(context.Background(), sampleIncidentInput())

	if want := "CA1 " + testTranscriptionURL; len(tr.got) != 1 || tr.got[0] != want {
		t.Errorf("Transcribe calls = %q, want [%q]", tr.got, want)
	}
}

// A call that rings out, or one with the transcription off, starts none.
func TestNoLiveTranscriptionWithoutAnAnswerOrTheURL(t *testing.T) {
	captureLog(t)
	status := &scriptStatus{bySID: map[string][]string{"CA1": {TwilioRinging, TwilioNoAnswer}, "CA2": {TwilioRinging, TwilioNoAnswer}}}
	e := testEngine(&memStore{phone: victimPhone}, &scriptPlacer{}, status, nil)
	tr := &recordTranscriber{}
	e.Transcriber, e.TranscriptionURL = tr, testTranscriptionURL
	e.StartCall(context.Background(), sampleIncidentInput())

	off := testEngine(&memStore{phone: victimPhone}, &scriptPlacer{}, &scriptStatus{bySID: map[string][]string{"CA1": {TwilioInProgress}}}, nil)
	off.Transcriber = tr // no TranscriptionURL
	off.StartCall(context.Background(), sampleIncidentInput())

	if len(tr.got) != 0 {
		t.Errorf("Transcribe calls = %q, want none", tr.got)
	}
}

// Twilio refusing the transcription (the call's audio forks are taken, a
// bad language) only logs: the call is connected and not placed again.
func TestFailedLiveTranscriptionLeavesTheCallAlone(t *testing.T) {
	out := captureLog(t)
	store := &memStore{phone: victimPhone}
	placer := &scriptPlacer{}
	status := &scriptStatus{bySID: map[string][]string{"CA1": {TwilioRinging, TwilioInProgress}}}
	e := testEngine(store, placer, status, nil)
	e.Transcriber, e.TranscriptionURL = &recordTranscriber{err: errors.New("twilio: 400 Bad Request")}, testTranscriptionURL

	e.StartCall(context.Background(), sampleIncidentInput())

	if placer.count() != 1 || strings.Join(store.snapshot(), " ") != StatusInProgress {
		t.Errorf("calls placed %d, statuses %v; want one call, still in progress", placer.count(), store.snapshot())
	}
	if line := timingLine(t, out.String()); !strings.Contains(line, "outcome=connected") {
		t.Errorf("timing line %q, want outcome=connected", line)
	}
	if !strings.Contains(out.String(), "live transcription not started: twilio: 400 Bad Request") ||
		!strings.Contains(out.String(), "step=live_transcription level=WARN") {
		t.Errorf("log:\n%s", out.String())
	}
}

func TestTwilioTranscribeStartsBothTracksInSpanish(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodPost || r.URL.Path != "/2010-04-01/Accounts/AC123/Calls/CA9/Transcriptions.json" ||
			!ok || user != "AC123" || pass != "tok" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("request %s %s auth %q %q %v type %q", r.Method, r.URL.Path, user, pass, ok, r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		got = r.PostForm
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"GT1","status":"in-progress"}`))
	}))
	t.Cleanup(srv.Close)
	ts := &TwilioStatus{BaseURL: srv.URL, AccountSID: "AC123", AuthToken: "tok"}

	if err := ts.Transcribe(context.Background(), "CA9", testTranscriptionURL); err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"StatusCallbackUrl":    {testTranscriptionURL},
		"StatusCallbackMethod": {"POST"},
		"Track":                {"both_tracks"},
		"LanguageCode":         {"es-US"},
		"PartialResults":       {"false"},
		"ProfanityFilter":      {"false"},
	}
	if got.Encode() != want.Encode() {
		t.Errorf("form = %v\nwant   %v", got, want)
	}
}

func TestTwilioTranscribeReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":32650,"message":"too many forks"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	ts := &TwilioStatus{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "t"}
	if err := ts.Transcribe(context.Background(), "CA1", testTranscriptionURL); err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want a 400 error", err)
	}
}

func TestSetTranscriptionNeedsTheTwilioAccount(t *testing.T) {
	rt := &Routes{engine: &Engine{Status: &TwilioStatus{AccountSID: "AC1", AuthToken: "t"}}}
	rt.SetTranscription(testTranscriptionURL)
	if rt.engine.Transcriber == nil || rt.engine.TranscriptionURL != testTranscriptionURL {
		t.Errorf("with Twilio: %+v", rt.engine)
	}
	none := &Routes{engine: &Engine{}}
	none.SetTranscription(testTranscriptionURL)
	if none.engine.Transcriber != nil || none.engine.TranscriptionURL != "" {
		t.Errorf("without Twilio: %+v", none.engine)
	}
	(&Routes{}).SetTranscription(testTranscriptionURL) // no database: no engine, no panic
}
