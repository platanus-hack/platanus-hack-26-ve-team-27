// Package call places the "Mamá" call through ElevenLabs Agents and its
// native Twilio integration, and receives the post-call webhook
// (docs/contract.md "CallWebhook").
//
// The call engine (#18, Engine) takes a BranchCall incident from intake,
// inserts the calls row, places the call with Client and stores the
// conversation_id; the webhook stores the transcript on that row (Store).
// A call that fails or is not answered is retried once; then the WebRTC
// fallback page GET /call/fallback shows the incident (#23, Fallback). Each
// incident logs one "call timing:" line with its event-to-ring time, read
// from Twilio's call status (TwilioStatus). CALL_FORCE_FAILURE fails every
// call without dialing, to rehearse the fallback.
// The token-gated POST /spike/call route is left from the #11 spike.
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
	// VoiceID, when set, overrides the agent's voice for a call whose
	// Call.VoiceID is blank: the global ELEVENLABS_VOICE_ID. The agent must
	// allow the TTS voice override (Agent > Security > Overrides).
	VoiceID string
	Client  *http.Client
}

// Placed is what ElevenLabs answers when the call is placed.
type Placed struct {
	ConversationID string // matches data.conversation_id of the webhook
	CallSID        string // the Twilio Call SID, for Twilio's call logs
}

// Call is one outbound call.
type Call struct {
	To string // E.164
	// Variables are the agent's dynamic variables. The engine always sends
	// every one CallVariables returns: ElevenLabs refuses a conversation
	// whose prompt names a missing one.
	Variables map[string]string
	// VoiceID is the device's own voice; blank falls back to Client.VoiceID,
	// and with both blank the agent's own voice speaks.
	VoiceID string
}

type outboundCall struct {
	AgentID            string      `json:"agent_id"`
	AgentPhoneNumberID string      `json:"agent_phone_number_id"`
	ToNumber           string      `json:"to_number"`
	ClientData         *clientData `json:"conversation_initiation_client_data,omitempty"`
}

type clientData struct {
	DynamicVariables map[string]string `json:"dynamic_variables,omitempty"`
	ConfigOverride   *configOverride   `json:"conversation_config_override,omitempty"`
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

// Place asks ElevenLabs to make the call, with its dynamic variables (the
// incident_id among them lets the webhook find the incident again) and its
// voice. A 2xx answer with success=false is an error too: that is how
// ElevenLabs reports a call Twilio refused. Errors never quote the phrases.
func (c *Client) Place(ctx context.Context, call Call) (Placed, error) {
	if c.APIKey == "" || c.AgentID == "" || c.PhoneNumberID == "" {
		return Placed{}, fmt.Errorf("%w: ELEVENLABS_API_KEY, ELEVENLABS_AGENT_ID and ELEVENLABS_PHONE_NUMBER_ID are required", ErrMissingConfig)
	}
	body := outboundCall{AgentID: c.AgentID, AgentPhoneNumberID: c.PhoneNumberID, ToNumber: call.To}
	voice := call.VoiceID
	if voice == "" {
		voice = c.VoiceID
	}
	if len(call.Variables) > 0 || voice != "" {
		body.ClientData = &clientData{}
		if len(call.Variables) > 0 {
			body.ClientData.DynamicVariables = call.Variables
		}
		if voice != "" {
			body.ClientData.ConfigOverride = &configOverride{TTS: ttsOverride{VoiceID: voice}}
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
		// Provider errors never echo our key; a validation error may echo the
		// request, so the phrases are cut out.
		return Placed{}, fmt.Errorf("elevenlabs: %s: %s", resp.Status, redactPhrases(snippet(raw), call.Variables))
	}
	var out outboundCallResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Placed{}, fmt.Errorf("elevenlabs: decode response: %w", err)
	}
	if !out.Success {
		return Placed{}, fmt.Errorf("elevenlabs: call not placed: %s", redactPhrases(out.Message, call.Variables))
	}
	return Placed{ConversationID: out.ConversationID, CallSID: out.CallSID}, nil
}

// redactPhrases replaces the duress and safe phrases in s, raw or JSON
// escaped, so a provider error that echoes the request never logs them.
func redactPhrases(s string, vars map[string]string) string {
	for _, k := range []string{contract.DynamicVarDuressPhrase, contract.DynamicVarSafePhrase} {
		v := vars[k]
		if v == "" {
			continue
		}
		s = strings.ReplaceAll(s, v, "[redacted]")
		if quoted, err := json.Marshal(v); err == nil {
			s = strings.ReplaceAll(s, strings.Trim(string(quoted), `"`), "[redacted]")
		}
	}
	return s
}

// snippet trims a provider body to something short enough to log.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 512 {
		s = s[:512] + "…"
	}
	return s
}
