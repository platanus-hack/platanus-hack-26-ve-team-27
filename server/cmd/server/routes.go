package main

import (
	"database/sql"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/remoteconfig"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/sms"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/voice"
)

// newMux registers every HTTP route. Add new routes here so there is one
// place to see the whole API surface. pool is nil when DATABASE_URL is unset;
// notifier alerts the family for the incidents that skip the call; calls
// holds the call engine, the ElevenLabs webhook and the #11 spike route.
// offline is the offline emergency channel (#55, #57, #58), an extra behind
// EMERGENCY_OFFLINE_ENABLED: nil leaves GET /config, POST /emergencies* and
// POST /sms/inbound/{provider} unmounted (404). With it, and without a pool,
// the emergency routes answer 503. reports is the incident report page (#76),
// an extra behind INCIDENT_REPORT_ENABLED: nil leaves GET /i/{token} and its
// files unmounted (404). The device voice routes (#107) read
// ELEVENLABS_API_KEY themselves and answer 503 without it or without a pool.
func newMux(pool *sql.DB, notifier intake.Notifier, calls *call.Routes, offline *offlineEmergency, reports *report.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	registerDeployRoutes(mux, pool)
	incidents := intake.New(pool, calls.Starter(), notifier).WithCanceller(calls.Canceller())
	incidents.Register(mux)
	calls.Register(mux)
	voice.FromEnv(pool).Register(mux)
	if offline != nil {
		offline.register(mux, pool, incidents.OfflinePublisher())
	}
	if reports != nil {
		reports.Register(mux)
	}
	return mux
}

// offlineEmergency is what the offline emergency routes need: the server key
// pair, the central SMS number served in GET /config and the inbound SMS
// providers (empty: every provider's webhook answers 404).
type offlineEmergency struct {
	keys         serverkeys.Keys
	smsNumber    string
	smsProviders map[string]sms.InboundSMSProvider
}

// register mounts the offline emergency routes. pub turns each stored
// emergency into an incident (intake.OfflinePublisher), whatever the channel.
func (o *offlineEmergency) register(mux *http.ServeMux, pool *sql.DB, pub ingest.Publisher) {
	remoteconfig.New(o.smsNumber, o.keys).Register(mux)
	// Without a database both handlers get no service and answer 503;
	// smsIngester stays a nil interface then, not a nil *ingest.Service, so
	// the SMS handler sees it.
	var emergencies *ingest.Service
	var smsIngester sms.Ingester
	if pool != nil {
		emergencies = ingest.New(ingest.NewDBStore(pool), o.keys, pub)
		smsIngester = emergencies
	}
	ingest.NewHandler(emergencies).Register(mux)
	sms.NewHandler(smsIngester, o.smsProviders).Register(mux)
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
