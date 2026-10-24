package call

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync"
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

// Defaults for the Engine fields left zero.
const (
	// defaultOutcomeTimeout bounds the wait for an attempt's outcome. Twilio
	// gives up on an unanswered phone after about 60 s.
	defaultOutcomeTimeout = 90 * time.Second
	defaultPollEvery      = 500 * time.Millisecond
)

// maxAttempts is the first call plus one retry; then the fallback opens.
const maxAttempts = 2

// ringTarget is the demo budget from alert to ringing (#23).
const ringTarget = 15 * time.Second

// Finished is a call the post-call webhook stored.
type Finished struct {
	IncidentID     string
	ConversationID string
	Status         string // a calls.status value: done or failed
	Transcript     []contract.TranscriptTurn
}

// Engine places the "Mamá" call of a BranchCall incident and stores what
// the webhook reports about it. A call that is not placed, or that Twilio
// or ElevenLabs reports busy, unanswered or failed, is retried once; when
// the retry fails too, Fallback opens the WebRTC page. Every incident logs
// one "call timing:" line. Create it with a struct literal.
type Engine struct {
	Store  Store
	Placer Placer
	// Status, when set, is polled after each placed call to time the ring
	// and to see a busy or unanswered phone. Without it there is no ring
	// time, and only the call_initiation_failure webhook reports a failure.
	Status StatusReader
	// Fallback, when set, opens the WebRTC fallback after the last failed
	// attempt; nil only logs it.
	Fallback *Fallback
	// Finished, when set, runs after a post-call webhook is stored: the
	// hook for the duress verdict and the family alert. It runs inside the
	// webhook request, so start a goroutine for slow work. It runs again for
	// a repeated webhook, so it must be idempotent (db.SaveVerdict
	// overwrites, and the notifier claims the incident once).
	Finished func(ctx context.Context, f Finished)

	RetryDelay     time.Duration    // pause before the retry; zero retries at once
	OutcomeTimeout time.Duration    // defaultOutcomeTimeout when zero
	PollEvery      time.Duration    // defaultPollEvery when zero
	Now            func() time.Time // time.Now when nil

	mu      sync.Mutex
	waiting map[string]chan webhookOutcome // incident id -> the attempt waiting for a webhook
}

var _ intake.CallStarter = (*Engine)(nil)

// webhookOutcome is what a webhook says about one conversation.
type webhookOutcome struct {
	conversationID string
	failed         bool // call_initiation_failure: never connected
}

// attemptResult is how one attempt ended.
type attemptResult int

const (
	attemptFailed    attemptResult = iota // retry, or fall back after the last
	attemptConnected                      // rang/answered, or no failure seen: stop
	attemptHopeless                       // not worth a retry (not a +58 number)
	attemptNoCall                         // the incident is not stored: nothing to do
)

// timing is what the "call timing:" line reports for one incident. Times
// are measured from when the engine got the incident (the alert reached
// the server and intake stored it); zero means it did not happen.
type timing struct {
	start    time.Time
	trigger  time.Time // triggered_at from the phone's clock
	attempts int
	placed   time.Time // first call accepted by ElevenLabs
	ringing  time.Time // first time Twilio said ringing (or later)
}

// StartCall calls the device's phone, retries once if that fails, and opens
// the fallback if the retry fails too. It blocks until then (intake runs it
// in its own goroutine) and only logs failures: the handoff has no caller to
// report to. It ends with one "call timing:" line.
func (e *Engine) StartCall(ctx context.Context, inc intake.Incident) {
	t := timing{start: e.now(), trigger: inc.TriggeredAt}
	outcome := "connected"
	for {
		t.attempts++
		res := e.attempt(ctx, inc, &t)
		if res == attemptConnected {
			break
		}
		if res == attemptNoCall {
			outcome = "not_called"
			break
		}
		if res == attemptFailed && t.attempts < maxAttempts {
			log.Printf("call: incident %s: attempt %d failed; retrying", inc.ID, t.attempts)
			if !sleep(ctx, e.RetryDelay) {
				outcome = "canceled"
				break
			}
			continue
		}
		outcome = "fallback"
		if e.Fallback != nil {
			e.Fallback.Open(inc.ID, t.attempts)
		} else {
			log.Printf("call fallback: incident %s: the call failed; no fallback page configured", inc.ID)
		}
		break
	}
	e.logTiming(inc.ID, outcome, t)
}

// attempt stores a requested call, places it with incident_id as a dynamic
// variable, stores the conversation_id and waits for the outcome. Inserting
// first lets a webhook that beats the response find the row.
func (e *Engine) attempt(ctx context.Context, inc intake.Incident, t *timing) attemptResult {
	start := e.now()
	callID, to, err := e.Store.Begin(ctx, inc.ID)
	if errors.Is(err, ErrUnknownIncident) {
		log.Printf("call: incident %s: not stored; not calling", inc.ID)
		return attemptNoCall
	}
	if err != nil {
		log.Printf("call: incident %s: store call: %v", inc.ID, err)
		return attemptFailed
	}
	if !venezuela.MatchString(to) {
		// No auth on /devices: refusing other numbers keeps a stranger from
		// running up international calls on the Twilio account.
		log.Printf("call: incident %s call %d: the device number is not a +58 number; not calling", inc.ID, callID)
		e.fail(ctx, inc.ID, callID)
		return attemptHopeless
	}

	// Listen before placing: the failure webhook can beat the response.
	outcomes := e.listen(inc.ID)
	defer e.unlisten(inc.ID, outcomes)

	pctx, cancel := context.WithTimeout(ctx, placeTimeout)
	placed, err := e.Placer.Place(pctx, to, inc.ID)
	cancel()
	if err != nil {
		log.Printf("call: incident %s call %d: place: %v", inc.ID, callID, err)
		e.fail(ctx, inc.ID, callID)
		return attemptFailed
	}
	if t.placed.IsZero() {
		t.placed = e.now()
	}
	log.Printf("call: incident %s call %d: placed conversation %s in %d ms", inc.ID, callID, placed.ConversationID, e.now().Sub(start).Milliseconds())
	if err := e.Store.Placed(ctx, callID, placed); err != nil {
		log.Printf("call: incident %s call %d: store conversation %s: %v", inc.ID, callID, placed.ConversationID, err)
	}

	if e.await(ctx, inc.ID, callID, placed, outcomes, t) {
		return attemptConnected
	}
	e.fail(ctx, inc.ID, callID)
	return attemptFailed
}

