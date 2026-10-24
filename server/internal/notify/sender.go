package notify

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

// sendTimeout bounds one provider request.
const sendTimeout = 15 * time.Second

// NewSender picks the channel from cfg.Notifier (the NOTIFIER env var):
// config.NotifierKapso or config.NotifierSMS. Any other value is an error, so
// a typo never sends through a channel nobody chose.
func NewSender(cfg config.Config) (Sender, error) {
	client := &http.Client{Timeout: sendTimeout}
	switch cfg.Notifier {
	case config.NotifierKapso:
		return &Kapso{
			BaseURL:       KapsoBaseURL,
			APIKey:        cfg.Kapso.APIKey,
			PhoneNumberID: cfg.Kapso.PhoneNumberID,
			Client:        client,
		}, nil
	case config.NotifierSMS:
		return newSMS(cfg, client), nil
	}
	return nil, fmt.Errorf("notify: NOTIFIER=%q is not %q or %q", cfg.Notifier, config.NotifierKapso, config.NotifierSMS)
}

// NewExtraSMS is the second channel of NOTIFY_SMS (#102), for WithSMS: the
// Twilio SMS sender, or nil when NOTIFY_SMS is off or NOTIFIER=sms already
// sends every recipient an SMS (so nobody gets the same SMS twice).
func NewExtraSMS(cfg config.Config) *SMS {
	if !cfg.NotifySMS || cfg.Notifier == config.NotifierSMS {
		return nil
	}
	return newSMS(cfg, &http.Client{Timeout: sendTimeout})
}

func newSMS(cfg config.Config, client *http.Client) *SMS {
	return &SMS{
		BaseURL:    TwilioBaseURL,
		AccountSID: cfg.Twilio.AccountSID,
		AuthToken:  cfg.Twilio.AuthToken,
		From:       cfg.Twilio.PhoneNumber,
		Client:     client,
	}
}

// errMissingConfig is wrapped when a sender lacks a credential.
var errMissingConfig = errors.New("notify: sender not configured")
