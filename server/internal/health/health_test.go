package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakePinger struct {
	err      error
	deadline bool
}

func (f *fakePinger) PingContext(ctx context.Context) error {
	_, f.deadline = ctx.Deadline()
	return f.err
}

func serve(t *testing.T, h http.Handler, path string) (int, map[string]string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	return rec.Code, body
}

func TestVersionReturnsCommit(t *testing.T) {
	code, body := serve(t, VersionHandler("abc123"), "/version")
	if code != http.StatusOK || body["commit"] != "abc123" {
		t.Fatalf("got %d %v, want 200 commit=abc123", code, body)
	}
}

func TestVersionDefaultsToDev(t *testing.T) {
	code, body := serve(t, VersionHandler(""), "/version")
	if code != http.StatusOK || body["commit"] != "dev" {
		t.Fatalf("got %d %v, want 200 commit=dev", code, body)
	}
}

func TestHealthOKWhenDBReachable(t *testing.T) {
	db := &fakePinger{}
	code, body := serve(t, HealthHandler(db), "/health")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v, want 200 status=ok", code, body)
	}
	if !db.deadline {
		t.Fatal("ping context has no deadline")
	}
}

func TestHealthUnavailableWhenPingFails(t *testing.T) {
	code, body := serve(t, HealthHandler(&fakePinger{err: errors.New("connection refused")}), "/health")
	if code != http.StatusServiceUnavailable || body["status"] == "ok" {
		t.Fatalf("got %d %v, want 503", code, body)
	}
}

func TestHealthUnavailableWithoutDB(t *testing.T) {
	code, _ := serve(t, HealthHandler(nil), "/health")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", code)
	}
}

type slowPinger struct{}

func (slowPinger) PingContext(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestHealthTimesOut(t *testing.T) {
	start := time.Now()
	code, _ := serve(t, HealthHandler(slowPinger{}), "/health")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", code)
	}
	if elapsed := time.Since(start); elapsed > PingTimeout+time.Second {
		t.Fatalf("took %v, want about %v", elapsed, PingTimeout)
	}
}
