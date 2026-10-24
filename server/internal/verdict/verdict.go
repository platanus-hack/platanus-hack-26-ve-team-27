// Package verdict is the post-call step between the call engine (#18) and
// the notifier (#20): judge the transcript, store the verdict on the call,
// then hand it to the notifier. Decide runs these in that order, in one
// goroutine, so the notifier never sends before the verdict exists, and the
// detector's own timeout (duress.Chain) bounds how long the alert waits.
package verdict

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// Store saves a call's verdict. DBStore is the real one.
type Store interface {
	SaveVerdict(ctx context.Context, conversationID string, verdict contract.Verdict, reason string) error
}

// DBStore stores verdicts in calls through db.SaveVerdict.
type DBStore struct{ Pool *sql.DB }

func (s DBStore) SaveVerdict(ctx context.Context, conversationID string, verdict contract.Verdict, reason string) error {
	return db.SaveVerdict(ctx, s.Pool, conversationID, verdict, reason)
}

// Call is one finished "Mamá" call, as the post-call webhook delivers it.
type Call struct {
	ConversationID string
	Transcript     []contract.TranscriptTurn
	DuressPhrase   string
}

// Notify receives the stored verdict. #20 adapts its Notifier to this.
type Notify func(ctx context.Context, call Call, r duress.Result)

// Decide judges call with det, stores the result, then calls notify (when
// not nil) with it, and returns it.
//
// A detector error becomes an unclear verdict instead of stopping the
// alert. A store error is returned, but only after notify ran: the family's
// alert matters more than the row, and a retried webhook stores it again.
func Decide(ctx context.Context, det duress.Detector, store Store, call Call, notify Notify) (duress.Result, error) {
	r, err := det.Detect(ctx, call.Transcript, call.DuressPhrase)
	if err != nil {
		r = duress.Result{Verdict: contract.VerdictUnclear, Reason: "the detector failed: " + err.Error()}
	}
	saveErr := store.SaveVerdict(ctx, call.ConversationID, r.Verdict, r.Reason)
	if notify != nil {
		notify(ctx, call, r)
	}
	if saveErr != nil {
		return r, fmt.Errorf("verdict: save %s: %w", call.ConversationID, saveErr)
	}
	return r, nil
}
