package intake

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

const (
	deviceID   = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"
	incidentID = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"
)

// fakeHandoff records every handoff, standing in for #18 and #20.
type fakeHandoff struct {
	mu       sync.Mutex
	calls    []Incident
	notifies []Incident
}

func (f *fakeHandoff) StartCall(_ context.Context, inc Incident) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, inc)
}

func (f *fakeHandoff) Notify(_ context.Context, inc Incident) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifies = append(f.notifies, inc)
}

type env struct {
	t    *testing.T
	pool *sql.DB
	mux  *http.ServeMux
	fake *fakeHandoff
}

// newEnv migrates a throwaway database and mounts the intake routes on it,
// with handoffs run inline so tests can check them right after the request.
func newEnv(t *testing.T) *env {
	t.Helper()
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fake := &fakeHandoff{}
	h := New(pool, fake, fake)
	h.spawn = func(f func()) { f() }
	mux := http.NewServeMux()
	h.Register(mux)
	e := &env{t: t, pool: pool, mux: mux, fake: fake}
	if rec := e.post("/devices", sample(t, "device_registration.json")); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /devices = %d %s", rec.Code, rec.Body)
	}
	return e
}

func (e *env) post(path string, body []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	return rec
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func sample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "contract", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func ptr(f float64) *float64 { return &f }

func at(sec int) time.Time {
	return time.Date(2026, 10, 24, 9, 10, sec, 0, time.UTC)
}

// alert builds an AlertEvent whose newest point has speed (nil = unknown).
func alert(id string, speed *float64) []byte {
	ev := contract.AlertEvent{
		IncidentID:  id,
		DeviceID:    deviceID,
		Trigger:     contract.TriggerVolumePattern,
		TriggeredAt: at(0),
		Points: []contract.Point{
			{Lat: 10.4950, Lng: -66.8540, SpeedMPS: ptr(3), RecordedAt: at(0)},
			{Lat: 10.4961, Lng: -66.8531, AccuracyM: ptr(12.5), SpeedMPS: speed, RecordedAt: at(1)},
		},
	}
	raw, _ := json.Marshal(ev)
	return raw
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) contract.AlertResponse {
	t.Helper()
	var resp contract.AlertResponse
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode AlertResponse: %v", err)
	}
	return resp
}

// One speed sample per branch, through the HTTP handler: acceptance
// criterion 1 of #17.
func TestAlertPicksBranchBySpeed(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name  string
		id    string
		speed *float64
		want  contract.Branch
	}{
		{"still", "00000000-0000-4000-8000-000000000001", ptr(0.4), contract.BranchImmediateAlert},
		{"walking", "00000000-0000-4000-8000-000000000002", ptr(1.4), contract.BranchCall},
		{"unknown speed", "00000000-0000-4000-8000-000000000003", nil, contract.BranchCall},
		{"in a car", "00000000-0000-4000-8000-000000000004", ptr(11.2), contract.BranchSilentTracker},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.post("/incidents", alert(c.id, c.speed))
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d %s, want 201", rec.Code, rec.Body)
			}
			resp := decodeResponse(t, rec)
			if resp.IncidentID != c.id || resp.Branch != c.want {
				t.Fatalf("response = %+v, want branch %s for %s", resp, c.want, c.id)
			}
			if (resp.SpeedMPS == nil) != (c.speed == nil) || (c.speed != nil && *resp.SpeedMPS != *c.speed) {
				t.Fatalf("speed_mps = %v, want %v", resp.SpeedMPS, c.speed)
			}
			var stored string
			if err := e.pool.QueryRow(`SELECT branch FROM incidents WHERE id = $1`, c.id).Scan(&stored); err != nil || stored != string(c.want) {
				t.Fatalf("stored branch = %q (err %v), want %s", stored, err, c.want)
			}
		})
	}
}

