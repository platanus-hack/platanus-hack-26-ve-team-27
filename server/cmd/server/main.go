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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/remoteconfig"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/sms"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/notify"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/ratelimit"
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

	offline := loadOfflineEmergency(cfg, os.Getenv("RENDER") != "")

	pool := openAndMigrate(cfg.DatabaseURL)
	reports, closer := newReports(cfg, pool, reportSigningKey(cfg, offline))
	notifier := newNotifier(cfg, pool, reportLinks(cfg, pool, reports != nil), deliveryLog(cfg, pool, reports != nil))
	calls := newCallRoutes(cfg, pool)
	// The post-call hooks, in order (call.Hooks). #21 adds "duress detector
	// -> save verdict -> notify" first, so the report closes after them.
	calls.SetFinished(call.Hooks(
		verdictHook(cfg, pool, notifier),
		reportHook(closer),
	))
	calls.SetProgress(progressHook(cfg, pool))
	calls.SetTranscription(transcriptionURL(cfg, pool))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMux(pool, notifier, calls, offline, reports),
		ReadHeaderTimeout: 10 * time.Second,
		// A phone on a weak signal still uploads any request body in this time;
		// a client trickling bytes cannot hold a connection forever.
		ReadTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	// Render sends SIGTERM on every deploy and kills the process 30 s later.
	stop, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	go func() {
		log.Printf("mama server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-stop.Done()
	shutdown(srv, shutdownTimeout, requestDrain)
}

const (
	// shutdownTimeout leaves a margin inside Render's 30 s between SIGTERM and SIGKILL.
	shutdownTimeout = 25 * time.Second
	// requestDrain is the part of it in-flight requests get. Every request
	// answers well within it; what is still open after it is a stream
	// (Server-Sent Events), which never goes idle on its own and would
	// otherwise spend the handoffs' time.
	requestDrain = 10 * time.Second
)

// shutdown stops taking requests, lets the in-flight ones answer within drain
// and closes what is still open, then waits for the handoffs they started
// until total: a call being placed or an alert being sent finishes instead of
// being cut by a deploy.
func shutdown(srv *http.Server, total, drain time.Duration) {
	log.Print("shutting down: draining requests and handoffs")
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()
	drainCtx, cancelDrain := context.WithTimeout(ctx, drain)
	defer cancelDrain()
	if err := srv.Shutdown(drainCtx); err != nil {
		// A closed stream makes the browser reconnect, to the new instance.
		log.Printf("shutdown: closing connections still open: %v", err)
		_ = srv.Close()
	}
	if err := intake.WaitHandoffs(ctx); err != nil {
		log.Printf("shutdown: handoffs still running: %v", err)
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
// links, when not nil, adds the incident report line to every message.
func newNotifier(cfg config.Config, pool *sql.DB, links notify.ReportLinks, deliveries notify.DeliveryLog) intake.Notifier {
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
	n := notify.New(notify.NewDBStore(pool), sender)
	limit := &ratelimit.Limiter{PerNumber: cfg.Limits.MessagesPerNumber, Total: cfg.Limits.MessagesTotal, Window: config.LimitWindow}
	log.Printf("notifier: sending at most %s (NOTIFY_LIMIT_PER_NUMBER, NOTIFY_LIMIT_TOTAL)", limit)
	n.WithLimit(limit)
	if links != nil {
		log.Print("notifier: family messages carry the incident report link")
		n.WithReportLinks(links)
	}
	if deliveries != nil {
		n.WithDeliveries(deliveries)
	}
	if cfg.NotifySelf {
		log.Print("notifier: NOTIFY_SELF is on; the reporting phone also gets the alert (demo only)")
		n.WithSelf()
	}
	return n
}

// deliveryLog records each family message for the report (#76): only when
// the report page is mounted. A nil interface otherwise.
func deliveryLog(cfg config.Config, pool *sql.DB, reportsOn bool) notify.DeliveryLog {
	if !reportsOn || pool == nil {
		return nil
	}
	channel := "whatsapp"
	if cfg.Notifier == config.NotifierSMS {
		channel = "sms"
	}
	return report.DeliveryLog{Store: report.NewDBStore(pool), Channel: channel}
}

// reportLinks is the family message's report link (#76): only when the
// report page is mounted (reportsOn: enabled, with a database) and the
// server knows its public URL. A nil interface otherwise.
func reportLinks(cfg config.Config, pool *sql.DB, reportsOn bool) notify.ReportLinks {
	if !reportsOn || pool == nil || cfg.PublicURL == "" {
		return nil
	}
	return report.Links{Store: report.NewDBStore(pool), BaseURL: cfg.PublicURL, TTLDays: cfg.Report.TTLDays}
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
	if cfg.Report.MapKey == "" {
		log.Print("report: CARTO_BASEMAPS_KEY is not set; the route is drawn without a map")
	}
	h := report.NewHandler(store, report.Options{Channel: channelName(cfg), CanSeal: signing != nil, BaseURL: cfg.PublicURL, MapKey: cfg.Report.MapKey}).RecoverWith(closer)
	if cfg.Kapso.WebhookSecret != "" {
		log.Print("report: Kapso message statuses arrive at POST /webhooks/kapso")
		h.WithKapso(&report.KapsoWebhook{Secret: cfg.Kapso.WebhookSecret, Store: store})
	} else {
		log.Print("report: KAPSO_WEBHOOK_SECRET is not set; the report shows each family message as sent, without delivery or read receipts")
	}
	if u := transcriptionURL(cfg, pool); u != "" {
		log.Printf("report: the call is transcribed live: Twilio posts each phrase to %s", u)
		h.WithTranscription(&report.TranscriptionWebhook{AuthToken: cfg.Twilio.AuthToken, URL: u, Store: store})
	} else if cfg.Report.LiveTranscription {
		log.Print("report: WARNING: LIVE_TRANSCRIPTION_ENABLED is on but TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN or PUBLIC_URL is not set; the conversation shows after the call only")
	}
	if cfg.Report.DemoKey != "" {
		log.Print("report: demo shortcuts GET /demo/latest and /demo/next are on (DEMO_KEY is set)")
		h.WithDemo(&report.Demo{Key: cfg.Report.DemoKey, Store: store, TTLDays: cfg.Report.TTLDays})
	}
	h.WithCentral(central(cfg, store))
	return h, closer
}

// central is the central dashboard (#99), GET /central/: every incident,
// live. It is on only with CENTRAL_KEY, or with CENTRAL_PUBLIC=true for a
// dashboard with no key (and the report, which builds it); nil otherwise, and
// every /central route answers 404.
func central(cfg config.Config, store report.CentralStore) *report.Central {
	if cfg.Report.CentralPublic {
		log.Print("report: WARNING: central dashboard GET /central/ is PUBLIC (CENTRAL_PUBLIC): anyone with the URL sees every incident, its position and its recording")
		return &report.Central{Public: true, Store: store, TTLDays: cfg.Report.TTLDays}
	}
	if cfg.Report.CentralKey == "" {
		log.Print("report: central dashboard GET /central/ is off (CENTRAL_KEY is not set, CENTRAL_PUBLIC is not true)")
		return nil
	}
	log.Print("report: central dashboard GET /central/ is on: open /central/?key=<CENTRAL_KEY> once on each screen")
	return &report.Central{Key: cfg.Report.CentralKey, Store: store, TTLDays: cfg.Report.TTLDays}
}

// progressHook stores what the call engine sees polling Twilio (ringing,
// answered) for the report's timeline and latencies; nil when the report is
// off or there is no database.
func progressHook(cfg config.Config, pool *sql.DB) func(context.Context, int64, string, time.Time) {
	if !cfg.Report.Enabled || pool == nil {
		return nil
	}
	store := report.NewDBStore(pool)
	return func(ctx context.Context, callID int64, status string, at time.Time) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := store.SaveProgress(ctx, callID, status, at); err != nil {
			log.Printf("report: call %d: store Twilio status %s: %v", callID, status, err)
		}
	}
}

// transcriptionURL is where Twilio posts the live transcription of a call
// (#143), or "" when it is off: it needs LIVE_TRANSCRIPTION_ENABLED, the
// report and its database, the Twilio account, and a public origin
// (TWILIO_WEBHOOK_BASE_URL, else PUBLIC_URL). The call engine starts the
// transcription with it and the report checks Twilio's signature over it.
func transcriptionURL(cfg config.Config, pool *sql.DB) string {
	if !cfg.Report.LiveTranscription || !cfg.Report.Enabled || pool == nil ||
		cfg.Twilio.AccountSID == "" || cfg.Twilio.AuthToken == "" {
		return ""
	}
	base := cfg.Twilio.WebhookBaseURL
	if base == "" {
		base = cfg.PublicURL
	}
	if base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/") + report.TranscriptionPath
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
