package report

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// demoStore holds incidents by creation time.
type demoStore struct {
	incidents map[string]time.Time
	ensured   []string
}

func (d *demoStore) NewestIncident(_ context.Context, after time.Time) (string, error) {
	best, bestAt := "", time.Time{}
	for id, t := range d.incidents {
		if t.After(after) && t.After(bestAt) {
			best, bestAt = id, t
		}
	}
	if best == "" {
		return "", ErrUnknownIncident
	}
	return best, nil
}

func (d *demoStore) Now(context.Context) (time.Time, error) { return at("2026-10-24T13:00:00Z"), nil }

func (d *demoStore) Ensure(_ context.Context, id string, _ int) (Link, error) {
	d.ensured = append(d.ensured, id)
	return Link{Token: "tok-" + id[:8] + "AAAAAAAAAAAAAA"}, nil
}

func demoMux(store DemoStore, key string) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(fakeSource{}, Options{}).WithDemo(&Demo{
		Key: key, Store: store, TTLDays: 30,
	}).Register(mux)
	return mux
}

func get(mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestDemoLatestRedirectsWithTheRightKeyOnly(t *testing.T) {
	store := &demoStore{incidents: map[string]time.Time{
		"11111111-old": at("2026-10-24T12:00:00Z"),
		"22222222-new": at("2026-10-24T13:10:00Z"),
	}}
	mux := demoMux(store, "k3y-demo")
	rec := get(mux, "/demo/latest?key=k3y-demo")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/i/tok-22222222AAAAAAAAAAAAAA" || store.ensured[0] != "22222222-new" {
		t.Fatalf("latest = %d %q, ensured %v", rec.Code, rec.Header().Get("Location"), store.ensured)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", rec.Header())
	}
	for _, path := range []string{"/demo/latest", "/demo/latest?key=", "/demo/latest?key=k3y-dem", "/demo/latest?key=k3y-demo2", "/demo/next?key=nope", "/demo/next.json?key=nope&since=2026-10-24T13:00:00Z"} {
		if rec := get(mux, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	if rec := get(demoMux(&demoStore{}, "k3y-demo"), "/demo/latest?key=k3y-demo"); rec.Code != http.StatusNotFound {
		t.Errorf("no incidents: %d, want 404", rec.Code)
	}
}

// A blank DEMO_KEY never matches, not even an empty key.
func TestDemoWithoutAKeyIsOff(t *testing.T) {
	d := &Demo{}
	if d.authorized(httptest.NewRequest(http.MethodGet, "/demo/latest?key=", nil)) {
		t.Error("an empty key matched a blank DEMO_KEY")
	}
	// Without WithDemo the routes do not exist.
	mux := http.NewServeMux()
	NewHandler(fakeSource{}, Options{}).Register(mux)
	if rec := get(mux, "/demo/latest?key=x"); rec.Code != http.StatusNotFound {
		t.Errorf("unmounted: %d", rec.Code)
	}
}

// /demo/next waits for an incident newer than the moment it opened.
func TestDemoNextWaitsForTheNextIncident(t *testing.T) {
	store := &demoStore{incidents: map[string]time.Time{"11111111-old": at("2026-10-24T12:00:00Z")}}
	mux := demoMux(store, "k3y-demo")
	page := get(mux, "/demo/next?key=k3y-demo")
	body := html.UnescapeString(page.Body.String())
	if page.Code != http.StatusOK || !strings.Contains(body, "Esperando la próxima alerta") {
		t.Fatalf("next = %d", page.Code)
	}
	i := strings.Index(body, `data-demo-wait="`)
	u, err := url.Parse(body[i+len(`data-demo-wait="`) : i+strings.Index(body[i:], `">`)])
	if err != nil || u.Path != "/demo/next.json" || u.Query().Get("key") != "k3y-demo" || u.Query().Get("since") != "2026-10-24T13:00:00Z" {
		t.Fatalf("wait URL = %v (%v)", u, err)
	}

	answer := func() map[string]any {
		rec := get(mux, u.String())
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("next.json = %d %s", rec.Code, rec.Body)
		}
		return out
	}
	if out := answer(); out["waiting"] != true {
		t.Errorf("before the gesture: %v", out)
	}
	store.incidents["22222222-new"] = at("2026-10-24T13:00:05Z")
	if out := answer(); out["url"] != "/i/tok-22222222AAAAAAAAAAAAAA" {
		t.Errorf("after the gesture: %v", out)
	}
	if rec := get(mux, "/demo/next.json?key=k3y-demo&since=yesterday"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad since = %d", rec.Code)
	}
}
