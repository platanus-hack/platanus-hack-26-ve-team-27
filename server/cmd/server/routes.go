package main

import (
	"database/sql"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// newMux registers every HTTP route. Add new routes here so there is one
// place to see the whole API surface. pool is nil when DATABASE_URL is unset;
// notifier alerts the family for the incidents that skip the call; calls
// holds the call engine, the ElevenLabs webhook and the #11 spike route.
func newMux(pool *sql.DB, notifier intake.Notifier, calls *call.Routes) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	registerDeployRoutes(mux, pool)
	intake.New(pool, calls.Starter(), notifier).Register(mux)
	calls.Register(mux)
	return mux
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