// await waits for the outcome of a placed call: Twilio's status when Status
// is set, and the webhooks. It reports false when the call never connected
// (busy, no-answer, failed, canceled), and true when it was answered, ended,
// or nothing said it failed before OutcomeTimeout: a call that may be live
// is never placed again.
func (e *Engine) await(ctx context.Context, incidentID string, callID int64, placed Placed, outcomes <-chan webhookOutcome, t *timing) bool {
	timeout := e.OutcomeTimeout
	if timeout <= 0 {
		timeout = defaultOutcomeTimeout
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	var poll <-chan time.Time
	if e.Status != nil && placed.CallSID != "" {
		every := e.PollEvery
		if every <= 0 {
			every = defaultPollEvery
		}
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		poll = ticker.C
	}
	pollErrLogged := false
	for {
		select {
		case <-ctx.Done():
			return true
		case <-deadline.C:
			log.Printf("call: incident %s call %d: no outcome after %s; not retrying", incidentID, callID, timeout)
			return true
		case o := <-outcomes:
			if o.conversationID != "" && placed.ConversationID != "" && o.conversationID != placed.ConversationID {
				continue // a late webhook of an earlier attempt
			}
			if o.failed {
				log.Printf("call: incident %s call %d: ElevenLabs reports the call never connected", incidentID, callID)
				return false
			}
			return true
		case <-poll:
			status, err := e.Status.Status(ctx, placed.CallSID)
			if err != nil {
				if !pollErrLogged {
					log.Printf("call: incident %s call %d: twilio status: %v", incidentID, callID, err)
					pollErrLogged = true
				}
				continue
			}
			switch status {
			case TwilioRinging, TwilioInProgress, TwilioCompleted:
				if t.ringing.IsZero() {
					t.ringing = e.now()
					log.Printf("call: incident %s call %d: twilio %s %d ms after the alert", incidentID, callID, status, t.ringing.Sub(t.start).Milliseconds())
				}
				if status != TwilioRinging {
					return true
				}
			case TwilioBusy, TwilioNoAnswer, TwilioFailed, TwilioCanceled:
				log.Printf("call: incident %s call %d: twilio %s", incidentID, callID, status)
				return false
			}
		}
	}
}

// listen registers the current attempt of the incident for webhook
// outcomes.
func (e *Engine) listen(incidentID string) chan webhookOutcome {
	ch := make(chan webhookOutcome, 4)
	e.mu.Lock()
	if e.waiting == nil {
		e.waiting = map[string]chan webhookOutcome{}
	}
	e.waiting[incidentID] = ch
	e.mu.Unlock()
	return ch
}

func (e *Engine) unlisten(incidentID string, ch chan webhookOutcome) {
	e.mu.Lock()
	if e.waiting[incidentID] == ch {
		delete(e.waiting, incidentID)
	}
	e.mu.Unlock()
}

// notify hands a webhook outcome to the attempt waiting on the incident, if
// any. It never blocks.
func (e *Engine) notify(incidentID string, o webhookOutcome) {
	e.mu.Lock()
	ch := e.waiting[incidentID]
	e.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- o:
	default:
	}
}

// logTiming writes the incident's "call timing:" line: key=value fields,
// "-" for what did not happen. event_* are measured from when the server
// got the alert; trigger_to_ring_ms from the phone's triggered_at, so it
// includes the upload and any clock skew.
func (e *Engine) logTiming(incidentID, outcome string, t timing) {
	ms := func(from, to time.Time) string {
		if from.IsZero() || to.IsZero() {
			return "-"
		}
		return strconv.FormatInt(to.Sub(from).Milliseconds(), 10)
	}
	within := "unknown"
	if !t.ringing.IsZero() {
		within = strconv.FormatBool(t.ringing.Sub(t.start) <= ringTarget)
	}
	log.Printf("call timing: incident=%s outcome=%s attempts=%d event_to_placed_ms=%s event_to_ring_ms=%s trigger_to_ring_ms=%s ring_within_15s=%s",
		incidentID, outcome, t.attempts, ms(t.start, t.placed), ms(t.start, t.ringing), ms(t.trigger, t.ringing), within)
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// sleep waits d, or less if ctx ends; it reports whether ctx is still live.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
	e.notify(hook.IncidentID(), webhookOutcome{conversationID: hook.Data.ConversationID})
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
// The attempt waiting on the incident then retries or falls back.
func (e *Engine) InitiationFailed(ctx context.Context, hook contract.CallWebhook) error {
	if err := e.Store.FailedConversation(ctx, hook.Data.ConversationID, hook.IncidentID()); err != nil {
		return err
	}
	e.notify(hook.IncidentID(), webhookOutcome{conversationID: hook.Data.ConversationID, failed: true})
	return nil
}
