package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Wires the real db.Open path with no DATABASE_URL: /health must answer 503,
// not panic on a nil *sql.DB.
func TestDeployRoutesWithoutDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("RENDER_GIT_COMMIT", "abc123")
	mux := newMux()

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
