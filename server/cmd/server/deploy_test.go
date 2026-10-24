package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// Wires the real boot path with no DATABASE_URL: migrations are skipped and
// /health must answer 503, not panic on a nil *sql.DB.
func TestDeployRoutesWithoutDatabase(t *testing.T) {
	t.Setenv("RENDER_GIT_COMMIT", "abc123")
	mux := newMux(openAndMigrate(""), intake.LogHandoff{}, call.NewRoutes(config.Config{}, nil), testOffline(t), nil)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /health status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"commit":"abc123"`) {
		t.Fatalf("GET /version = %d %q, want 200 with commit abc123", rec.Code, rec.Body.String())
	}
}
