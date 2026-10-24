package main

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

func TestRootReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), testOffline(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntakeRoutesAreMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), testOffline(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/incidents", nil))

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

func testKeys(t *testing.T) serverkeys.Keys {
	t.Helper()
	k, err := serverkeys.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func testOffline(t *testing.T) *offlineEmergency {
	t.Helper()
	return &offlineEmergency{keys: testKeys(t)}
}

func TestConfigRouteIsMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), testOffline(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /config with no database = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestEmergencyRoutesAreMounted(t *testing.T) {
	mux := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), testOffline(t))
	for _, path := range []string{"/emergencies", "/emergencies/relay"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("MAMA1 x")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("POST %s = %d, want 503", path, rec.Code)
		}
	}
}

func TestInboundSMSRouteIsMounted(t *testing.T) {
	mux := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), testOffline(t))
	for path, want := range map[string]int{
		"/sms/inbound/twilio": http.StatusServiceUnavailable, // mounted, no database
		"/sms/inbound/nope":   http.StatusNotFound,           // mounted, unknown provider
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("Body=x")))
		if rec.Code != want {
			t.Errorf("POST %s = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestCallWebhookIsMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", strings.NewReader(`{"type":"post_call_audio"}`))
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), testOffline(t)).ServeHTTP(rec, req)

	// Mounted, and with no secret configured it fails closed.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /webhooks/elevenlabs = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestOfflineEmergencyRoutesAreNotMountedWhenOff(t *testing.T) {
	mux := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/config"},
		{http.MethodPost, "/emergencies"},
		{http.MethodPost, "/emergencies/relay"},
		{http.MethodPost, "/sms/inbound/twilio"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader("MAMA1 x")))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s with the flag off = %d, want 404", c.method, c.path, rec.Code)
		}
	}

	// The rest of the API is unaffected.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/incidents", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /incidents with the flag off = %d, want 503", rec.Code)
	}
}

func TestDemoPanelRoute(t *testing.T) {
	get := func(off *offlineEmergency, user, pass string, auth bool) int {
		req := httptest.NewRequest(http.MethodGet, "/demo/emergencies", nil)
		if auth {
			req.SetBasicAuth(user, pass)
		}
		rec := httptest.NewRecorder()
		newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.ElevenLabs{}, nil), off).ServeHTTP(rec, req)
		return rec.Code
	}
	withToken := testOffline(t)
	withToken.demoPanelToken = "s3cret"

	if got := get(testOffline(t), "", "s3cret", true); got != http.StatusNotFound {
		t.Errorf("flag on, token unset = %d, want 404", got)
	}
	if got := get(nil, "", "s3cret", true); got != http.StatusNotFound {
		t.Errorf("flag off = %d, want 404", got)
	}
	if got := get(withToken, "", "", false); got != http.StatusUnauthorized {
		t.Errorf("mounted, no credentials = %d, want 401", got)
	}
	if got := get(withToken, "jury", "wrong", true); got != http.StatusUnauthorized {
		t.Errorf("mounted, wrong token = %d, want 401", got)
	}
	if got := get(withToken, "jury", "s3cret", true); got != http.StatusServiceUnavailable {
		t.Errorf("mounted, right token, no database = %d, want 503", got)
	}
}
