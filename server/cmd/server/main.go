// Command server is the MAMÁ backend HTTP server.
package main

import (
	"context"
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
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/notify"
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
	notifier := newNotifier(cfg, pool)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMux(pool, notifier, newCallRoutes(cfg, pool), offline),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("mama server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// loadOfflineEmergency returns the offline emergency channel, or nil when
// EMERGENCY_OFFLINE_ENABLED is off: then no key is read or required and the
// SMS number is not checked. Enabled, a bad SMS number exits and the keys load
// through loadServerKeys.
func loadOfflineEmergency(cfg config.Config, onRender bool) *offlineEmergency {
	if !cfg.Emergency.Enabled {
		log.Print("emergency: offline channel is off (EMERGENCY_OFFLINE_ENABLED is not true)")
		return nil
	}
	keys := loadServerKeys(cfg, onRender)
	if remoteconfig.CheckSMSNumber(cfg.Emergency.SMSNumber) != nil {
		log.Fatalf("emergency: EMERGENCY_SMS_NUMBER must be E.164 (+ and 7-15 digits)")
	}
	return &offlineEmergency{keys: keys, smsNumber: cfg.Emergency.SMSNumber}
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
		return call.NewRoutes(cfg.ElevenLabs, nil)
	}
	return call.NewRoutes(cfg.ElevenLabs, call.NewDBStore(pool))
}

// channelConfigured reports whether the NOTIFIER channel has its credentials,
// so a misconfigured deploy says so at boot instead of at the first alert.
func channelConfigured(cfg config.Config) bool {
	if cfg.Notifier == config.NotifierSMS {
		return cfg.Twilio.AccountSID != "" && cfg.Twilio.AuthToken != "" && cfg.Twilio.PhoneNumber != ""
	}
	return cfg.Kapso.APIKey != "" && cfg.Kapso.PhoneNumberID != ""
}
