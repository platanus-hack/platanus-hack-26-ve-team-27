package main

import (
	"database/sql"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/inboundsms"
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
// POST /sms/inbound/{provider} (#58) is part of the extra.
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
// pair, the central SMS number served in GET /config, and what the inbound SMS
// webhook needs to authenticate Twilio (TWILIO_AUTH_TOKEN, PUBLIC_BASE_URL);
// without them the webhook answers 503.
type offlineEmergency struct {
	keys            serverkeys.Keys
	smsNumber       string
	twilioAuthToken string
	publicBaseURL   string
}

func (o *offlineEmergency) register(mux *http.ServeMux, pool *sql.DB) {
	remoteconfig.New(o.smsNumber, o.keys).Register(mux)
	// The Kapso module replaces LogPublisher. emergencies is nil without a
	// database, so both handlers answer 503 then.
	var emergencies *ingest.Service
	if pool != nil {
		emergencies = ingest.New(ingest.NewDBStore(pool), o.keys, ingest.LogPublisher{})
	}
	ingest.NewHandler(emergencies).Register(mux)
	inboundsms.New(emergencies, inboundsms.NewTwilio(o.twilioAuthToken, o.publicBaseURL)).Register(mux)
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
