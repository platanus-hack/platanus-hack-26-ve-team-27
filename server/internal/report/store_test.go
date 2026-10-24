package report

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// newDB migrates a throwaway database holding sealedRecord's incident,
// points and call (no recording, no seal). It skips without
// TEST_DATABASE_URL.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	r := sealedRecord()
	exec(t, pool, `INSERT INTO devices (id, phone_number, family_numbers, duress_phrase)
		VALUES ($1, '+584141234567', '{+584241112233,+584125556677}', $2)`, r.Incident.DeviceID, testPhrase)
	exec(t, pool, `INSERT INTO incidents (id, device_id, trigger, branch, speed_mps, triggered_at, created_at, notified_at)
		VALUES ($1, $2, 'volume_pattern', 'call', 1.1, $3, $4, $5)`,
		testIncidentID, r.Incident.DeviceID, r.Incident.TriggeredAt, r.Incident.CreatedAt, *r.Incident.NotifiedAt)
	for _, p := range r.Points {
		exec(t, pool, `INSERT INTO gps_points (incident_id, lat, lng, accuracy_m, speed_mps, recorded_at, received_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, testIncidentID, p.Lat, p.Lng, p.AccuracyM, p.SpeedMPS, p.RecordedAt, p.ReceivedAt)
	}
	c := r.Calls[0]
	exec(t, pool, `INSERT INTO calls (incident_id, conversation_id, status, transcript, duration_secs, requested_at, ended_at, verdict, verdict_reason, verdict_at)
		VALUES ($1, $2, 'done', $3, $4, $5, $6, $7, $8, $9)`,
		testIncidentID, c.ConversationID, `[
			{"role":"agent","message":"¿Aló? Mi amor, ¿dónde estás?","time_in_call_secs":0},
			{"role":"user","message":"Hola mami, voy saliendo del trabajo.","time_in_call_secs":4},
			{"role":"agent","message":"¿Y ya comiste algo?","time_in_call_secs":9.5},
			{"role":"user","message":"Sí, mami. Oye, dile a la tía Carmen que llego tarde.","time_in_call_secs":15},
			{"role":"agent","message":"Va, mi amor, yo le digo. Cuídate.","time_in_call_secs":22}]`,
		*c.DurationSecs, c.RequestedAt, *c.EndedAt, string(c.Verdict), c.VerdictReason, *c.VerdictAt)
	return pool
}

func exec(t *testing.T, pool *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestEnsureCreatesOneLinkAndReusesIt(t *testing.T) {
	pool := newDB(t)
	s := NewDBStore(pool)
	ctx := context.Background()

	l1, err := s.Ensure(ctx, testIncidentID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidToken(l1.Token) {
		t.Fatalf("token %q", l1.Token)
	}
	if d := time.Until(l1.ExpiresAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("expires in %s, want 30 days", d)
	}
	l2, err := s.Ensure(ctx, testIncidentID, 7)
	if err != nil || l2.Token != l1.Token || !l2.ExpiresAt.Equal(l1.ExpiresAt) {
		t.Errorf("second Ensure = %+v, %v; want the same link %+v", l2, err, l1)
	}

	// Concurrent callers all get the one link.
	var wg sync.WaitGroup
	tokens := make([]string, 8)
	for i := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := s.Ensure(ctx, testIncidentID, 30)
			if err != nil {
				t.Error(err)
			}
			tokens[i] = l.Token
		}()
	}
	wg.Wait()
	for _, tok := range tokens {
		if tok != l1.Token {
			t.Errorf("concurrent Ensure = %q, want %q", tok, l1.Token)
		}
	}
}

func TestEnsureUnknownIncident(t *testing.T) {
	s := NewDBStore(newDB(t))
	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
		if _, err := s.Ensure(context.Background(), id, 30); !errors.Is(err, ErrUnknownIncident) {
			t.Errorf("Ensure(%q) = %v, want ErrUnknownIncident", id, err)
		}
	}
}

// An expired link answers 404, and the next Ensure replaces it with a new
// token: the old one never comes back.
func TestExpiredLinkIs404AndIsReplaced(t *testing.T) {
	pool := newDB(t)
	s := NewDBStore(pool)
	ctx := context.Background()
	old, err := s.Ensure(ctx, testIncidentID, 30)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `UPDATE incident_reports SET expires_at = now() - interval '1 second'`)

	if _, err := s.ByToken(ctx, old.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByToken(expired) = %v, want ErrNotFound", err)
	}
	mux := http.NewServeMux()
	NewHandler(s, Options{}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+old.Token, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET expired = %d, want 404", rec.Code)
	}

	fresh, err := s.Ensure(ctx, testIncidentID, 30)
	if err != nil || fresh.Token == old.Token || !fresh.ExpiresAt.After(time.Now()) {
		t.Fatalf("Ensure after expiry = %+v, %v; want a new live token", fresh, err)
	}
	if _, err := s.ByToken(ctx, old.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token after rotation = %v, want ErrNotFound", err)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+fresh.Token, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET fresh = %d, want 200", rec.Code)
	}
}

func TestByTokenUnknown(t *testing.T) {
	s := NewDBStore(newDB(t))
	for _, tok := range []string{"BBBBBBBBBBBBBBBBBBBBBB", "short", testIncidentID} {
		if _, err := s.ByToken(context.Background(), tok); !errors.Is(err, ErrNotFound) {
			t.Errorf("ByToken(%q) = %v, want ErrNotFound", tok, err)
		}
	}
}

// Load reads back what the fixture stored, and Build renders the same
// timeline as from the in-memory record.
func TestLoadReadsTheWholeRecord(t *testing.T) {
	pool := newDB(t)
	s := NewDBStore(pool)
	got, err := s.Load(context.Background(), testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	want := openRecord()
	if got.Incident.Trigger != want.Incident.Trigger || got.Incident.Branch != want.Incident.Branch ||
		*got.Incident.SpeedMPS != 1.1 || !got.Incident.TriggeredAt.Equal(want.Incident.TriggeredAt) ||
		!got.Incident.NotifiedAt.Equal(*want.Incident.NotifiedAt) {
		t.Errorf("incident = %+v", got.Incident)
	}
	if got.Device.FamilyNumbers != 2 || got.Device.PhoneNumber != "+584141234567" || got.Device.DuressPhrase != testPhrase {
		t.Errorf("device = %+v", got.Device)
	}
	if len(got.Points) != 3 || got.Points[2].SpeedMPS != nil || *got.Points[0].AccuracyM != 12.5 {
		t.Errorf("points = %+v", got.Points)
	}
	if len(got.Calls) != 1 || len(got.Calls[0].Transcript) != 5 || got.Calls[0].Verdict != "duress" || got.Calls[0].Recording != nil || got.Seal != nil {
		t.Fatalf("calls = %+v seal = %+v", got.Calls, got.Seal)
	}
	g, w := rows(Timeline(got, Options{})), rows(Timeline(want, Options{}))
	if len(g) != len(w) {
		t.Fatalf("timeline from the database has %d events, from memory %d", len(g), len(w))
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("event %d: db %+v, memory %+v", i, g[i], w[i])
		}
	}

	if _, err := s.Load(context.Background(), "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrUnknownIncident) {
		t.Errorf("Load(unknown) = %v", err)
	}
}

func TestLinksReportURL(t *testing.T) {
	s := NewDBStore(newDB(t))
	ctx := context.Background()
	u, err := Links{Store: s, BaseURL: "https://mama.example.com/", TTLDays: 30}.ReportURL(ctx, testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	link, _ := s.Ensure(ctx, testIncidentID, 30)
	if u != "https://mama.example.com/i/"+link.Token {
		t.Errorf("ReportURL = %q, want the live link %q", u, link.Token)
	}
	if u, err := (Links{Store: s, TTLDays: 30}).ReportURL(ctx, testIncidentID); u != "" || err != nil {
		t.Errorf("no public URL: %q %v, want no link", u, err)
	}
	if _, err := (Links{Store: s, BaseURL: "https://x", TTLDays: 30}).ReportURL(ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrUnknownIncident) {
		t.Errorf("unknown incident: %v", err)
	}
}
