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
	saved   duress.Result
	saveErr error
}

func (r *recorder) SaveVerdict(_ context.Context, _ string, v contract.Verdict, reason string) error {
	r.events = append(r.events, "save")
	r.saved = duress.Result{Verdict: v, Reason: reason}
	return r.saveErr
}

func (r *recorder) notify(t *testing.T) Notify {
	return func(_ context.Context, _ Call, got duress.Result) {
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

func (failing) Detect(context.Context, []contract.TranscriptTurn, string) (duress.Result, error) {
	return duress.Result{}, errors.New("boom")
}

func TestDecideTurnsADetectorErrorIntoUnclear(t *testing.T) {
	rec := &recorder{}
	got, err := Decide(context.Background(), failing{}, rec, call, nil)
	if err != nil || got.Verdict != contract.VerdictUnclear || rec.saved != got {
		t.Fatalf("got %+v, %v, stored %+v", got, err, rec.saved)
	}
}
