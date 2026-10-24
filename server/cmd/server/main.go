// Command server is the MAMÁ backend HTTP server.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
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

	pool := openAndMigrate(cfg.DatabaseURL)
	notifier := newNotifier(cfg, pool)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMux(pool, notifier),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("mama server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
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

// channelConfigured reports whether the NOTIFIER channel has its credentials,
// so a misconfigured deploy says so at boot instead of at the first alert.
func channelConfigured(cfg config.Config) bool {
	if cfg.Notifier == config.NotifierSMS {
		return cfg.Twilio.AccountSID != "" && cfg.Twilio.AuthToken != "" && cfg.Twilio.PhoneNumber != ""
	}
	return cfg.Kapso.APIKey != "" && cfg.Kapso.PhoneNumberID != ""
}