func TestAlertStoresIncidentAndPoints(t *testing.T) {
	e := newEnv(t)
	rec := e.post("/incidents", sample(t, "alert_event.json"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if got := decodeResponse(t, rec); got.Branch != contract.BranchImmediateAlert || got.SpeedMPS == nil || *got.SpeedMPS != 0.4 {
		t.Fatalf("response = %+v, want the alert_response.json decision", got)
	}

	var trigger, branch string
	var speed float64
	var hasAudio bool
	var triggeredAt time.Time
	err := e.pool.QueryRow(`SELECT trigger, branch, speed_mps, has_audio, triggered_at FROM incidents WHERE id = $1 AND device_id = $2`,
		incidentID, deviceID).Scan(&trigger, &branch, &speed, &hasAudio, &triggeredAt)
	if err != nil {
		t.Fatalf("incident row: %v", err)
	}
	wantAt := time.Date(2026, 10, 24, 9, 10, 0, 253_000_000, time.UTC)
	if trigger != "volume_pattern" || branch != "immediate_alert" || speed != 0.4 || hasAudio || !triggeredAt.Equal(wantAt) {
		t.Fatalf("incident row = %s %s %v %v %v", trigger, branch, speed, hasAudio, triggeredAt)
	}

	var lat, lng, acc, pSpeed float64
	if err := e.pool.QueryRow(`SELECT lat, lng, accuracy_m, speed_mps FROM gps_points WHERE incident_id = $1`, incidentID).
		Scan(&lat, &lng, &acc, &pSpeed); err != nil {
		t.Fatalf("gps row: %v", err)
	}
	if lat != 10.4961 || lng != -66.8531 || acc != 12.5 || pSpeed != 0.4 {
		t.Fatalf("gps row = %v %v %v %v", lat, lng, acc, pSpeed)
	}
}

// A retried incident_id keeps the stored decision, stores only the new
// points and is not handed off again.
func TestAlertRetryReturnsStoredDecision(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", alert(incidentID, ptr(0.4))); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", rec.Code, rec.Body)
	}

	// The retry's newest point is fast and new: it would pick silent_tracker.
	var ev contract.AlertEvent
	_ = json.Unmarshal(alert(incidentID, ptr(0.4)), &ev)
	ev.Points = append(ev.Points, contract.Point{Lat: 10.4975, Lng: -66.8502, SpeedMPS: ptr(11.2), RecordedAt: at(15)})
	retry, _ := json.Marshal(ev)

	rec := e.post("/incidents", retry)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d %s, want 200", rec.Code, rec.Body)
	}
	if got := decodeResponse(t, rec); got.Branch != contract.BranchImmediateAlert || got.SpeedMPS == nil || *got.SpeedMPS != 0.4 {
		t.Fatalf("retry response = %+v, want the stored immediate_alert at 0.4", got)
	}
	if n := e.count(`SELECT count(*) FROM incidents`); n != 1 {
		t.Fatalf("incidents = %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM gps_points WHERE incident_id = $1`, incidentID); n != 3 {
		t.Fatalf("gps_points = %d, want 3 (2 first + 1 new)", n)
	}
	if len(e.fake.notifies) != 1 || len(e.fake.calls) != 0 {
		t.Fatalf("handoffs: %d notifies, %d calls; want 1 notify only", len(e.fake.notifies), len(e.fake.calls))
	}
}

func TestAlertUnknownDeviceIs422(t *testing.T) {
	e := newEnv(t)
	raw := bytes.Replace(alert(incidentID, ptr(0.4)), []byte(deviceID), []byte("11111111-2222-4333-8444-555555555555"), 1)
	if rec := e.post("/incidents", raw); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d %s, want 422", rec.Code, rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM incidents`); n != 0 {
		t.Fatalf("incidents = %d, want 0", n)
	}
	if len(e.fake.notifies)+len(e.fake.calls) != 0 {
		t.Fatal("an unknown device was handed off")
	}
}

