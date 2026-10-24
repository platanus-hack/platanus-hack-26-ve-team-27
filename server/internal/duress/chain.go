package duress

import (
	"context"
	"errors"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// DefaultLLMTimeout bounds the LLM step. The verdict sits between the end
// of the call and the family's alert, so a slow LLM must not hold the alert.
const DefaultLLMTimeout = 6 * time.Second

// Chain is the production detector: Rules first, then the LLM only for the
// calls Rules marks unclear.
//
// Override policy: Rules' duress and no duress are final. A safety alert
// values recall over precision, so the LLM never turns a rules duress into
// no duress; and the rules score no false alarm on the evals, so the LLM is
// not allowed to raise one on a call Rules cleared either. Widening the LLM's
// reach (for example the call where only a third party talks, which Rules
// reads as no duress) needs measured LLM numbers in server/evals/results.md
// first.
//
// Chain never errors and always answers within Timeout plus the rules' own
// microseconds: on any LLM failure (no key, HTTP error, timeout, bad JSON)
// it returns the rules verdict with a reason that says the LLM fell back.
type Chain struct {
	Rules   Detector      // nil = Rules{}
	LLM     Detector      // nil = rules only, no fallback note
	Timeout time.Duration // 0 = DefaultLLMTimeout
}

var _ Detector = Chain{}

func (c Chain) Detect(ctx context.Context, transcript []contract.TranscriptTurn, phrase string) (Result, error) {
	rules := c.Rules
	if rules == nil {
		rules = Rules{}
	}
	base, err := rules.Detect(ctx, transcript, phrase)
	if err != nil {
		// Rules{} never errors; a custom first step that does still must not
		// lose the call. Unclear lets the notifier decide.
		base = Result{contract.VerdictUnclear, "the rules step failed: " + err.Error()}
	}
	if base.Verdict != contract.VerdictUnclear || c.LLM == nil {
		return base, nil
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultLLMTimeout
	}
	llmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	got, err := detectBounded(llmCtx, c.LLM, transcript, phrase)
	if err != nil {
		return Result{base.Verdict, base.Reason + " (LLM unavailable: " + fallbackCause(llmCtx, err) + ")"}, nil
	}
	return Result{got.Verdict, "rules: " + base.Reason + "; LLM: " + got.Reason}, nil
}

// detectBounded returns when d answers or ctx ends, whichever is first, so a
// Detector that ignores its context still can't hold the verdict.
func detectBounded(ctx context.Context, d Detector, transcript []contract.TranscriptTurn, phrase string) (Result, error) {
	type answer struct {
		r   Result
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		r, err := d.Detect(ctx, transcript, phrase)
		ch <- answer{r, err}
	}()
	select {
	case a := <-ch:
		return a.r, a.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// fallbackCause names why the LLM gave no verdict, short enough for
// calls.verdict_reason and never carrying a response body.
func fallbackCause(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	msg := err.Error()
	if len(msg) > 120 {
		msg = msg[:120] + "..."
	}
	return msg
}
