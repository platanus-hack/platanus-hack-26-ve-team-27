// Package verdict is the post-call step between the call engine (#18) and
// the notifier (#20): judge the transcript, take the call's context when the
// family will be alerted (#106), store both on the call, then hand them to
// the notifier. Decide runs these in that order, in one goroutine, so the
// notifier never sends before the verdict exists, and the detector's and the
// extraction's own timeouts bound how long the alert waits.
package verdict

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// Decision is one call's outcome: the detector's verdict and, on duress and
// unclear calls, the context the extraction took (nil when it was not asked
// for or failed).
type Decision struct {
	duress.Result
	Ficha *contract.CallContext
}

// Store saves a call's decision. DBStore is the real one.
type Store interface {
	SaveVerdict(ctx context.Context, conversationID string, d Decision) error
}

// DBStore stores decisions in calls through db.SaveVerdictWithContext.
type DBStore struct{ Pool *sql.DB }

func (s DBStore) SaveVerdict(ctx context.Context, conversationID string, d Decision) error {
	return db.SaveVerdictWithContext(ctx, s.Pool, conversationID, d.Verdict, d.Reason, d.Ficha)
}

// Call is one finished "Mamá" call, as the post-call webhook delivers it,
// with the device's two phrases (devices.duress_phrase and safe_phrase).
type Call struct {
	ConversationID string
	Transcript     []contract.TranscriptTurn
	DuressPhrase   string
	SafePhrase     string // "" when unset
}

func (c Call) phrases() duress.Phrases {
	return duress.Phrases{Duress: c.DuressPhrase, Safe: c.SafePhrase}
}

// Notify receives the stored decision. #20 adapts its Notifier to this.
type Notify func(ctx context.Context, call Call, d Decision)

// Decider holds the steps of Decide. Detector and Store are required.
type Decider struct {
	Detector duress.Detector
	// Extractor takes the call's context on duress and unclear calls; nil
	// skips it.
	Extractor duress.Extractor
	// ExtractTimeout bounds the extraction; 0 is duress.DefaultExtractTimeout.
	ExtractTimeout time.Duration
	Store          Store
}

// Decide judges call, takes its context when the verdict alerts the family
// (duress or unclear, never no_duress), stores both, then calls notify (when
// not nil) with them, and returns them.
//
// A detector error becomes an unclear verdict instead of stopping the
// alert. An extraction error or timeout only drops the context: the alert
// goes out without it. A store error is returned, but only after notify ran:
// the family's alert matters more than the row, and a retried webhook stores
// it again.
func (d Decider) Decide(ctx context.Context, call Call, notify Notify) (Decision, error) {
	r, err := d.Detector.Detect(ctx, call.Transcript, call.phrases())
	if err != nil {
		r = duress.Result{Verdict: contract.VerdictUnclear, Reason: "the detector failed: " + err.Error()}
	}
	dec := Decision{Result: r}
	if d.Extractor != nil && r.Verdict != contract.VerdictNoDuress {
		c, err := duress.ExtractContext(ctx, d.Extractor, d.ExtractTimeout, call.Transcript, call.phrases())
		if err != nil {
			log.Printf("verdict: conversation %s: no call context (%v); alerting without it", call.ConversationID, err)
		} else {
			dec.Ficha = &c
		}
	}
	saveErr := d.Store.SaveVerdict(ctx, call.ConversationID, dec)
	if notify != nil {
		notify(ctx, call, dec)
	}
	if saveErr != nil {
		return dec, fmt.Errorf("verdict: save %s: %w", call.ConversationID, saveErr)
	}
	return dec, nil
}

// Decide is Decider{Detector: det, Store: store}.Decide: no call context.
func Decide(ctx context.Context, det duress.Detector, store Store, call Call, notify Notify) (Decision, error) {
	return Decider{Detector: det, Store: store}.Decide(ctx, call, notify)
}
