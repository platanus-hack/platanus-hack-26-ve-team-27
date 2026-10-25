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

// Transcriber starts the real-time transcription of a call in progress, so
// the report shows the conversation while it happens (#143). *TwilioStatus is
// the real one.
type Transcriber interface {
	Transcribe(ctx context.Context, callSID, callbackURL string) error
}

// TwilioStatus reads call statuses from the Twilio REST API, and starts the
// real-time transcription of a call (#143). Create it with a struct literal;
// BaseURL and Client default to TwilioBaseURL and http.DefaultClient.
type TwilioStatus struct {
	BaseURL    string // TwilioBaseURL; tests point it at an httptest.Server
	AccountSID string // TWILIO_ACCOUNT_SID
	AuthToken  string // TWILIO_AUTH_TOKEN
	Client     *http.Client
}

var (
	_ StatusReader = (*TwilioStatus)(nil)
	_ Transcriber  = (*TwilioStatus)(nil)
)

// Status fetches GET /2010-04-01/Accounts/{sid}/Calls/{callSID}.json and
// returns its status field.
func (t *TwilioStatus) Status(ctx context.Context, callSID string) (string, error) {
	raw, err := t.do(ctx, http.MethodGet, "/Calls/"+url.PathEscape(callSID)+".json", nil)
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

// TranscriptionLanguage is the language Twilio transcribes a call in. Its
// default engine picks the provider for it.
const TranscriptionLanguage = "es-US"

// Transcribe starts the real-time transcription of a call in progress:
// POST /2010-04-01/Accounts/{sid}/Calls/{callSID}/Transcriptions.json. Twilio
// then posts the final phrase of each track (inbound is the person called,
// outbound is "Mamá") to callbackURL until the call ends.
func (t *TwilioStatus) Transcribe(ctx context.Context, callSID, callbackURL string) error {
	form := url.Values{
		"StatusCallbackUrl":    {callbackURL},
		"StatusCallbackMethod": {http.MethodPost},
		"Track":                {"both_tracks"},
		"LanguageCode":         {TranscriptionLanguage},
		"PartialResults":       {"false"},
		"ProfanityFilter":      {"false"}, // what was said, word for word
	}
	_, err := t.do(ctx, http.MethodPost, "/Calls/"+url.PathEscape(callSID)+"/Transcriptions.json", form)
	return err
}

// do sends one request to the account's resource at path (form-encoded when
// form is set) and returns the body of a 2xx answer.
func (t *TwilioStatus) do(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	base := t.BaseURL
	if base == "" {
		base = TwilioBaseURL
	}
	u := strings.TrimSuffix(base, "/") + "/2010-04-01/Accounts/" + url.PathEscape(t.AccountSID) + path
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
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
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("twilio: %s: %s", resp.Status, snippet(raw))
	}
	return raw, nil
}
