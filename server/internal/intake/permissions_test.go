package intake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

const permissionsPath = "/devices/" + deviceID + "/permissions"

// storedDeviceState is incidents.device_state of id, decoded; nil when NULL.
func (e *env) storedDeviceState(id string) *contract.DeviceState {
	e.t.Helper()
	var raw []byte
	if err := e.pool.QueryRow(`SELECT device_state FROM incidents WHERE id = $1`, id).Scan(&raw); err != nil {
		e.t.Fatalf("device_state: %v", err)
	}
	if raw == nil {
		return nil
	}
	var state contract.DeviceState
	if err := json.Unmarshal(raw, &state); err != nil {
		e.t.Fatalf("stored device_state %s: %v", raw, err)
	}
	return &state
}

func TestAlertStoresDeviceState(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", sample(t, "alert_event.json")); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	state := e.storedDeviceState(incidentID)
	if state == nil {
		t.Fatal("device_state is NULL")
	}
	if want := time.Date(2026, 10, 24, 9, 10, 0, 251_000_000, time.UTC); !state.CapturedAt.Equal(want) {
		t.Errorf("captured_at = %v, want %v", state.CapturedAt, want)
	}
	if len(state.Permissions) != 9 || state.Permissions[contract.PermissionBatteryUnrestricted] || !state.Permissions[contract.PermissionAccessibilityService] {
		t.Errorf("permissions = %v, want the sample's 9 with battery_unrestricted off", state.Permissions)
	}
	if _, ok := state.Permissions[contract.PermissionFullScreenIntent]; ok {
		t.Error("full_screen_intent stored, but the sample leaves it out (it does not apply to the phone)")
	}
}

// An app from before #125 sends no device_state: the alert works as before.
func TestAlertWithoutDeviceStateStoresNull(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", alert(incidentID, nil)); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if state := e.storedDeviceState(incidentID); state != nil {
		t.Fatalf("device_state = %+v, want NULL", state)
	}
	if len(e.fake.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(e.fake.calls))
	}
}

// Whatever the phone puts in device_state, the alert is created and handed
// off; only well-formed permissions are kept.
func TestAlertIsNeverRejectedOverDeviceState(t *testing.T) {
	cases := []struct {
		name  string
		state string
		want  map[string]bool // nil: stored NULL
	}{
		{"not an object", `"todo bien"`, nil},
		{"no permission decodes", `{"captured_at":"2026-10-24T09:10:00Z","permissions":{"sms":"yes"}}`, nil},
		{"some values malformed", `{"captured_at":"2026-10-24T09:10:00Z","permissions":{"sms":true,"microphone":null,"contacts":1},"extra":[1]}`,
			map[string]bool{"sms": true}},
		{"a newer app's permission", `{"captured_at":"2026-10-24T09:10:00Z","permissions":{"exact_alarms":false}}`,
			map[string]bool{"exact_alarms": false}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			id := fmt.Sprintf("9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e%02d", i)
			body := strings.TrimSuffix(string(alert(id, nil)), "}") + `,"device_state":` + c.state + `}`
			if rec := e.post("/incidents", []byte(body)); rec.Code != http.StatusCreated {
				t.Fatalf("status = %d %s", rec.Code, rec.Body)
			}
			state := e.storedDeviceState(id)
			switch {
			case c.want == nil && state != nil:
				t.Fatalf("device_state = %+v, want NULL", state)
			case c.want != nil && (state == nil || fmt.Sprint(state.Permissions) != fmt.Sprint(c.want)):
				t.Fatalf("device_state = %+v, want permissions %v", state, c.want)
			}
			if len(e.fake.calls) != 1 {
				t.Fatalf("calls = %d, want the alert handed off", len(e.fake.calls))
			}
		})
	}
}

