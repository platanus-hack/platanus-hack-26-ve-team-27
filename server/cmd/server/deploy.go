package main

import (
	"log"
	"net/http"
	"os"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/health"
)

// registerDeployRoutes mounts the Render probes: GET /version and GET /health.
// It opens the pool from DATABASE_URL; newMux runs once per process in main.
func registerDeployRoutes(mux *http.ServeMux) {
	var pinger health.Pinger // a true nil interface unless a pool exists
	switch pool, err := db.Open(os.Getenv("DATABASE_URL")); {
	case err != nil:
		log.Printf("database: %v", err)
	case pool == nil:
		log.Print("database: DATABASE_URL is not set; /health will return 503")
	default:
		pinger = pool
	}
	mux.Handle("GET /version", health.VersionHandler(os.Getenv("RENDER_GIT_COMMIT")))
	mux.Handle("GET /health", health.HealthHandler(pinger))
}