func TestAlertIncidentOfAnotherDeviceIs409(t *testing.T) {
	e := newEnv(t)
	other := bytes.Replace(sample(t, "device_registration.json"), []byte(deviceID), []byte("other-device"), 1)
	if rec := e.post("/devices", other); rec.Code != http.StatusNoContent {
		t.Fatalf("register other = %d", rec.Code)
	}
	if rec := e.post("/incidents", alert(incidentID, ptr(0.4))); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d", rec.Code)
	}
	raw := bytes.Replace(alert(incidentID, ptr(0.4)), []byte(deviceID), []byte("other-device"), 1)
	if rec := e.post("/incidents", raw); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d %s, want 409", rec.Code, rec.Body)
	}
}

// The seam: one handoff per new incident, to the call engine for call and
// to the notifier for the other two branches.
func TestAlertHandsOffByBranch(t *testing.T) {
	e := newEnv(t)
	e.post("/incidents", alert("00000000-0000-4000-8000-000000000001", ptr(0.4)))
	e.post("/incidents", alert("00000000-0000-4000-8000-000000000002", ptr(2)))
	e.post("/incidents", alert("00000000-0000-4000-8000-000000000003", ptr(9)))
	e.post("/incidents", alert("00000000-0000-4000-8000-000000000002", ptr(2))) // retry

	if len(e.fake.calls) != 1 || e.fake.calls[0].ID != "00000000-0000-4000-8000-000000000002" || e.fake.calls[0].Branch != contract.BranchCall {
		t.Fatalf("calls = %+v, want the call incident once", e.fake.calls)
	}
	if len(e.fake.notifies) != 2 ||
		e.fake.notifies[0].Branch != contract.BranchImmediateAlert ||
		e.fake.notifies[1].Branch != contract.BranchSilentTracker {
		t.Fatalf("notifies = %+v, want immediate_alert then silent_tracker", e.fake.notifies)
	}
	c := e.fake.calls[0]
	if c.DeviceID != deviceID || c.Location.Lat != 10.4961 || !c.TriggeredAt.Equal(at(0)) || c.SpeedMPS == nil || *c.SpeedMPS != 2 {
		t.Fatalf("handed-off incident = %+v", c)
	}
}

func TestLocationUpdateAppendsPoints(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", sample(t, "alert_event.json")); rec.Code != http.StatusCreated {
		t.Fatalf("alert = %d", rec.Code)
	}
	path := "/incidents/" + incidentID + "/points"
	update := sample(t, "location_update.json")
	if rec := e.post(path, update); rec.Code != http.StatusNoContent {
		t.Fatalf("update = %d %s", rec.Code, rec.Body)
	}
	if rec := e.post(path, update); rec.Code != http.StatusNoContent { // resent fixes
		t.Fatalf("resend = %d %s", rec.Code, rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM gps_points WHERE incident_id = $1`, incidentID); n != 3 {
		t.Fatalf("gps_points = %d, want 3 (1 alert + 2 update, resend ignored)", n)
	}
	if n := e.count(`SELECT count(*) FROM gps_points WHERE incident_id = $1 AND speed_mps IS NULL`, incidentID); n != 1 {
		t.Fatalf("points with no speed = %d, want 1", n)
	}
}

func TestLocationUpdateErrors(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", sample(t, "alert_event.json")); rec.Code != http.StatusCreated {
		t.Fatalf("alert = %d", rec.Code)
	}
	update := sample(t, "location_update.json")
	other := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	cases := []struct {
		name string
		path string
		body []byte
		want int
	}{
		{"path does not match body", "/incidents/" + other + "/points", update, http.StatusBadRequest},
		{"unknown incident", "/incidents/" + other + "/points", bytes.ReplaceAll(update, []byte(incidentID), []byte(other)), http.StatusNotFound},
		{"another device", "/incidents/" + incidentID + "/points", bytes.Replace(update, []byte(deviceID), []byte("other-device"), 1), http.StatusForbidden},
		{"no points", "/incidents/" + incidentID + "/points", []byte(`{"incident_id":"` + incidentID + `","device_id":"` + deviceID + `","points":[]}`), http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := e.post(c.path, c.body); rec.Code != c.want {
				t.Fatalf("status = %d %s, want %d", rec.Code, rec.Body, c.want)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM gps_points`); n != 1 {
		t.Fatalf("gps_points = %d, want only the alert's 1", n)
	}
}

