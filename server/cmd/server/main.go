// Command server is the MAMÁ backend HTTP server.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/remoteconfig"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/sms"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/notify"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// migrateTimeout bounds the boot migration, lock wait included.
const migrateTimeout = 60 * time.Second

func main() {
	cfg := config.Load()
	if missing := cfg.Missing(); len(missing) > 0 {
		// Names only, never values.
		log.Printf("config: not set: %s", strings.Join(missing, ", "))
	}

	intake.SetCallWhenStill(cfg.CallWhenStill)
	if cfg.CallWhenStill {
		log.Print("intake: CALL_WHEN_STILL is on: a still phone gets the call branch")
	}

	offline := loadOfflineEmergency(cfg, os.Getenv("RENDER") != "")

	pool := openAndMigrate(cfg.DatabaseURL)
	notifier := newNotifier(cfg, pool)
	reports, closer := newReports(cfg, pool, reportSigningKey(cfg, offline))
	calls := newCallRoutes(cfg, pool)
	// The post-call hooks, in order (call.Hooks). #21 adds "duress detector
	// -> save verdict -> notify" first, so the report closes after them.
	calls.SetFinished(call.Hooks(
		reportHook(closer),
	))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMux(pool, notifier, calls, offline, reports),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("mama server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// loadOfflineEmergency returns the offline emergency channel, or nil when
// EMERGENCY_OFFLINE_ENABLED is off: then no key is read or required and the
// SMS number is not checked. Enabled, a bad SMS number or webhook base URL
// exits, the keys load through loadServerKeys and the SMS providers through
// smsProviders.
func loadOfflineEmergency(cfg config.Config, onRender bool) *offlineEmergency {
	if !cfg.Emergency.Enabled {
		log.Print("emergency: offline channel is off (EMERGENCY_OFFLINE_ENABLED is not true)")
		return nil
	}
	keys := loadServerKeys(cfg, onRender)
	if remoteconfig.CheckSMSNumber(cfg.Emergency.SMSNumber) != nil {
		log.Fatalf("emergency: EMERGENCY_SMS_NUMBER must be E.164 (+ and 7-15 digits)")
	}
	providers, err := smsProviders(cfg.Twilio)
	if err != nil {
		log.Fatalf("emergency: %v", err)
	}
	return &offlineEmergency{keys: keys, smsNumber: cfg.Emergency.SMSNumber, smsProviders: providers}
}

// smsProviders returns the inbound SMS adapters, keyed by the {provider}
// segment of POST /sms/inbound/{provider}. Twilio needs TWILIO_AUTH_TOKEN and
// TWILIO_WEBHOOK_BASE_URL, since an unsigned webhook is never accepted:
// without either it is left out (its webhook answers 404) and the log names
// what is missing. A malformed base URL is an error naming the variable,
// never its value.
func smsProviders(tw config.Twilio) (map[string]sms.InboundSMSProvider, error) {
	providers := map[string]sms.InboundSMSProvider{}
	var missing []string
	if tw.AuthToken == "" {
		missing = append(missing, "TWILIO_AUTH_TOKEN")
	}
	if tw.WebhookBaseURL == "" {
		missing = append(missing, "TWILIO_WEBHOOK_BASE_URL")
	}
	if len(missing) > 0 {
		log.Printf("emergency: SMS webhook POST /sms/inbound/twilio is off: %s not set", strings.Join(missing, " and "))
		return providers, nil
	}
	twilio, err := sms.NewTwilio(tw.AuthToken, tw.WebhookBaseURL)
	if err != nil {
		return nil, err
	}
	providers["twilio"] = twilio
	return providers, nil
}

// loadServerKeys builds the offline-emergency key pair. A malformed key exits
// (naming the variable, never its value). An unset one exits on Render, where
// an ephemeral key would silently break every phone's pinned key on each
// deploy; locally it warns and uses an ephemeral key.
func loadServerKeys(cfg config.Config, onRender bool) serverkeys.Keys {
	keys, ephemeral, err := serverkeys.FromConfig(cfg.Emergency, rand.Reader)
	if err != nil {
		log.Fatalf("emergency: %v", err)
	}
	if err := checkEphemeralKeys(ephemeral, onRender); err != nil {
		log.Fatalf("emergency: %v", err)
	}
	for _, name := range ephemeral {
		log.Printf("emergency: WARNING: %s is not set; using an ephemeral key (dev only): phones' cached server key breaks on every restart", name)
	}
	return keys
}

// checkEphemeralKeys refuses ephemeral server keys in production. onRender is
// whether the RENDER env var is set (Render sets it on every service);
// ephemeral lists the unset key variables. The error names them, never values.
func checkEphemeralKeys(ephemeral []string, onRender bool) error {
	if !onRender || len(ephemeral) == 0 {
		return nil
	}
	return fmt.Errorf("%s not set: production needs both server keys (README \"Secrets\")", strings.Join(ephemeral, " and "))
}

// openAndMigrate opens the pool and applies pending migrations, so every
// deploy migrates its database. With no DATABASE_URL it returns nil and the
// server boots without a database. A failed migration exits: Render then keeps
// the previous deploy running instead of serving a half-migrated schema.
func openAndMigrate(url string) *sql.DB {
	pool, err := db.Open(url)
	switch {
	case err != nil:
		log.Fatalf("database: %v", err)
	case pool == nil:
		log.Print("database: DATABASE_URL is not set; skipping migrations, /health will return 503")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	if len(applied) == 0 {
		log.Print("database: schema up to date")
	} else {
		log.Printf("database: applied migrations %s", strings.Join(applied, ", "))
	}
	return pool
}

// newNotifier builds the family notifier on the channel NOTIFIER picks. An
// unknown NOTIFIER exits, like a failed migration: Render keeps the previous
// deploy instead of alerting through a channel nobody chose. Without a
// database intake answers 503 and never hands off, so LogHandoff is enough.
func newNotifier(cfg config.Config, pool *sql.DB) intake.Notifier {
	sender, err := notify.NewSender(cfg)
	if err != nil {
		log.Fatalf("notifier: %v", err)
	}
	if pool == nil {
		return intake.LogHandoff{}
	}
	log.Printf("notifier: sending through %s", cfg.Notifier)
	if !channelConfigured(cfg) {
		log.Printf("notifier: WARNING: %s credentials are not set; every family alert will fail", cfg.Notifier)
	}
	return notify.New(notify.NewDBStore(pool), sender)
}

// newCallRoutes builds the call engine on the database. Without one the
// webhook only logs and intake never hands off a call.
func newCallRoutes(cfg config.Config, pool *sql.DB) *call.Routes {
	if pool == nil {
		return call.NewRoutes(cfg, nil)
	}
	return call.NewRoutes(cfg, call.NewDBStore(pool))
}

// newReports builds the incident report page (#76) and the closer that
// stores each call's recording and seals the report with signing (nil: no
// seal, the page says "Sin sellar"). Both are nil when
// INCIDENT_REPORT_ENABLED is off or there is no database: then GET
// /i/{token} answers 404 and finished calls are left alone. Nothing about
// the report ever stops the boot.
func newReports(cfg config.Config, pool *sql.DB, signing ed25519.PrivateKey) (*report.Handler, *report.Closer) {
	if !cfg.Report.Enabled {
		log.Print("report: incident report page is off (INCIDENT_REPORT_ENABLED is not true)")
		return nil, nil
	}
	if pool == nil {
		log.Print("report: WARNING: INCIDENT_REPORT_ENABLED is on but DATABASE_URL is not set; GET /i/{token} answers 404")
		return nil, nil
	}
	if cfg.PublicURL == "" {
		log.Print("report: WARNING: PUBLIC_URL (or RENDER_EXTERNAL_URL) is not set; report links will be bare paths")
	}
	store := report.NewDBStore(pool)
	closer := &report.Closer{Store: store, Signer: signing}
	if cfg.ElevenLabs.APIKey != "" {
		closer.Audio = &report.ElevenLabsAudio{APIKey: cfg.ElevenLabs.APIKey}
	} else {
		log.Print("report: WARNING: ELEVENLABS_API_KEY is not set; call recordings are not stored")
	}
	if signing == nil {
		log.Print("report: WARNING: no server signing key; reports stay unsealed (\"Sin sellar\")")
	}
	log.Printf("report: incident report page is on; links live %d days", cfg.Report.TTLDays)
	h := report.NewHandler(store, report.Options{Channel: channelName(cfg), CanSeal: signing != nil}).RecoverWith(closer)
	return h, closer
}

// reportSigningKey is the key that seals reports: the offline channel's when
// it is on (the key GET /config publishes; locally it may be ephemeral),
// else EMERGENCY_ED25519_SEED. Unset or malformed gives nil and a warning,
// never an exit: the report works unsealed. No key is generated here, so
// Render never seals with an ephemeral key.
func reportSigningKey(cfg config.Config, offline *offlineEmergency) ed25519.PrivateKey {
	if !cfg.Report.Enabled {
		return nil
	}
	if offline != nil {
		return offline.keys.Signing
	}
	key, err := serverkeys.SigningKey(cfg.Emergency)
	if err != nil {
		log.Printf("report: WARNING: %v; reports stay unsealed", err)
		return nil
	}
	if key == nil {
		log.Print("report: WARNING: EMERGENCY_ED25519_SEED is not set; reports stay unsealed")
	}
	return key
}

// reportHook is the post-call hook that closes the report: it stores the
// recording and seals, in the background. nil without a closer.
func reportHook(c *report.Closer) func(context.Context, call.Finished) {
	if c == nil {
		return nil
	}
	return func(ctx context.Context, f call.Finished) {
		c.CallFinished(ctx, f.IncidentID, f.ConversationID, f.Status)
	}
}

// channelName is how the report names the family message channel.
func channelName(cfg config.Config) string {
	if cfg.Notifier == config.NotifierSMS {
		return "SMS"
	}
	return "WhatsApp"
}

// channelConfigured reports whether the NOTIFIER channel has its credentials,
// so a misconfigured deploy says so at boot instead of at the first alert.
func channelConfigured(cfg config.Config) bool {
	if cfg.Notifier == config.NotifierSMS {
		return cfg.Twilio.AccountSID != "" && cfg.Twilio.AuthToken != "" && cfg.Twilio.PhoneNumber != ""
	}
	return cfg.Kapso.APIKey != "" && cfg.Kapso.PhoneNumberID != ""
}
