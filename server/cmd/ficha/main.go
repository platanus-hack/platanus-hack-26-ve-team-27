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
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
)

func main() {
	cfg := config.Load()
	ttl := flag.Int("ttl-days", cfg.Report.TTLDays, "days a new link lives")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: go run ./cmd/ficha [-ttl-days N] <incident_id>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(cfg, flag.Arg(0), *ttl); err != nil {
		fmt.Fprintln(os.Stderr, "ficha:", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, incidentID string, ttlDays int) error {
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
	return nil
}
