package report

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

const (
	cancelledID = "11111111-1111-4111-8111-111111111111"
	callingID   = "22222222-2222-4222-8222-222222222222"
)

// centralDB is newDB plus an older cancelled case and a newer case whose
// call is ringing, from another device.
func centralDB(t *testing.T) *DBStore {
	t.Helper()
	pool := newDB(t)
	exec(t, pool, `INSERT INTO devices (id, phone_number, family_numbers, duress_phrase) VALUES ('dev-2', '+584247779012', '{+584121234567}', 'trae las llaves')`)
	// Cancelled, before the fixture: said it is fine, nobody notified.
	exec(t, pool, `INSERT INTO incidents (id, device_id, trigger, branch, speed_mps, triggered_at, created_at)
		VALUES ($1, 'dev-2', 'widget', 'call', 1.0, '2026-10-24T13:00:00Z', '2026-10-24T13:00:00.500Z')`, cancelledID)
	exec(t, pool, `INSERT INTO calls (incident_id, status, requested_at, ended_at, duration_secs, verdict, verdict_reason, verdict_at)
		VALUES ($1, 'done', '2026-10-24T13:00:01Z', '2026-10-24T13:00:40Z', 30, 'no_duress', 'the user said the safe phrase', '2026-10-24T13:00:42Z')`, cancelledID)
	// Calling, after the fixture: ringing, with two points.
	exec(t, pool, `INSERT INTO incidents (id, device_id, trigger, branch, speed_mps, triggered_at, created_at)
		VALUES ($1, 'dev-2', 'volume_pattern', 'call', 1.3, '2026-10-24T13:11:20Z', '2026-10-24T13:11:20.400Z')`, callingID)
	exec(t, pool, `INSERT INTO gps_points (incident_id, lat, lng, accuracy_m, speed_mps, recorded_at, received_at) VALUES
		($1, 10.5, -66.9, 20, 1.3, '2026-10-24T13:11:20Z', '2026-10-24T13:11:20.500Z'),
		($1, 10.501, -66.899, 7, 1.4, '2026-10-24T13:11:35Z', '2026-10-24T13:11:35.500Z')`, callingID)
	exec(t, pool, `INSERT INTO calls (incident_id, status, requested_at) VALUES ($1, 'in_progress', '2026-10-24T13:11:21Z')`, callingID)
	exec(t, pool, `INSERT INTO call_progress (call_id, twilio_status, ringing_at, updated_at)
		SELECT id, 'ringing', '2026-10-24T13:11:25Z', '2026-10-24T13:11:25Z' FROM calls WHERE incident_id = $1`, callingID)
	// The fixture's family messages: one read, one sent.
	exec(t, pool, `INSERT INTO notify_deliveries (incident_id, recipient, channel, message_id, status, read_at, delivered_at) VALUES
		($1, '+58 424 ···33', 'whatsapp', 'wamid.a', 'read', now(), now()), ($1, '+58 412 ···77', 'sms', 'SM.b', 'accepted', NULL, NULL)`, testIncidentID)
	s := NewDBStore(pool)
	return &s
}

// Cases lists the newest first, each with the state the detail gives it
// too, and reads user_name only once the column exists (#104).
func TestCasesNewestFirstWithTheirStatesOnPostgres(t *testing.T) {
	s := centralDB(t)
	ctx := context.Background()
	now := at("2026-10-24T13:11:40Z")
	items, err := s.Cases(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].ID != callingID || items[1].ID != testIncidentID || items[2].ID != cancelledID {
		t.Fatalf("order = %v", ids(items))
	}
	want := map[string]string{callingID: CaseCalling, testIncidentID: CaseAlert, cancelledID: CaseCancelled}
	for _, it := range items {
		if got := caseState(it, now); got != want[it.ID] {
			t.Errorf("%s: state %s, want %s", it.ID, got, want[it.ID])
		}
		// The detail sums up the full record the same way.
		rec, err := s.Load(ctx, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		x, err := s.Extras(ctx, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if a, b := caseRow(it, now), caseRow(caseItemOf(rec, x), now); a != b {
			t.Errorf("%s: list row and detail header differ:\n%+v\n%+v", it.ID, a, b)
		}
	}
	calling, alert := items[0], items[1]
	if calling.Last == nil || calling.Last.Lat != 10.501 || calling.Call == nil || calling.Call.RingingAt == nil || calling.UserName != "" {
		t.Errorf("calling = %+v", calling)
	}
	if alert.Verdict != contract.VerdictDuress || alert.Deliveries != 2 || alert.Delivered != 1 || alert.NotifiedAt == nil {
		t.Errorf("alert = %+v", alert)
	}
	if r := caseRow(calling, now); r.Line != "Sonando" || r.LineSince == "" {
		t.Errorf("calling row = %+v", r)
	}
	if items, _ := s.Cases(ctx, 1); len(items) != 1 || items[0].ID != callingID {
		t.Errorf("limit 1 = %v", ids(items))
	}

	exec(t, s.pool, `ALTER TABLE devices ADD COLUMN IF NOT EXISTS user_name TEXT`)
	exec(t, s.pool, `UPDATE devices SET user_name = 'Carla Díaz' WHERE id = 'dev-2'`)
	items, err = s.Cases(ctx, 10)
	if err != nil || items[0].UserName != "Carla Díaz" || items[1].UserName != "" {
		t.Fatalf("with user_name: %v %+v", err, items)
	}
}

// Extras reads #106's calls.ficha and an incident cancellation column once
// they exist, and nothing before.
func TestExtrasReadsTheFichaOnceTheColumnExistsOnPostgres(t *testing.T) {
	s := centralDB(t)
	ctx := context.Background()
	x, err := s.Extras(ctx, testIncidentID)
	if err != nil || x.Ficha != nil || x.Cancelled {
		t.Fatalf("before the columns: %+v %v", x, err)
	}
	if _, err := s.Extras(ctx, "00000000-0000-4000-8000-000000000000"); err != ErrUnknownIncident {
		t.Errorf("unknown incident: %v", err)
	}
	exec(t, s.pool, `ALTER TABLE calls ADD COLUMN ficha JSONB`)
	if x, _ := s.Extras(ctx, testIncidentID); x.Ficha != nil {
		t.Errorf("an empty ficha column: %s", x.Ficha)
	}
	exec(t, s.pool, `UPDATE calls SET ficha = $2 WHERE incident_id = $1`, testIncidentID, testFicha)
	x, err = s.Extras(ctx, testIncidentID)
	if err != nil || situationOf(x.Ficha) != "Parece que se la llevan en un carro." {
		t.Fatalf("with a ficha: %s %v", x.Ficha, err)
	}
	var m map[string]any
	if json.Unmarshal(x.Ficha, &m) != nil {
		t.Errorf("ficha is not an object: %s", x.Ficha)
	}

	exec(t, s.pool, `ALTER TABLE incidents ADD COLUMN cancelled_at TIMESTAMPTZ`)
	exec(t, s.pool, `UPDATE incidents SET cancelled_at = now() WHERE id = $1`, callingID)
	if x, _ := s.Extras(ctx, callingID); !x.Cancelled {
		t.Error("cancelled_at did not cancel")
	}
	items, _ := s.Cases(ctx, 10)
	if caseState(items[0], time.Now()) != CaseCancelled {
		t.Errorf("list state with cancelled_at = %s", caseState(items[0], time.Now()))
	}
}

func ids(items []CaseItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
