package report

import (
	"context"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Load reads the alert's permission snapshot and the device's permission
// events (#125), and nothing when there are none.
func TestLoadReadsThePermissions(t *testing.T) {
	pool := newDB(t)
	s := NewDBStore(pool)
	ctx := context.Background()
	rec, err := s.Load(ctx, testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.DeviceState != nil || rec.PermissionEvents != nil {
		t.Fatalf("without #125 data: device_state %+v, events %+v; want nil", rec.DeviceState, rec.PermissionEvents)
	}

	deviceID := sealedRecord().Incident.DeviceID
	exec(t, pool, `UPDATE incidents SET device_state = $2::jsonb WHERE id = $1`, testIncidentID,
		`{"captured_at":"2026-10-24T09:10:00.251Z","permissions":{"location_fine":true,"battery_unrestricted":false}}`)
	at := time.Date(2026, 10, 24, 8, 2, 0, 0, time.UTC)
	exec(t, pool, `INSERT INTO device_permission_events (device_id, permission, granted, at) VALUES
		($1, 'location_background', false, $2), ($1, 'notifications', true, $3), ($1, 'location_background', true, $4)`,
		deviceID, at.Add(time.Hour), at, at.Add(time.Minute))
	exec(t, pool, `INSERT INTO devices (id, phone_number, duress_phrase) VALUES ('otro', '+584140000000', 'x')`)
	exec(t, pool, `INSERT INTO device_permission_events (device_id, permission, granted, at) VALUES ('otro', 'sms', true, $1)`, at)

	rec, err = s.Load(ctx, testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	want := contract.DeviceState{
		CapturedAt:  time.Date(2026, 10, 24, 9, 10, 0, 251_000_000, time.UTC),
		Permissions: map[string]bool{contract.PermissionLocationFine: true, contract.PermissionBatteryUnrestricted: false},
	}
	if rec.DeviceState == nil || !rec.DeviceState.CapturedAt.Equal(want.CapturedAt) || len(rec.DeviceState.Permissions) != 2 ||
		!rec.DeviceState.Permissions[contract.PermissionLocationFine] || rec.DeviceState.Permissions[contract.PermissionBatteryUnrestricted] {
		t.Errorf("device_state = %+v, want %+v", rec.DeviceState, want)
	}
	got := make([]string, len(rec.PermissionEvents))
	for i, e := range rec.PermissionEvents {
		got[i] = e.Permission + "=" + map[bool]string{true: "on", false: "off"}[e.Granted] + "@" + e.At.UTC().Format("15:04")
		if e.ReceivedAt.IsZero() {
			t.Errorf("event %d has no received_at", i)
		}
	}
	wantEvents := []string{"notifications=on@08:02", "location_background=on@08:03", "location_background=off@09:02"}
	if len(got) != len(wantEvents) {
		t.Fatalf("events = %v, want %v (this device's, oldest first)", got, wantEvents)
	}
	for i := range got {
		if got[i] != wantEvents[i] {
			t.Fatalf("events = %v, want %v (this device's, oldest first)", got, wantEvents)
		}
	}

	// A flood keeps only the newest maxPermissionEvents, still oldest first.
	exec(t, pool, `INSERT INTO device_permission_events (device_id, permission, granted, at)
		SELECT $1, 'sms', n % 2 = 0, $2::timestamptz + n * interval '1 second' FROM generate_series(1, $3) n`,
		deviceID, at.Add(2*time.Hour), maxPermissionEvents)
	rec, err = s.Load(ctx, testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(rec.PermissionEvents); n != maxPermissionEvents {
		t.Fatalf("events = %d, want %d", n, maxPermissionEvents)
	}
	first, last := rec.PermissionEvents[0], rec.PermissionEvents[maxPermissionEvents-1]
	if first.Permission != "sms" || !last.At.After(first.At) {
		t.Fatalf("first %+v, last %+v: want only the flood's sms events, oldest first", first, last)
	}
}

func TestParseDeviceState(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"captured_at":"2026-10-24T09:10:00Z","permissions":{}}`, `[1]`} {
		if got := parseDeviceState([]byte(raw)); got != nil {
			t.Errorf("parseDeviceState(%q) = %+v, want nil", raw, got)
		}
	}
	if got := parseDeviceState([]byte(`{"captured_at":"2026-10-24T09:10:00Z","permissions":{"sms":true}}`)); got == nil || !got.Permissions["sms"] {
		t.Errorf("parseDeviceState = %+v", got)
	}
}
