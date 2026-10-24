package call

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

// e164 is a loose E.164 check: "+", a non-zero digit, 7 to 15 digits in all.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// Routes holds the call routes. Create it with NewRoutes.
type Routes struct {
	webhook    *Webhook
	client     *Client
	spikeToken string
}

// NewRoutes builds the routes from the ElevenLabs config.
func NewRoutes(cfg config.ElevenLabs) *Routes {
	return &Routes{
		webhook: &Webhook{Secret: cfg.WebhookSecret},
		client: &Client{
			APIKey:        cfg.APIKey,
			AgentID:       cfg.AgentID,
			PhoneNumberID: cfg.PhoneNumberID,
			VoiceID:       cfg.VoiceID,
		},
		spikeToken: cfg.CallSpikeToken,
	}
}

// Register mounts POST /webhooks/elevenlabs always, and POST /spike/call only
// when CALL_SPIKE_TOKEN is set: without it the route does not exist (404).
func (rt *Routes) Register(mux *http.ServeMux) {
	if rt.webhook.Secret == "" {
		log.Print("call: WARNING: ELEVENLABS_WEBHOOK_SECRET is not set; /webhooks/elevenlabs accepts unsigned requests")
	}
	mux.Handle("POST /webhooks/elevenlabs", rt.webhook)
	if rt.spikeToken != "" {
		mux.HandleFunc("POST /spike/call", rt.handleSpikeCall)
	}
}

type spikeCallRequest struct {
	To         string `json:"to"`                    // E.164, e.g. +58414...
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
// body {"to": "+58..."}. 401 bad token, 400 bad body, 502 when ElevenLabs
// refuses, 200 with the ids and the request time otherwise.
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
	if !e164.MatchString(in.To) {
		http.Error(w, `"to" must be an E.164 number such as +584141234567`, http.StatusBadRequest)
		return
	}

	start := time.Now()
	placed, err := rt.client.Place(r.Context(), in.To, in.IncidentID)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("call spike: place: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
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
