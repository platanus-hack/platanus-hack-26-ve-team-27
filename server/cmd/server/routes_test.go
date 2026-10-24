package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

func TestRootReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntakeRoutesAreMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/incidents", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /incidents with no database = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestChannelConfigured(t *testing.T) {
	kapso := config.Config{Notifier: config.NotifierKapso, Kapso: config.Kapso{APIKey: "k", PhoneNumberID: "1"}}
	sms := config.Config{Notifier: config.NotifierSMS, Twilio: config.Twilio{AccountSID: "AC", AuthToken: "t", PhoneNumber: "+1"}}
	if !channelConfigured(kapso) || !channelConfigured(sms) {
		t.Error("fully configured channels reported unconfigured")
	}
	kapso.Kapso.APIKey, sms.Twilio.AuthToken = "", ""
	if channelConfigured(kapso) || channelConfigured(sms) {
		t.Error("channels missing a credential reported configured")
	}
}

func TestCallWebhookIsMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", strings.NewReader(`{"type":"post_call_audio"}`))
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{})).ServeHTTP(rec, req)

	// Mounted, and with no secret configured it fails closed.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /webhooks/elevenlabs = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
