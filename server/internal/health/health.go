// Package health serves the deploy probes: /version reports the running
// commit and /health reports whether the database answers.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// PingTimeout bounds how long /health waits for the database.
const PingTimeout = 2 * time.Second

// Pinger is the part of *sql.DB that /health needs.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// VersionHandler returns {"commit": commit}, or "dev" when commit is empty.
// Pass os.Getenv("RENDER_GIT_COMMIT").
func VersionHandler(commit string) http.Handler {
	if commit == "" {
		commit = "dev"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"commit": commit})
	})
}

// HealthHandler returns 200 {"status":"ok"} when db answers a ping within
// PingTimeout, and 503 otherwise. A nil db (DATABASE_URL unset) is a 503.
func HealthHandler(db Pinger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": "database not configured"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), PingTimeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": "database unreachable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
