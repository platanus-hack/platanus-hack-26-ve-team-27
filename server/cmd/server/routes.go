package main

import (
	"database/sql"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/remoteconfig"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// newMux registers every HTTP route. Add new routes here so there is one
// place to see the whole API surface. pool is nil when DATABASE_URL is unset;
// notifier alerts the family for the incidents that skip the call; calls
// holds the call engine, the ElevenLabs webhook and the #11 spike route.
// offline is the offline emergency channel (#55, #57), an extra behind
// EMERGENCY_OFFLINE_ENABLED: nil leaves GET /config and POST /emergencies*
// unmounted (404). With it, and without a pool, the emergency routes answer 503.
func newMux(pool *sql.DB, notifier intake.Notifier, calls *call.Routes, offline *offlineEmergency) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	registerDeployRoutes(mux, pool)
	intake.New(pool, calls.Starter(), notifier).Register(mux)
	calls.Register(mux)
	if offline != nil {
		offline.register(mux, pool)
	}
	return mux
}

// offlineEmergency is what the offline emergency routes need: the server key
// pair and the central SMS number served in GET /config.
type offlineEmergency struct {
	keys      serverkeys.Keys
	smsNumber string
}

func (o *offlineEmergency) register(mux *http.ServeMux, pool *sql.DB) {
	remoteconfig.New(o.smsNumber, o.keys).Register(mux)
	// The Kapso module replaces LogPublisher. #58 mounts the SMS webhook with
	// emergencies. emergencies is nil without a database, so the webhook
	// handler must answer 503 like ingest.Handler.ready does.
	var emergencies *ingest.Service
	if pool != nil {
		emergencies = ingest.New(ingest.NewDBStore(pool), o.keys, ingest.LogPublisher{})
	}
	ingest.NewHandler(emergencies).Register(mux)
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
