// Command ficha prints the private report link of an incident (#76),
// creating it when the incident has none or its link expired, so the report
// page can be tried before the family message carries the link.
//
//	cd server
//	DATABASE_URL=postgres://... PUBLIC_URL=https://mama.example.com go run ./cmd/ficha <incident_id>
//
// It reads the same env vars as the server (DATABASE_URL, PUBLIC_URL or
// RENDER_EXTERNAL_URL, INCIDENT_REPORT_TTL_DAYS) and never migrates: run it
// against a database the server already migrated. The page only answers on
// a server started with INCIDENT_REPORT_ENABLED=true.
//
// -seal also seals the report now with EMERGENCY_ED25519_SEED (the server
// seals by itself after the call; this is for rehearsals and tests).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
)

func main() {
	cfg := config.Load()
	ttl := flag.Int("ttl-days", cfg.Report.TTLDays, "days a new link lives")
	seal := flag.Bool("seal", false, "seal the report now with EMERGENCY_ED25519_SEED")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: go run ./cmd/ficha [-ttl-days N] <incident_id>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(cfg, flag.Arg(0), *ttl, *seal); err != nil {
		fmt.Fprintln(os.Stderr, "ficha:", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, incidentID string, ttlDays int, seal bool) error {
	pool, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if pool == nil {
		return errors.New("DATABASE_URL is not set")
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	link, err := report.NewDBStore(pool).Ensure(ctx, incidentID, ttlDays)
	if errors.Is(err, report.ErrUnknownIncident) {
		return fmt.Errorf("incident %q is not stored", incidentID)
	}
	if err != nil {
		return err
	}
	fmt.Println(link.URL(cfg.PublicURL))
	if cfg.PublicURL == "" {
		fmt.Fprintln(os.Stderr, "ficha: PUBLIC_URL is not set; prefix the path with the server's URL")
	}
	fmt.Fprintf(os.Stderr, "ficha: expires %s\n", link.ExpiresAt.UTC().Format(time.RFC3339))
	if !seal {
		return nil
	}
	key, err := serverkeys.SigningKey(cfg.Emergency)
	if err != nil {
		return err
	}
	if key == nil {
		return errors.New("-seal needs EMERGENCY_ED25519_SEED")
	}
	sealed, err := (&report.Closer{Store: report.NewDBStore(pool), Signer: key}).Seal(ctx, incidentID)
	if err != nil {
		return err
	}
	if !sealed {
		fmt.Fprintln(os.Stderr, "ficha: not sealed: already sealed, or the incident had no call")
	}
	return nil
}
