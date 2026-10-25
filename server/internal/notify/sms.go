package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TwilioBaseURL is the Twilio REST API.
const TwilioBaseURL = "https://api.twilio.com"

// SMS sends a text message through the Twilio Messages API. It is the
// fallback channel, switched on with NOTIFIER=sms.
type SMS struct {
	BaseURL    string // TwilioBaseURL; tests point it at an httptest.Server
	AccountSID string // basic auth user
	AuthToken  string // basic auth password
	From       string // the Twilio number, E.164
	Client     *http.Client
}

// Send posts one SMS. Twilio takes E.164 numbers with the "+".
func (s *SMS) Send(ctx context.Context, to, body string) error {
	if s.AccountSID == "" || s.AuthToken == "" || s.From == "" {
		return fmt.Errorf("%w: TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN and TWILIO_PHONE_NUMBER are required", errMissingConfig)
	}
	form := url.Values{"To": {to}, "From": {s.From}, "Body": {body}}
	endpoint := strings.TrimSuffix(s.BaseURL, "/") + "/2010-04-01/Accounts/" + url.PathEscape(s.AccountSID) + "/Messages.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.AccountSID, s.AuthToken)
	return do(s.Client, req, "twilio")
}
