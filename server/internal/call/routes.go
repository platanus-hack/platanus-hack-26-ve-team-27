package call

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// venezuela is the only destination the spike route dials: a +58 number with
// 10 digits after the country code. A leaked CALL_SPIKE_TOKEN then cannot
// run up premium-rate or international calls on the Twilio account.
var venezuela = regexp.MustCompile(`^\+58[0-9]{10}$`)

// Routes holds the call routes and the call engine. Create it with
// NewRoutes.
type Routes struct {
	webhook    *Webhook
	client     *Client
	engine     *Engine // nil without a database
	fallback   *Fallback
	spikeToken string
	forceFail  bool
}

// retryDelay is the pause before the one retry of a failed call.
const retryDelay = time.Second

// NewRoutes builds the routes from the config: ElevenLabs places the call,
// Twilio (when its account is set) reports the call status, and PublicURL
// goes into the fallback log line. store is nil when the server has no
// database: the webhook then only logs, and Starter only logs the handoff.
// With CALL_FORCE_FAILURE every call fails without dialing, to rehearse the
// retry and the fallback.
func NewRoutes(cfg config.Config, store Store) *Routes {
	el := cfg.ElevenLabs
	rt := &Routes{
		webhook: &Webhook{Secret: el.WebhookSecret},
		client: &Client{
			APIKey:        el.APIKey,
			AgentID:       el.AgentID,
			PhoneNumberID: el.PhoneNumberID,
			VoiceID:       el.VoiceID,
		},
		fallback:   NewFallback(el.AgentID, cfg.PublicURL, el.FallbackToken),
		spikeToken: el.CallSpikeToken,
		forceFail:  el.ForceCallFailure,
	}
	if store != nil {
		rt.engine = &Engine{Store: store, Placer: rt.client, Fallback: rt.fallback, RetryDelay: retryDelay}
		if rt.forceFail {
			rt.engine.Placer = forcedFailure{}
		}
		if cfg.Twilio.AccountSID != "" && cfg.Twilio.AuthToken != "" {
			rt.engine.Status = &TwilioStatus{AccountSID: cfg.Twilio.AccountSID, AuthToken: cfg.Twilio.AuthToken}
		}
		rt.webhook.Received = rt.engine.Received
		rt.webhook.InitiationFailed = rt.engine.InitiationFailed
	}
	return rt
}

// errForcedFailure is what every call returns under CALL_FORCE_FAILURE.
var errForcedFailure = errors.New("call: forced failure (CALL_FORCE_FAILURE)")

// forcedFailure is the Placer under CALL_FORCE_FAILURE: it never dials.
type forcedFailure struct{}

func (forcedFailure) Place(context.Context, Call) (Placed, error) {
	return Placed{}, errForcedFailure
}

// SetFinished sets the engine's post-call hook (Engine.Finished); compose
// several with Hooks. Without a database there is no engine and no webhook
// is stored, so it does nothing.
func (rt *Routes) SetFinished(f func(ctx context.Context, f Finished)) {
	if rt.engine != nil {
		rt.engine.Finished = f
	}
}

// SetProgress sets the engine's Twilio status hook (Engine.Progress). Without
// a database it does nothing.
func (rt *Routes) SetProgress(f func(ctx context.Context, callID int64, twilioStatus string, at time.Time)) {
	if rt.engine != nil {
		rt.engine.Progress = f
	}
}

// Hooks runs each post-call hook in order, skipping nil ones, so main can
// list them in one place: the duress verdict and the family alert (#21)
// before the incident report (#76), which seals what they stored. It returns
// nil when every hook is nil. Each hook runs inside the webhook request, so
// a slow one must start its own goroutine.
func Hooks(hooks ...func(ctx context.Context, f Finished)) func(ctx context.Context, f Finished) {
	var list []func(ctx context.Context, f Finished)
	for _, h := range hooks {
		if h != nil {
			list = append(list, h)
		}
	}
	if len(list) == 0 {
		return nil
	}
	return func(ctx context.Context, f Finished) {
		for _, h := range list {
			h(ctx, f)
		}
	}
}

// Starter is what intake hands BranchCall incidents to: the engine, or
// intake.LogHandoff without a database.
func (rt *Routes) Starter() intake.CallStarter {
	if rt.engine == nil {
		return intake.LogHandoff{}
	}
	return rt.engine
}

// Register mounts POST /webhooks/elevenlabs and GET /call/fallback always,
// and POST /spike/call only when CALL_SPIKE_TOKEN is set: without it the
// route does not exist (404).
func (rt *Routes) Register(mux *http.ServeMux) {
	if rt.webhook.Secret == "" {
		log.Print("call: WARNING: ELEVENLABS_WEBHOOK_SECRET is not set; /webhooks/elevenlabs answers 503")
	}
	if rt.engine != nil && rt.forceFail {
		log.Print("call: WARNING: CALL_FORCE_FAILURE is on; every call fails and opens the fallback")
	}
	if rt.engine != nil && rt.engine.Status == nil {
		log.Print("call: WARNING: TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN are not set; no ring time, and only the call_initiation_failure webhook triggers the retry")
	}
	if rt.engine != nil && (rt.client.APIKey == "" || rt.client.AgentID == "" || rt.client.PhoneNumberID == "") {
		log.Print("call: WARNING: ElevenLabs credentials are not set; every call will fail")
	}
	if rt.fallback.token == "" {
		log.Print("call: WARNING: FALLBACK_TOKEN is not set; the fallback page sends the browser call without the duress and safe phrases")
	}
	mux.Handle("POST /webhooks/elevenlabs", rt.webhook)
	mux.Handle("GET "+FallbackPath, rt.fallback)
	if rt.spikeToken != "" {
		mux.HandleFunc("POST /spike/call", rt.handleSpikeCall)
	}
}

type spikeCallRequest struct {
	To         string `json:"to"`                    // +58 E.164, e.g. +584141234567
	IncidentID string `json:"incident_id,omitempty"` // optional, echoed by the webhook
}

type spikeCallResponse struct {
	ConversationID string `json:"conversation_id"`
	CallSID        string `json:"call_sid"`
	// RequestMS is how long ElevenLabs took to accept the call, measured on
	// the server. Ringing comes later; spikes/call/place_call.py times it.
	RequestMS int64 `json:"request_ms"`
}

// handleSpikeCall places one call: Authorization: Bearer <CALL_SPIKE_TOKEN>,
// body {"to": "+58..."}. 401 bad token, 400 bad body or a non-+58 number,
// 502 when ElevenLabs refuses (the reason goes to the server log only), 200
// with the ids and the request time otherwise.
func (rt *Routes) handleSpikeCall(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(rt.spikeToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var in spikeCallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if !venezuela.MatchString(in.To) {
		http.Error(w, `"to" must be a +58 number such as +584141234567`, http.StatusBadRequest)
		return
	}

	start := time.Now()
	// The spike has no device: the defaults keep every variable the prompt
	// names set.
	placed, err := rt.client.Place(r.Context(), Call{To: in.To, Variables: CallVariables(in.IncidentID, Callee{})})
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("call spike: place: %v", err)
		http.Error(w, "call not placed; see the server log", http.StatusBadGateway)
		return
	}
	log.Printf("call spike: placed conversation %s call %s in %d ms", placed.ConversationID, placed.CallSID, elapsed.Milliseconds())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(spikeCallResponse{
		ConversationID: placed.ConversationID,
		CallSID:        placed.CallSID,
		RequestMS:      elapsed.Milliseconds(),
	})
}
