package main

import (
	"database/sql"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/remoteconfig"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// newMux registers every HTTP route. Add new routes here so there is one
// place to see the whole API surface. pool is nil when DATABASE_URL is unset;
// notifier alerts the family for the incidents that skip the call. keys is the
// server key pair and smsNumber the central SMS number served in GET /config.
// Without a pool the offline emergency routes answer 503.
func newMux(pool *sql.DB, notifier intake.Notifier, keys serverkeys.Keys, smsNumber string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	registerDeployRoutes(mux, pool)
	// The call engine (#18) replaces LogHandoff.
	intake.New(pool, intake.LogHandoff{}, notifier).Register(mux)
	remoteconfig.New(smsNumber, keys).Register(mux)
	// The Kapso module replaces LogPublisher. #58 mounts the SMS webhook with
	// emergencies.
	var emergencies *ingest.Service
	if pool != nil {
		emergencies = ingest.New(ingest.NewDBStore(pool), keys, ingest.LogPublisher{})
	}
	ingest.NewHandler(emergencies).Register(mux)
	return mux
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
