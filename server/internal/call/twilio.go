package call

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TwilioBaseURL is the Twilio REST API.
const TwilioBaseURL = "https://api.twilio.com"

// Twilio call statuses (https://www.twilio.com/docs/voice/api/call-resource#call-status-values).
const (
	TwilioQueued     = "queued"
	TwilioInitiated  = "initiated"
	TwilioRinging    = "ringing"
	TwilioInProgress = "in-progress"
	TwilioCompleted  = "completed"
	TwilioBusy       = "busy"
	TwilioNoAnswer   = "no-answer"
	TwilioFailed     = "failed"
	TwilioCanceled   = "canceled"
)

// StatusReader reads the Twilio status of a call. *TwilioStatus is the real
// one. ElevenLabs places the call on our Twilio number but sends no ringing
// event, so the engine polls Twilio to time the ring and to see a busy or
// unanswered phone.
type StatusReader interface {
	Status(ctx context.Context, callSID string) (string, error)
}

// TwilioStatus reads call statuses from the Twilio REST API. Create it with
// a struct literal; BaseURL and Client default to TwilioBaseURL and
// http.DefaultClient.
type TwilioStatus struct {
	BaseURL    string // TwilioBaseURL; tests point it at an httptest.Server
	AccountSID string // TWILIO_ACCOUNT_SID
	AuthToken  string // TWILIO_AUTH_TOKEN
	Client     *http.Client
}

// Status fetches GET /2010-04-01/Accounts/{sid}/Calls/{callSID}.json and
// returns its status field.
func (t *TwilioStatus) Status(ctx context.Context, callSID string) (string, error) {
	base := t.BaseURL
	if base == "" {
		base = TwilioBaseURL
	}
	u := strings.TrimSuffix(base, "/") + "/2010-04-01/Accounts/" + url.PathEscape(t.AccountSID) + "/Calls/" + url.PathEscape(callSID) + ".json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("twilio: %s: %s", resp.Status, snippet(raw))
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("twilio: decode call: %w", err)
	}
	return out.Status, nil
}
