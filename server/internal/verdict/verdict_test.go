package verdict

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// recorder is a Store and a Notify that log what happened, in order.
type recorder struct {
	events  []string
	saved   Decision
	saveErr error
}

func (r *recorder) SaveVerdict(_ context.Context, _ string, d Decision) error {
	r.events = append(r.events, "save")
	r.saved = d
	return r.saveErr
}

func (r *recorder) notify(t *testing.T) Notify {
	return func(_ context.Context, _ Call, got Decision) {
		r.events = append(r.events, "notify")
		if got != r.saved {
			t.Errorf("notifier got %+v, but the stored verdict is %+v", got, r.saved)
		}
	}
}

var call = Call{
	ConversationID: "conv_1",
	DuressPhrase:   "se me quemaron las arepas",
	Transcript: []contract.TranscriptTurn{
		{Role: duress.RoleAgent, Message: "¿Qué cocinaste?"},
		{Role: duress.RoleUser, Message: "Esta vez no se me quemaron las arepas."},
	},
}

func TestDecideStoresBeforeNotifying(t *testing.T) {
	rec := &recorder{}
	got, err := Decide(context.Background(), duress.Chain{}, rec, call, rec.notify(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 2 || rec.events[0] != "save" || rec.events[1] != "notify" {
		t.Fatalf("events %v, want [save notify]", rec.events)
	}
	if got != rec.saved || got.Verdict != contract.VerdictUnclear {
		t.Errorf("returned %+v, stored %+v", got, rec.saved)
	}
}

func TestDecideIsBoundedWhenTheLLMHangs(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Read the body first: only then does the server notice the
		// client hang up and cancel r.Context().
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	const timeout = 100 * time.Millisecond
	det := duress.Chain{LLM: duress.LLM{APIKey: "test-key", Endpoint: srv.URL, Client: srv.Client()}, Timeout: timeout}
	rec := &recorder{}
	start := time.Now()
	got, err := Decide(context.Background(), det, rec, call, rec.notify(t))
	if elapsed := time.Since(start); elapsed > timeout+time.Second {
		t.Errorf("Decide took %v with a hung LLM, want it bounded by %v", elapsed, timeout)
	}
	if err != nil || got.Verdict != contract.VerdictUnclear || len(rec.events) != 2 {
		t.Fatalf("got %+v, %v, events %v; want the rules verdict stored and sent", got, err, rec.events)
	}
}

func TestDecideStillNotifiesWhenTheStoreFails(t *testing.T) {
	rec := &recorder{saveErr: errors.New("db down")}
	_, err := Decide(context.Background(), duress.Rules{}, rec, call, rec.notify(t))
	if err == nil {
		t.Fatal("want the store error back")
	}
	if len(rec.events) != 2 || rec.events[1] != "notify" {
		t.Errorf("events %v, want the notifier to run anyway", rec.events)
	}
}

type failing struct{}

func (failing) Detect(context.Context, []contract.TranscriptTurn, duress.Phrases) (duress.Result, error) {
	return duress.Result{}, errors.New("boom")
}

func TestDecideTurnsADetectorErrorIntoUnclear(t *testing.T) {
	rec := &recorder{}
	got, err := Decide(context.Background(), failing{}, rec, call, nil)
	if err != nil || got.Verdict != contract.VerdictUnclear || rec.saved != got {
		t.Fatalf("got %+v, %v, stored %+v", got, err, rec.saved)
	}
}

// fakeExtractor returns ficha (or err) after delay, counting its calls. It
// ignores its context on purpose, like a stuck client.
type fakeExtractor struct {
	ficha contract.CallContext
	err   error
	delay time.Duration
	calls int
}

func (f *fakeExtractor) Extract(context.Context, []contract.TranscriptTurn, duress.Phrases) (contract.CallContext, error) {
	f.calls++
	time.Sleep(f.delay)
	return f.ficha, f.err
}

var carFicha = contract.CallContext{Lugar: "la autopista", Personas: "dos panas", Vehiculo: "un carro",
	Rumbo: contract.CallContextUnknown, Situacion: "Va en un carro con dos panas.", Confianza: contract.ConfianzaMedia}

func safeCall(said string) Call {
	return Call{
		ConversationID: "conv_2",
		DuressPhrase:   "se me quemaron las arepas",
		SafePhrase:     "ya voy llegando",
		Transcript: []contract.TranscriptTurn{
			{Role: duress.RoleAgent, Message: "¿Dónde estás?"},
			{Role: duress.RoleUser, Message: said},
		},
	}
}

// TestDecideExtractsOnAlertOnly: the context is taken and stored on duress
// and unclear calls, before the notifier runs, and never asked for on the
// safe phrase.
func TestDecideExtractsOnAlertOnly(t *testing.T) {
	tests := []struct {
		said      string
		want      contract.Verdict
		wantCalls int
	}{
		{"Voy en un carro con dos panas por la autopista. Se me quemaron las arepas.", contract.VerdictDuress, 1},
		{"Voy en un carro con dos panas por la autopista.", contract.VerdictUnclear, 1},
		{"Tranquila, ma, ya voy llegando.", contract.VerdictNoDuress, 0},
	}
	for _, tt := range tests {
		t.Run(string(tt.want), func(t *testing.T) {
			ex := &fakeExtractor{ficha: carFicha}
			rec := &recorder{}
			d := Decider{Detector: duress.Rules{}, Extractor: ex, Store: rec}
			got, err := d.Decide(context.Background(), safeCall(tt.said), rec.notify(t))
			if err != nil {
				t.Fatal(err)
			}
			if got.Verdict != tt.want || ex.calls != tt.wantCalls {
				t.Fatalf("got %s (%s) after %d extractions, want %s after %d", got.Verdict, got.Reason, ex.calls, tt.want, tt.wantCalls)
			}
			if tt.wantCalls == 0 {
				if got.Ficha != nil || rec.saved.Ficha != nil {
					t.Errorf("no_duress has a ficha: %+v", got.Ficha)
				}
				return
			}
			if rec.saved.Ficha == nil || *rec.saved.Ficha != carFicha {
				t.Errorf("stored ficha %+v, want %+v", rec.saved.Ficha, carFicha)
			}
			if len(rec.events) != 2 || rec.events[0] != "save" {
				t.Errorf("events %v, want [save notify]", rec.events)
			}
		})
	}
}

// TestDecideAlertsWithoutTheFichaWhenTheLLMFails: an extraction error or a
// timeout drops the ficha, and the notifier still runs, on time.
func TestDecideAlertsWithoutTheFichaWhenTheLLMFails(t *testing.T) {
	const timeout = 100 * time.Millisecond
	for name, ex := range map[string]*fakeExtractor{
		"error":   {err: errors.New("HTTP 529")},
		"timeout": {ficha: carFicha, delay: 2 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			d := Decider{Detector: duress.Rules{}, Extractor: ex, ExtractTimeout: timeout, Store: rec}
			start := time.Now()
			got, err := d.Decide(context.Background(), safeCall("Se me quemaron las arepas."), rec.notify(t))
			if elapsed := time.Since(start); elapsed > timeout+500*time.Millisecond {
				t.Errorf("Decide took %v, want it bounded by the %v extraction timeout", elapsed, timeout)
			}
			if err != nil || got.Verdict != contract.VerdictDuress || got.Ficha != nil {
				t.Fatalf("got %+v, %v; want duress without a ficha", got, err)
			}
			if len(rec.events) != 2 || rec.events[1] != "notify" {
				t.Errorf("events %v, want the notifier to run", rec.events)
			}
		})
	}
}

// slowDetector answers after delay, like a slow LLM step.
type slowDetector struct{ delay time.Duration }

func (s slowDetector) Detect(ctx context.Context, t []contract.TranscriptTurn, p duress.Phrases) (duress.Result, error) {
	time.Sleep(s.delay)
	return duress.Rules{}.Detect(ctx, t, p)
}

// The detector's LLM and the extraction wait together, not one after the
// other.
func TestDecideRunsTheExtractionAlongsideTheDetector(t *testing.T) {
	const delay = 300 * time.Millisecond
	ex := &fakeExtractor{ficha: carFicha, delay: delay}
	rec := &recorder{}
	d := Decider{Detector: slowDetector{delay}, Extractor: ex, Store: rec}
	start := time.Now()
	got, err := d.Decide(context.Background(), safeCall("Voy en un carro por la autopista."), nil)
	if elapsed := time.Since(start); elapsed > delay+200*time.Millisecond {
		t.Errorf("Decide took %v, want about %v (the two waits overlap)", elapsed, delay)
	}
	if err != nil || got.Verdict != contract.VerdictUnclear || got.Ficha == nil {
		t.Fatalf("got %+v, %v; want unclear with the ficha", got, err)
	}
}
