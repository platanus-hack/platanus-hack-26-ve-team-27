package panel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
)

type fakeLister struct {
	rows []ingest.Emergency
	err  error
	got  int
}

func (f *fakeLister) ListRecent(_ context.Context, limit int) ([]ingest.Emergency, error) {
	f.got = limit
	return f.rows, f.err
}

var t0 = time.Date(2026, 10, 24, 13, 0, 0, 0, time.UTC)

func sampleRows() []ingest.Emergency {
	cancelled := t0.Add(30 * time.Second)
	return []ingest.Emergency{
		{
			ID: 0xfedcba9876543210, DeviceID: "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77", Status: ingest.StatusOpen,
			Channels: []ingest.Channel{ingest.ChannelMesh, ingest.ChannelHTTP}, MinHops: 2,
			Location:        &ingest.Location{LatE5: 1043000, LonE5: -6690000, AgeS: 12, SentAt: t0},
			FirstReceivedAt: t0, UpdatedAt: t0.Add(10 * time.Second),
		},
		{
			ID: 7, DeviceID: "aaaaaaaa-0000-0000-0000-000000000000", Status: ingest.StatusCancelled,
			Channels: []ingest.Channel{ingest.ChannelSMS}, MinHops: ingest.HopsUnknown,
			FirstReceivedAt: t0, UpdatedAt: cancelled, CancelledAt: &cancelled,
		},
	}
}

func get(h *Handler, user, pass string, auth bool) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/demo/emergencies", nil)
	if auth {
		req.SetBasicAuth(user, pass)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func newHandler(l Lister) *Handler {
	h := New(l, "s3cret")
	h.now = func() time.Time { return t0.Add(time.Minute) }
	return h
}

func TestRendersRows(t *testing.T) {
	l := &fakeLister{rows: sampleRows()}
	rec := get(newHandler(l), "jury", "s3cret", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	if l.got != Limit {
		t.Errorf("limit = %d, want %d", l.got, Limit)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`http-equiv="refresh"`,
		"fedcba9876543210", "3f1c9a52", "MESH, HTTP", "Abierta",
		"09:00:00",  // t0 is 13:00 UTC = 09:00 in Caracas
		"hace 50 s", // updated 10 s after t0, now is t0+60 s
		"10.43000, -66.90000",
		`href="https://www.openstreetmap.org/?mlat=10.43000&amp;mlon=-66.90000#map=17/10.43000/-66.90000"`,
		"antigüedad del fix 12 s",
		"0000000000000007", "aaaaaaaa", "SMS", "Cancelada", "desconocido", "Sin ubicación",
		"hora de Caracas",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "3f1c9a52-7b1e") {
		t.Error("full device id is shown, want only the prefix")
	}
}

func TestEscapesHostileValues(t *testing.T) {
	rows := sampleRows()[:1]
	rows[0].DeviceID = "<script>alert(1)</script>"
	rows[0].Channels = []ingest.Channel{`"><img src=x>`}
	body := get(newHandler(&fakeLister{rows: rows}), "", "s3cret", true).Body.String()
	for _, bad := range []string{"<script>alert", `"><img`} {
		if strings.Contains(body, bad) {
			t.Errorf("body contains unescaped %q", bad)
		}
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("device prefix was not escaped")
	}
}

func TestEmptyState(t *testing.T) {
	rec := get(newHandler(&fakeLister{}), "", "s3cret", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Aún no hay emergencias") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "<table") {
		t.Error("empty state renders a table")
	}
}

func TestAuth(t *testing.T) {
	h := newHandler(&fakeLister{rows: sampleRows()})
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"no credentials": get(h, "", "", false),
		"wrong password": get(h, "jury", "nope", true),
		"empty password": get(h, "jury", "", true),
		"token as user":  get(h, "s3cret", "", true),
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="MAMA demo"` {
			t.Errorf("%s: WWW-Authenticate = %q", name, got)
		}
		if strings.Contains(rec.Body.String(), "fedcba98") {
			t.Errorf("%s: body leaks data", name)
		}
	}
	if rec := get(h, "anyone", "s3cret", true); rec.Code != http.StatusOK {
		t.Errorf("right password: status = %d, want 200", rec.Code)
	}
}

func TestNoDatabaseIs503(t *testing.T) {
	h := New(nil, "s3cret")
	if rec := get(h, "", "s3cret", true); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if rec := get(h, "", "wrong", true); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token without a database = %d, want 401", rec.Code)
	}
}

func TestListerErrorIs500(t *testing.T) {
	rec := get(newHandler(&fakeLister{err: errors.New("boom secret-detail")}), "", "s3cret", true)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("status %d, body %q", rec.Code, rec.Body)
	}
}

// TestWriteSample writes the rendered panel for two rows to PANEL_SAMPLE_OUT
// (skipped when unset), for a quick visual check outside the repo.
func TestWriteSample(t *testing.T) {
	out := os.Getenv("PANEL_SAMPLE_OUT")
	if out == "" {
		t.Skip("PANEL_SAMPLE_OUT not set")
	}
	rec := get(newHandler(&fakeLister{rows: sampleRows()}), "", "s3cret", true)
	if err := os.WriteFile(out, rec.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
