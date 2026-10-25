package main

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/health"
)

// registerDeployRoutes mounts the Render probes: GET /version and GET /health.
// pool is nil when DATABASE_URL is unset; /health then answers 503.
func registerDeployRoutes(mux *http.ServeMux, pool *sql.DB) {
	var pinger health.Pinger // a true nil interface unless a pool exists
	if pool != nil {
		pinger = pool
	}
	mux.Handle("GET /version", health.VersionHandler(os.Getenv("RENDER_GIT_COMMIT")))
	mux.Handle("GET /health", health.HealthHandler(pinger))
}
