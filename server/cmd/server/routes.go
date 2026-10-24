package main

import (
	"database/sql"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// newMux registers every HTTP route. Add new routes here so there is one
// place to see the whole API surface. pool is nil when DATABASE_URL is unset.
func newMux(pool *sql.DB) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	registerDeployRoutes(mux, pool)
	// The call engine (#18) and the notifier (#20) replace LogHandoff.
	intake.New(pool, intake.LogHandoff{}, intake.LogHandoff{}).Register(mux)
	return mux
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
