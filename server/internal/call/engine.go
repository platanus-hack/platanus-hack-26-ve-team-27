package call

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// placeTimeout bounds the outbound-call request. The phone must ring within
// 15 s of the alert; the spike measured 2.3–6.2 s from request to ringing.
const placeTimeout = 15 * time.Second

// Placer places one outbound call. *Client is the real one.
type Placer interface {
	Place(ctx context.Context, to, incidentID string) (Placed, error)
}

// Finished is a call the post-call webhook stored.
type Finished struct {
	IncidentID     string
	ConversationID string
	Status         string // a calls.status value: done or failed
	Transcript     []contract.TranscriptTurn
}

// Engine places the "Mamá" call of a BranchCall incident and stores what
// the webhook reports about it. Create it with a struct literal.
type Engine struct {
	Store  Store
	Placer Placer
	// Finished, when set, runs after a post-call webhook is stored: the
	// hook for the duress verdict and the family alert. It runs inside the
	// webhook request, so start a goroutine for slow work. It runs again for
	// a repeated webhook, so it must be idempotent (db.SaveVerdict
	// overwrites, and the notifier claims the incident once).
	Finished func(ctx context.Context, f Finished)
}

var _ intake.CallStarter = (*Engine)(nil)

// StartCall stores a requested call, then places it to the device's phone
// with incident_id as a dynamic variable, then stores the conversation_id.
// Inserting first lets a webhook that beats the response find the row. It
// only logs failures: the handoff has no caller to report to.
func (e *Engine) StartCall(ctx context.Context, inc intake.Incident) {
	start := time.Now()
	callID, to, err := e.Store.Begin(ctx, inc.ID)
	if err != nil {
		log.Printf("call: incident %s: store call: %v", inc.ID, err)
		return
	}
	if !venezuela.MatchString(to) {
		// No auth on /devices: refusing other numbers keeps a stranger from
		// running up international calls on the Twilio account.
		log.Printf("call: incident %s call %d: the device number is not a +58 number; not calling", inc.ID, callID)
		e.fail(ctx, inc.ID, callID)
		return
	}

	pctx, cancel := context.WithTimeout(ctx, placeTimeout)
	placed, err := e.Placer.Place(pctx, to, inc.ID)
	cancel()
	if err != nil {
		log.Printf("call: incident %s call %d: place: %v", inc.ID, callID, err)
		e.fail(ctx, inc.ID, callID)
		return
	}
	log.Printf("call: incident %s call %d: placed conversation %s in %d ms", inc.ID, callID, placed.ConversationID, time.Since(start).Milliseconds())
	if err := e.Store.Placed(ctx, callID, placed); err != nil {
		log.Printf("call: incident %s call %d: store conversation %s: %v", inc.ID, callID, placed.ConversationID, err)
	}
}

func (e *Engine) fail(ctx context.Context, incidentID string, callID int64) {
	if err := e.Store.Failed(ctx, callID); err != nil {
		log.Printf("call: incident %s call %d: mark failed: %v", incidentID, callID, err)
	}
}

// Received stores a post_call_transcription webhook, then runs Finished. A
// webhook for an incident the server does not know is logged and dropped;
// any other store error is returned so the webhook answers 500 and
// ElevenLabs retries it.
func (e *Engine) Received(ctx context.Context, hook contract.CallWebhook) error {
	LogSummary(hook)
	err := e.Store.SaveTranscript(ctx, hook)
	if errors.Is(err, ErrUnknownIncident) {
		log.Printf("call webhook: conversation %s: no call or incident %q to store it on; dropped", hook.Data.ConversationID, hook.IncidentID())
		return nil
	}
	if err != nil {
		return err
	}
	if e.Finished != nil {
		e.Finished(ctx, Finished{
			IncidentID:     hook.IncidentID(),
			ConversationID: hook.Data.ConversationID,
			Status:         webhookStatus(hook.Data.Status),
			Transcript:     hook.Data.Transcript,
		})
	}
	return nil
}

// InitiationFailed marks the call failed when it never connected (busy, no
// answer, Twilio refused the number).
func (e *Engine) InitiationFailed(ctx context.Context, hook contract.CallWebhook) error {
	return e.Store.FailedConversation(ctx, hook.Data.ConversationID, hook.IncidentID())
}
