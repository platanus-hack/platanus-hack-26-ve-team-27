package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

func TestCheckEphemeralKeys(t *testing.T) {
	both := []string{"EMERGENCY_ED25519_SEED", "EMERGENCY_X25519_PRIVATE_KEY"}
	cases := []struct {
		name      string
		ephemeral []string
		onRender  bool
		wantErr   bool
	}{
		{"local, both unset", both, false, false},
		{"local, all set", nil, false, false},
		{"render, all set", nil, true, false},
		{"render, both unset", both, true, true},
		{"render, seed unset", both[:1], true, true},
		{"render, x25519 unset", both[1:], true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkEphemeralKeys(c.ephemeral, c.onRender)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if err == nil {
				return
			}
			for _, name := range c.ephemeral {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error %q does not name %s", err, name)
				}
			}
			for _, name := range both {
				if !slices.Contains(c.ephemeral, name) && strings.Contains(err.Error(), name) {
					t.Errorf("error %q names %s, which is set", err, name)
				}
			}
		})
	}
}

// With the flag off nothing is loaded: no key is read and, on Render, its
// absence does not exit (a fatal would end the test process).
func TestLoadOfflineEmergencyOffNeedsNoKeys(t *testing.T) {
	for _, onRender := range []bool{false, true} {
		if got := loadOfflineEmergency(config.LoadFrom(func(string) string { return "" }), onRender); got != nil {
			t.Errorf("onRender=%v: flag unset returned %+v, want nil", onRender, got)
		}
	}
}

func TestSMSProviders(t *testing.T) {
	const token = "test-auth-token" // secret-scan:allow (test fixture)
	cases := []struct {
		name       string
		tw         config.Twilio
		wantTwilio bool
		wantErr    bool
	}{
		{"both set", config.Twilio{AuthToken: token, WebhookBaseURL: "https://mama.example.com"}, true, false},
		{"no base URL", config.Twilio{AuthToken: token}, false, false},
		{"no token", config.Twilio{WebhookBaseURL: "https://mama.example.com"}, false, false},
		{"nothing set", config.Twilio{}, false, false},
		{"base URL not a URL", config.Twilio{AuthToken: token, WebhookBaseURL: "mama.test"}, false, true},
		{"base URL with a path", config.Twilio{AuthToken: token, WebhookBaseURL: "https://mama.test/sms/inbound/twilio"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := smsProviders(c.tw)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "TWILIO_WEBHOOK_BASE_URL") || strings.Contains(err.Error(), c.tw.WebhookBaseURL) {
					t.Fatalf("error %q must name the variable, not its value", err)
				}
				return
			}
			if _, ok := got["twilio"]; ok != c.wantTwilio {
				t.Fatalf("twilio registered = %v, want %v", ok, c.wantTwilio)
			}
		})
	}
}
