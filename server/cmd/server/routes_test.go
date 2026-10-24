package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
)

func TestRootReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntakeRoutesAreMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/incidents", nil))

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
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /config with no database = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestEmergencyRoutesAreMounted(t *testing.T) {
	mux := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil)
	for _, path := range []string{"/emergencies", "/emergencies/relay"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("MAMA1 x")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("POST %s = %d, want 503", path, rec.Code)
		}
	}
}

func TestSMSWebhookIsMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sms/inbound/twilio", strings.NewReader("Body=MAMA1+x"))
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /sms/inbound/twilio with no database = %d, want 503", rec.Code)
	}
}

func TestCallWebhookIsMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", strings.NewReader(`{"type":"post_call_audio"}`))
	newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil).ServeHTTP(rec, req)

	// Mounted, and with no secret configured it fails closed.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /webhooks/elevenlabs = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestOfflineEmergencyRoutesAreNotMountedWhenOff(t *testing.T) {
	mux := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), nil, nil)
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

// stubReports serves no record: every token is unknown.
type stubReports struct{}

func (stubReports) ByToken(context.Context, string) (report.Record, error) {
	return report.Record{}, report.ErrNotFound
}

func (stubReports) Audio(context.Context, string, int) ([]byte, report.Recording, error) {
	return nil, report.Recording{}, report.ErrNotFound
}

func TestReportRoutesOnlyWhenEnabledWithADatabase(t *testing.T) {
	if h, c := newReports(config.Config{}, nil, nil); h != nil || c != nil {
		t.Error("newReports with INCIDENT_REPORT_ENABLED off is not nil")
	}
	if h, c := newReports(config.Config{Report: config.Report{Enabled: true, TTLDays: 30}}, nil, nil); h != nil || c != nil {
		t.Error("newReports without a database is not nil")
	}
	if reportHook(nil) != nil {
		t.Error("reportHook(nil) is not nil")
	}

	off := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), nil, nil)
	for _, path := range []string{"/i/AAAAAAAAAAAAAAAAAAAAAA", "/ficha/static/ficha.css"} {
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("off: GET %s = %d, want 404", path, rec.Code)
		}
	}

	on := newMux(nil, intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), nil, report.NewHandler(stubReports{}, report.Options{}))
	rec := httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ficha/static/ficha.css", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("on: GET /ficha/static/ficha.css = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/AAAAAAAAAAAAAAAAAAAAAA", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("on: GET unknown token = %d (Referrer-Policy %q), want the report's 404", rec.Code, rec.Header().Get("Referrer-Policy"))
	}
}

func TestChannelName(t *testing.T) {
	if channelName(config.Config{Notifier: config.NotifierSMS}) != "SMS" || channelName(config.Config{Notifier: config.NotifierKapso}) != "WhatsApp" {
		t.Error("channelName")
	}
	if channelName(config.Config{Notifier: config.NotifierKapso, NotifySMS: true}) != "WhatsApp y SMS" || channelName(config.Config{Notifier: config.NotifierSMS, NotifySMS: true}) != "SMS" {
		t.Error("channelName with NOTIFY_SMS")
	}
}

// The report's key never stops the boot: unset or malformed gives no key
// (unsealed reports), and with the offline channel on it is that key.
func TestReportSigningKey(t *testing.T) {
	on := config.Config{Report: config.Report{Enabled: true}}
	if reportSigningKey(on, nil) != nil {
		t.Error("no seed: want no key")
	}
	bad := on
	bad.Emergency.Ed25519Seed = "not base64"
	if reportSigningKey(bad, nil) != nil {
		t.Error("malformed seed: want no key, not an exit")
	}
	good := on
	good.Emergency.Ed25519Seed = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	if k := reportSigningKey(good, nil); k == nil {
		t.Error("seed set: want a key")
	}
	if reportSigningKey(config.Config{Emergency: good.Emergency}, nil) != nil {
		t.Error("report off: want no key")
	}
	offline := testOffline(t)
	if k := reportSigningKey(on, offline); !k.Equal(offline.keys.Signing) {
		t.Error("offline channel on: want its key, the one GET /config publishes")
	}
}

func TestReportLinksOnlyWithThePageAndAPublicURL(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := config.Config{PublicURL: "https://mama.example.com", Report: config.Report{Enabled: true, TTLDays: 30}}
	if reportLinks(cfg, pool, false) != nil || reportLinks(cfg, nil, true) != nil {
		t.Error("links without the report page or a database")
	}
	noURL := cfg
	noURL.PublicURL = ""
	if reportLinks(noURL, pool, true) != nil {
		t.Error("links without a public URL")
	}
	if l, ok := reportLinks(cfg, pool, true).(report.Links); !ok || l.BaseURL != "https://mama.example.com" || l.TTLDays != 30 {
		t.Errorf("links = %#v", reportLinks(cfg, pool, true))
	}
}
