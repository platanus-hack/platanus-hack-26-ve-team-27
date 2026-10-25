package notify

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// newDB migrates a throwaway database with one device (two family numbers)
// and one incident. It skips without TEST_DATABASE_URL.
func newDB(t *testing.T, branch contract.Branch) *sql.DB {
	t.Helper()
	pool := dbtest.Fresh(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	exec(t, pool, `INSERT INTO devices (id, phone_number, family_numbers, duress_phrase)
		VALUES ('dev-1', '+584141234567', '{+584241112233,+584125556677}', 'frase')`)
	exec(t, pool, `INSERT INTO incidents (id, device_id, trigger, branch, triggered_at)
		VALUES ($1, 'dev-1', 'widget', $2, '2026-10-24T13:10:00Z')`, incidentID, string(branch))
	return pool
}

func exec(t *testing.T, pool *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func incident(branch contract.Branch) intake.Incident {
	return intake.Incident{
		ID:          incidentID,
		DeviceID:    "dev-1",
		Branch:      branch,
		Location:    contract.Point{Lat: 1, Lng: 2, RecordedAt: time.Date(2026, 10, 24, 13, 9, 0, 0, time.UTC)},
		TriggeredAt: time.Date(2026, 10, 24, 13, 10, 0, 0, time.UTC),
	}
}

func TestDBStoreMessageWithoutCall(t *testing.T) {
	pool := newDB(t, contract.BranchImmediateAlert)
	store := NewDBStore(pool)
	inc := incident(contract.BranchImmediateAlert)

	// No stored fix: the incident's own location is used.
	msg, err := store.Message(context.Background(), inc)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.To) != 2 || msg.To[0] != "+584241112233" || msg.To[1] != "+584125556677" {
		t.Errorf("To = %v", msg.To)
	}
	if msg.Reporter != "+584141234567" {
		t.Errorf("Reporter = %q, want the device's phone_number", msg.Reporter)
	}
	if msg.Location.Lat != 1 || msg.Verdict != "" || msg.Reason != "" || msg.Branch != contract.BranchImmediateAlert {
		t.Errorf("msg = %+v, want inc location and no verdict", msg)
	}

	exec(t, pool, `INSERT INTO gps_points (incident_id, lat, lng, accuracy_m, recorded_at) VALUES
		($1, 10.4950, -66.8540, NULL, '2026-10-24T13:10:00Z'),
		($1, 10.4961, -66.8531, 12.5, '2026-10-24T13:11:00Z')`, incidentID)
	msg, err = store.Message(context.Background(), inc)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Location.Lat != 10.4961 || msg.Location.AccuracyM == nil || *msg.Location.AccuracyM != 12.5 {
		t.Errorf("Location = %+v, want the newest fix", msg.Location)
	}
}

// Contract: duress if any call says duress, otherwise the newest call's
// verdict.
func TestDBStoreMessageVerdict(t *testing.T) {
	pool := newDB(t, contract.BranchCall)
	store := NewDBStore(pool)
	verdict := func() contract.Verdict {
		t.Helper()
		msg, err := store.Message(context.Background(), incident(contract.BranchCall))
		if err != nil {
			t.Fatal(err)
		}
		return msg.Verdict
	}

	exec(t, pool, `INSERT INTO calls (incident_id, requested_at, verdict, verdict_reason)
		VALUES ($1, '2026-10-24T13:10:00Z', 'unclear', 'first')`, incidentID)
	exec(t, pool, `INSERT INTO calls (incident_id, requested_at, verdict, verdict_reason)
		VALUES ($1, '2026-10-24T13:12:00Z', 'no_duress', 'second')`, incidentID)
	exec(t, pool, `INSERT INTO calls (incident_id, requested_at) VALUES ($1, '2026-10-24T13:13:00Z')`, incidentID)
	if got := verdict(); got != contract.VerdictNoDuress {
		t.Fatalf("verdict = %q, want the newest judged call's no_duress", got)
	}

	exec(t, pool, `INSERT INTO calls (incident_id, requested_at, verdict, verdict_reason)
		VALUES ($1, '2026-10-24T13:05:00Z', 'duress', 'oldest')`, incidentID)
	if got := verdict(); got != contract.VerdictDuress {
		t.Fatalf("verdict = %q, want duress from any call", got)
	}
}

func TestDBStoreClaimIsAtMostOnce(t *testing.T) {
	pool := newDB(t, contract.BranchImmediateAlert)
	store := NewDBStore(pool)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := store.Claim(ctx, incidentID)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("concurrent claims won = %d, want 1", won)
	}

	if err := store.Release(ctx, incidentID); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Claim(ctx, incidentID); err != nil || !ok {
		t.Fatalf("claim after release = %v, %v; want true", ok, err)
	}
}

// A cancelled incident (POST /incidents/{id}/cancel) is never claimed, so its
// family message is not sent.
func TestDBStoreClaimRefusesACancelledIncident(t *testing.T) {
	pool := newDB(t, contract.BranchImmediateAlert)
	store := NewDBStore(pool)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, `UPDATE incidents SET cancelled_at = now() WHERE id = $1`, incidentID); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Claim(ctx, incidentID); err != nil || ok {
		t.Fatalf("claim of a cancelled incident = %v, %v; want false", ok, err)
	}

	k := newKapsoServer(t, http.StatusOK)
	New(store, k.sender()).Notify(ctx, incident(contract.BranchImmediateAlert))
	if got := k.count(); got != 0 {
		t.Errorf("Kapso requests = %d, want 0 for a cancelled incident", got)
	}
}

// End to end on the real claim: concurrent and retried Notify calls send one
// WhatsApp per family number, and notified_at is set.
func TestNotifyOnDatabaseSendsOncePerFamilyNumber(t *testing.T) {
	pool := newDB(t, contract.BranchSilentTracker)
	k := newKapsoServer(t, http.StatusOK)
	n := New(NewDBStore(pool), k.sender())
	inc := incident(contract.BranchSilentTracker)

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Notify(context.Background(), inc)
		}()
	}
	wg.Wait()
	n.Notify(context.Background(), inc)

	if got := k.count(); got != 2 {
		t.Fatalf("Kapso requests = %d, want 2 (one per family number)", got)
	}
	if k.messages[0].To != "584241112233" || k.messages[1].To != "584125556677" {
		t.Errorf("to = %q, %q; want the family numbers without +", k.messages[0].To, k.messages[1].To)
	}
	var notified sql.NullTime
	if err := pool.QueryRow(`SELECT notified_at FROM incidents WHERE id = $1`, incidentID).Scan(&notified); err != nil || !notified.Valid {
		t.Fatalf("notified_at = %v, %v; want set", notified, err)
	}
}

func TestNotifyOnDatabaseReleasesAfterFailedSend(t *testing.T) {
	pool := newDB(t, contract.BranchImmediateAlert)
	k := newKapsoServer(t, http.StatusUnprocessableEntity)
	New(NewDBStore(pool), k.sender()).Notify(context.Background(), incident(contract.BranchImmediateAlert))

	var notified sql.NullTime
	if err := pool.QueryRow(`SELECT notified_at FROM incidents WHERE id = $1`, incidentID).Scan(&notified); err != nil || notified.Valid {
		t.Fatalf("notified_at = %v, %v; want NULL so a retry can send", notified, err)
	}
}