func TestPermissionEventsAreStoredOnce(t *testing.T) {
	e := newEnv(t)
	body := sample(t, "permission_events.json")
	for try := 1; try <= 2; try++ {
		if rec := e.post(permissionsPath, body); rec.Code != http.StatusNoContent {
			t.Fatalf("try %d: status = %d %s", try, rec.Code, rec.Body)
		}
	}
	if n := e.count(`SELECT count(*) FROM device_permission_events WHERE device_id = $1`, deviceID); n != 4 {
		t.Fatalf("events = %d, want the sample's 4 once", n)
	}
	var granted bool
	var at time.Time
	if err := e.pool.QueryRow(`SELECT granted, at FROM device_permission_events
		WHERE device_id = $1 AND permission = 'location_background' ORDER BY at DESC LIMIT 1`, deviceID).Scan(&granted, &at); err != nil {
		t.Fatal(err)
	}
	if granted || !at.Equal(time.Date(2026, 10, 24, 9, 40, 3, 951_000_000, time.UTC)) {
		t.Fatalf("newest location_background = %v at %v, want lost at 09:40:03.951", granted, at)
	}

	// A later change of the same permission is a new event.
	later := `{"events":[{"permission":"location_background","granted":true,"at":"2026-10-24T10:00:00Z"}]}`
	if rec := e.post(permissionsPath, []byte(later)); rec.Code != http.StatusNoContent {
		t.Fatalf("later = %d %s", rec.Code, rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM device_permission_events WHERE device_id = $1`, deviceID); n != 5 {
		t.Fatalf("events = %d, want 5", n)
	}
}

func TestPermissionEventsOfUnknownDeviceAre422(t *testing.T) {
	e := newEnv(t)
	rec := e.post("/devices/0e1f2a3b-4c5d-4e6f-8a7b-9c0d1e2f3a4b/permissions", sample(t, "permission_events.json"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d %s, want 422", rec.Code, rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM device_permission_events`); n != 0 {
		t.Fatalf("events = %d, want none", n)
	}
}

func TestBadPermissionEventsAre400(t *testing.T) {
	e := newEnv(t)
	many := make([]string, maxPermissionEvents+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"permission":"sms","granted":true,"at":"2026-10-24T09:%02d:00Z"}`, i)
	}
	cases := map[string]string{
		"no events":     `{"events":[]}`,
		"too many":      `{"events":[` + strings.Join(many, ",") + `]}`,
		"no at":         `{"events":[{"permission":"sms","granted":true}]}`,
		"bad name":      `{"events":[{"permission":"SMS; DROP","granted":true,"at":"2026-10-24T09:10:00Z"}]}`,
		"empty name":    `{"events":[{"permission":"","granted":true,"at":"2026-10-24T09:10:00Z"}]}`,
		"unknown field": `{"events":[{"permission":"sms","granted":true,"at":"2026-10-24T09:10:00Z","why":"x"}]}`,
		"not JSON":      `events`,
	}
	for name, body := range cases {
		if rec := e.post(permissionsPath, []byte(body)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d %s, want 400", name, rec.Code, rec.Body)
		}
	}
	if n := e.count(`SELECT count(*) FROM device_permission_events`); n != 0 {
		t.Fatalf("events = %d, want none", n)
	}
}

func TestValidatePermissionEvents(t *testing.T) {
	at := time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC)
	ok := []contract.PermissionEvent{
		{Permission: contract.PermissionNotifications, Granted: true, At: at},
		{Permission: contract.PermissionHibernationExempt, At: at},
		{Permission: "a_newer_permission_2", Granted: true, At: at},
	}
	if err := validatePermissionEvents(ok); err != nil {
		t.Fatalf("valid events: %v", err)
	}
	for _, bad := range [][]contract.PermissionEvent{
		nil,
		{{Permission: "Location", Granted: true, At: at}},
		{{Permission: strings.Repeat("a", 41), Granted: true, At: at}},
		{{Permission: "sms", Granted: true}},
	} {
		if err := validatePermissionEvents(bad); err == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

func TestPermissionsWithoutDatabaseIs503(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, &fakeHandoff{}, &fakeHandoff{}).Register(mux)
	e := &env{t: t, mux: mux}
	if rec := e.post(permissionsPath, []byte(`{"events":[]}`)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