func TestDeviceRegistrationUpserts(t *testing.T) {
	e := newEnv(t)
	var firstUpdated time.Time
	if err := e.pool.QueryRow(`SELECT updated_at FROM devices WHERE id = $1`, deviceID).Scan(&firstUpdated); err != nil {
		t.Fatal(err)
	}
	changed := []byte(`{"device_id":"` + deviceID + `","phone_number":"+584149998877","family_numbers":["+584241112233"],"duress_phrase":"otra frase"}`)
	if rec := e.post("/devices", changed); rec.Code != http.StatusNoContent {
		t.Fatalf("re-register = %d %s", rec.Code, rec.Body)
	}
	var phone, phrase string
	var appVersion sql.NullString
	var family string
	var updated time.Time
	err := e.pool.QueryRow(`SELECT phone_number, family_numbers::text, duress_phrase, app_version, updated_at FROM devices WHERE id = $1`, deviceID).
		Scan(&phone, &family, &phrase, &appVersion, &updated)
	if err != nil {
		t.Fatal(err)
	}
	if phone != "+584149998877" || family != "{+584241112233}" || phrase != "otra frase" || appVersion.Valid || !updated.After(firstUpdated) {
		t.Fatalf("device row = %s %s %s %v %v (first updated %v)", phone, family, phrase, appVersion, updated, firstUpdated)
	}
	if n := e.count(`SELECT count(*) FROM devices`); n != 1 {
		t.Fatalf("devices = %d, want 1", n)
	}
}

// Bad input is a 400 and stores nothing.
func TestBadInputIs400(t *testing.T) {
	e := newEnv(t)
	good := string(alert(incidentID, ptr(0.4)))
	cases := []struct {
		name, path, body string
	}{
		{"not JSON", "/incidents", `{`},
		{"unknown field", "/incidents", strings.Replace(good, `"has_audio"`, `"extra":1,"has_audio"`, 1)},
		{"trailing data", "/incidents", good + `{}`},
		{"no points", "/incidents", `{"incident_id":"` + incidentID + `","device_id":"` + deviceID + `","trigger":"widget","triggered_at":"2026-10-24T09:10:00Z","points":[],"has_audio":false}`},
		{"bad trigger", "/incidents", strings.Replace(good, `"volume_pattern"`, `"shake"`, 1)},
		{"incident_id not a UUID", "/incidents", strings.Replace(good, incidentID, "abc", 1)},
		{"lat out of range", "/incidents", strings.Replace(good, `10.4961`, `100.4961`, 1)},
		{"device without phone", "/devices", `{"device_id":"x","phone_number":"","family_numbers":[],"duress_phrase":"p"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := e.post(c.path, []byte(c.body)); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d %s, want 400", rec.Code, rec.Body)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM incidents`); n != 0 {
		t.Fatalf("incidents = %d, want 0", n)
	}
}

func TestNoDatabaseIs503(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, LogHandoff{}, LogHandoff{}).Register(mux)
	for _, path := range []string{"/devices", "/incidents", "/incidents/" + incidentID + "/points"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503", path, rec.Code)
		}
	}
}

// The default spawn runs the handoff in the background: a slow call engine
// does not hold the response.
func TestHandoffDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	h := New(nil, blockingStarter{release, done}, LogHandoff{})
	start := time.Now()
	h.handoff(context.Background(), Incident{ID: incidentID, Branch: contract.BranchCall})
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("handoff blocked the caller")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the call engine never ran")
	}
}

type blockingStarter struct{ release, done chan struct{} }

func (b blockingStarter) StartCall(context.Context, Incident) {
	<-b.release
	close(b.done)
}
