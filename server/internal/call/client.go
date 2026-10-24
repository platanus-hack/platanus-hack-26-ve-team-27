// Package call places the "Mamá" call through ElevenLabs Agents and its
// native Twilio integration, and receives the post-call webhook
// (docs/contract.md "CallWebhook").
//
// This is the #11 spike: it proves the call rings a +58 phone and measures
// the latency. The call engine (#18) builds on Client and Webhook; the
// token-gated POST /spike/call route exists only for the spike.
package call

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// BaseURL is the ElevenLabs API.
const BaseURL = "https://api.elevenlabs.io"

// outboundCallPath is the Twilio outbound-call endpoint of ElevenLabs Agents:
// https://elevenlabs.io/docs/api-reference/twilio/outbound-call
const outboundCallPath = "/v1/convai/twilio/outbound-call"

// ErrMissingConfig means a required ELEVENLABS_* value is unset.
var ErrMissingConfig = errors.New("call: missing config")

// Client places outbound calls. Create it with a struct literal; BaseURL
// and Client default to BaseURL and http.DefaultClient.
type Client struct {
	BaseURL       string // BaseURL; tests point it at an httptest.Server
	APIKey        string // sent as xi-api-key
	AgentID       string // agent_id of the "Mamá" agent
	PhoneNumberID string // agent_phone_number_id of the imported Twilio number
	// VoiceID, when set, overrides the agent's voice for the call. The agent
	// must allow the TTS voice override (Agent > Security > Overrides).
	VoiceID string
	Client  *http.Client
}

// Placed is what ElevenLabs answers when the call is placed.
type Placed struct {
	ConversationID string // matches data.conversation_id of the webhook
	CallSID        string // the Twilio Call SID, for Twilio's call logs
}

type outboundCall struct {
	AgentID            string      `json:"agent_id"`
	AgentPhoneNumberID string      `json:"agent_phone_number_id"`
	ToNumber           string      `json:"to_number"`
	ClientData         *clientData `json:"conversation_initiation_client_data,omitempty"`
}

type clientData struct {
	DynamicVariables map[string]any  `json:"dynamic_variables,omitempty"`
	ConfigOverride   *configOverride `json:"conversation_config_override,omitempty"`
}

type configOverride struct {
	TTS ttsOverride `json:"tts"`
}

type ttsOverride struct {
	VoiceID string `json:"voice_id"`
}

type outboundCallResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
	CallSID        string `json:"callSid"`
}

// Place asks ElevenLabs to call to (E.164). incidentID, when not blank, is
// sent as the incident_id dynamic variable so the webhook can find the
// incident again. A 2xx answer with success=false is an error too: that is
// how ElevenLabs reports a call Twilio refused.
func (c *Client) Place(ctx context.Context, to, incidentID string) (Placed, error) {
	if c.APIKey == "" || c.AgentID == "" || c.PhoneNumberID == "" {
		return Placed{}, fmt.Errorf("%w: ELEVENLABS_API_KEY, ELEVENLABS_AGENT_ID and ELEVENLABS_PHONE_NUMBER_ID are required", ErrMissingConfig)
	}
	body := outboundCall{AgentID: c.AgentID, AgentPhoneNumberID: c.PhoneNumberID, ToNumber: to}
	if incidentID != "" || c.VoiceID != "" {
		body.ClientData = &clientData{}
		if incidentID != "" {
			body.ClientData.DynamicVariables = map[string]any{contract.DynamicVarIncidentID: incidentID}
		}
		if c.VoiceID != "" {
			body.ClientData.ConfigOverride = &configOverride{TTS: ttsOverride{VoiceID: c.VoiceID}}
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Placed{}, err
	}
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+outboundCallPath, bytes.NewReader(payload))
	if err != nil {
		return Placed{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", c.APIKey)

	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Placed{}, fmt.Errorf("elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Provider errors never echo our key, so the body is safe to show.
		return Placed{}, fmt.Errorf("elevenlabs: %s: %s", resp.Status, snippet(raw))
	}
	var out outboundCallResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Placed{}, fmt.Errorf("elevenlabs: decode response: %w", err)
	}
	if !out.Success {
		return Placed{}, fmt.Errorf("elevenlabs: call not placed: %s", out.Message)
	}
	return Placed{ConversationID: out.ConversationID, CallSID: out.CallSID}, nil
}

// snippet trims a provider body to something short enough to log.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 512 {
		s = s[:512] + "…"
	}
	return s
}
