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

// Hanger ends a placed call. *TwilioStatus is the real one.
type Hanger interface {
	// Hangup cancels a call that is queued or ringing and ends one in
	// progress. A call that already ended is no error.
	Hangup(ctx context.Context, callSID string) error
}

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.callURL(callSID), nil)
	if err != nil {
		return "", err
	}
	raw, err := t.do(req)
	if err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("twilio: decode call: %w", err)
	}
	return out.Status, nil
}

// Hangup stops the call: it reads the status, then POSTs Status=canceled to a
// call that is queued, initiated or ringing and Status=completed to one in
// progress (https://www.twilio.com/docs/voice/api/call-resource#update-a-call-resource).
// Any other status means the call already ended, and nothing is sent.
func (t *TwilioStatus) Hangup(ctx context.Context, callSID string) error {
	status, err := t.Status(ctx, callSID)
	if err != nil {
		return err
	}
	var target string
	switch status {
	case TwilioQueued, TwilioInitiated, TwilioRinging:
		target = TwilioCanceled
	case TwilioInProgress:
		target = TwilioCompleted
	default:
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.callURL(callSID), strings.NewReader(url.Values{"Status": {target}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = t.do(req)
	return err
}

// callURL is /2010-04-01/Accounts/{sid}/Calls/{callSID}.json.
func (t *TwilioStatus) callURL(callSID string) string {
	base := t.BaseURL
	if base == "" {
		base = TwilioBaseURL
	}
	return strings.TrimSuffix(base, "/") + "/2010-04-01/Accounts/" + url.PathEscape(t.AccountSID) + "/Calls/" + url.PathEscape(callSID) + ".json"
}

// do sends req with the account's basic auth and returns the body of a 2xx
// answer.
func (t *TwilioStatus) do(req *http.Request) ([]byte, error) {
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("twilio: %s: %s", resp.Status, snippet(raw))
	}
	return raw, nil
}
