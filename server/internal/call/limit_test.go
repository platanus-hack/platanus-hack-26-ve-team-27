package call

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/ratelimit"
)

const otherPhone = "+584241112233"

func limited(perNumber, total int) *ratelimit.Limiter {
	return &ratelimit.Limiter{PerNumber: perNumber, Total: total, Window: 10 * time.Minute}
}

// The call after the cap is not placed, opens no fallback and says so in the
// timing line; another number is still called.
func TestCallOverTheLimitIsNotPlaced(t *testing.T) {
	captureLog(t)
	store := &memStore{phone: victimPhone}
	placer := &scriptPlacer{}
	e := testEngine(store, placer, nil, nil)
	e.OutcomeTimeout = 10 * time.Millisecond // placed, no webhook: one call each
	e.Limit = limited(2, 10)

	e.StartCall(context.Background(), sampleIncidentInput())
	e.StartCall(context.Background(), sampleIncidentInput())
	out := captureLog(t)
	e.StartCall(context.Background(), sampleIncidentInput())

	if placer.calls != 2 {
		t.Errorf("Place calls = %d, want 2 (the per-number cap)", placer.calls)
	}
	if got := store.snapshot(); len(got) != 3 || got[2] != StatusFailed {
		t.Errorf("calls = %v, want the refused third row failed", got)
	}
	if p := e.Fallback.pending(); len(p) != 0 {
		t.Errorf("fallback opened for %+v; a refused call must not open it", p)
	}
	line := timingLine(t, out.String())
	if !strings.Contains(line, "outcome=rate_limited") || !strings.Contains(line, "attempts=1") {
		t.Errorf("timing line %q, want outcome=rate_limited after 1 attempt", line)
	}
	if !strings.Contains(out.String(), "call to ****4567 over the limit of 2 per number per 10m0s; not calling") {
		t.Errorf("log does not report the refused call:\n%s", out)
	}
	if strings.Contains(out.String(), victimPhone) {
		t.Errorf("log names the phone in full:\n%s", out)
	}

	store.mu.Lock()
	store.phone = otherPhone
	store.mu.Unlock()
	e.StartCall(context.Background(), sampleIncidentInput())
	if placer.calls != 3 {
		t.Errorf("Place calls = %d, want another number still called", placer.calls)
	}
}

// The retry counts as a placement, and a retry over the cap falls back to
// nothing: no third call and no fallback page.
func TestRetryCountsTowardTheLimit(t *testing.T) {
	captureLog(t)
	placer := &scriptPlacer{errs: []error{errors.New("boom"), errors.New("boom"), errors.New("boom")}}
	limit := limited(3, 10)
	first := testEngine(&memStore{phone: victimPhone}, placer, nil, nil)
	first.Limit = limit
	first.StartCall(context.Background(), sampleIncidentInput())
	if placer.calls != 2 || len(first.Fallback.pending()) != 1 {
		t.Fatalf("first incident: %d calls, fallback %+v; want a call, a retry and the fallback", placer.calls, first.Fallback.pending())
	}

	out := captureLog(t)
	second := testEngine(&memStore{phone: victimPhone}, placer, nil, nil)
	second.Limit = limit
	second.StartCall(context.Background(), sampleIncidentInput())

	if placer.calls != 3 {
		t.Errorf("Place calls = %d, want 3: the second incident's retry is over the cap", placer.calls)
	}
	if p := second.Fallback.pending(); len(p) != 0 {
		t.Errorf("fallback opened for %+v", p)
	}
	if line := timingLine(t, out.String()); !strings.Contains(line, "outcome=rate_limited") || !strings.Contains(line, "attempts=2") {
		t.Errorf("timing line %q, want outcome=rate_limited after 2 attempts", line)
	}
}

func TestTotalCallLimitRefusesEveryNumber(t *testing.T) {
	out := captureLog(t)
	store := &memStore{}
	placer := &scriptPlacer{}
	e := testEngine(store, placer, nil, nil)
	e.OutcomeTimeout = 10 * time.Millisecond
	e.Limit = limited(5, 2)

	for _, phone := range []string{victimPhone, otherPhone, "+584125556677"} {
		store.mu.Lock()
		store.phone = phone
		store.mu.Unlock()
		e.StartCall(context.Background(), sampleIncidentInput())
	}
	if placer.calls != 2 {
		t.Errorf("Place calls = %d, want 2 (the total cap)", placer.calls)
	}
	if !strings.Contains(out.String(), "call to ****6677 over the limit of 2 in total per 10m0s") {
		t.Errorf("log does not report the total cap:\n%s", out)
	}
}

// NewRoutes takes the caps from CALL_LIMIT_*, with the defaults for the
// rest, and Register logs them at boot.
func TestCallLimitsComeFromTheConfig(t *testing.T) {
	out := captureLog(t)
	cfg := config.LoadFrom(func(k string) string { return map[string]string{"CALL_LIMIT_PER_NUMBER": "1"}[k] })
	rt := NewRoutes(cfg, &memStore{phone: victimPhone})
	rt.Register(http.NewServeMux())

	if l := rt.engine.Limit; l.PerNumber != 1 || l.Total != config.DefaultCallsTotal || l.Window != config.LimitWindow {
		t.Errorf("Limit = %v", l)
	}
	if !strings.Contains(out.String(), "call: placing at most 1 per number and 40 in total per 10m0s") {
		t.Errorf("boot log:\n%s", out)
	}
}
