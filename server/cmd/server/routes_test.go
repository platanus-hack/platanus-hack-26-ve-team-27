package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

func TestRootReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIntakeRoutesAreMounted(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, intake.LogHandoff{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/incidents", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /incidents with no database = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
