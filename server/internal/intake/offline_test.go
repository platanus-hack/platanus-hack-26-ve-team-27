package intake

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
)

const emergencyID = "00c0ffee12345678"

// offlineEvent is an open emergency of the sample device; loc is nil for a no-fix frame.
func offlineEvent(seq uint16, loc *ingest.Location) ingest.EmergencyReceived {
	return ingest.EmergencyReceived{
		EmergencyID: emergencyID,
		DeviceID:    deviceID,
		Status:      ingest.StatusOpen,
		Location:    loc,
		Channel:     ingest.ChannelMesh,
		Hops:        1,
		Seq:         seq,
		SentAt:      at(int(seq)),
		ReceivedAt:  at(int(seq)).Add(time.Second),
	}
}

func fix(latE5, lonE5 int32, sec int) *ingest.Location {
	return &ingest.Location{LatE5: latE5, LonE5: lonE5, AgeS: 2, SentAt: at(sec)}
}

func (e *env) publisher() OfflinePublisher {
	h := New(e.pool, e.fake, e.fake)
	h.spawn = func(f func()) { f() }
	return h.OfflinePublisher()
}

func TestOfflineEmergencyOpensOneIncidentAndNotifiesOnce(t *testing.T) {
	e := newEnv(t)
	pub := e.publisher()
	ctx := context.Background()

	pub.Publish(ctx, offlineEvent(0, fix(1048000, -6690000, 0)))
	// The same emergency again (another channel, or a re-published event).
	pub.Publish(ctx, offlineEvent(0, fix(1048000, -6690000, 0)))
	// An UPDATE frame with a fresh fix.
	pub.Publish(ctx, offlineEvent(1, fix(1048100, -6690100, 30)))

	id := OfflineIncidentID(emergencyID)
	if n := e.count(`SELECT count(*) FROM incidents WHERE id = $1 AND device_id = $2 AND trigger = 'offline' AND branch = 'immediate_alert'`, id, deviceID); n != 1 {
		t.Fatalf("offline incidents = %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM gps_points WHERE incident_id = $1`, id); n != 2 {
		t.Fatalf("points = %d, want 2", n)
	}
	if n := e.count(`SELECT count(*) FROM gps_points WHERE incident_id = $1 AND lat = 10.48 AND lng = -66.9 AND recorded_at = $2`, id, at(0).Add(-2*time.Second)); n != 1 {
		t.Fatalf("first point not stored at the fix time")
	}
	if len(e.fake.notifies) != 1 || len(e.fake.calls) != 0 {
		t.Fatalf("notifies=%d calls=%d, want 1 and 0", len(e.fake.notifies), len(e.fake.calls))
	}
	got := e.fake.notifies[0]
	if got.ID != id || got.Branch != contract.BranchImmediateAlert || got.Location.Lat != 10.48 || got.Location.Lng != -66.9 {
		t.Fatalf("handoff = %+v", got)
	}
}

func TestOfflineEmergencyWithoutFixStillNotifies(t *testing.T) {
	e := newEnv(t)
	e.publisher().Publish(context.Background(), offlineEvent(0, nil))

	id := OfflineIncidentID(emergencyID)
	if n := e.count(`SELECT count(*) FROM incidents WHERE id = $1`, id); n != 1 {
		t.Fatalf("incidents = %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM gps_points WHERE incident_id = $1`, id); n != 0 {
		t.Fatalf("points = %d, want 0", n)
	}
	if len(e.fake.notifies) != 1 {
		t.Fatalf("notifies = %d, want 1", len(e.fake.notifies))
	}
}

func TestCancelledOfflineEmergencyOpensNothing(t *testing.T) {
	e := newEnv(t)
	pub := e.publisher()
	ctx := context.Background()

	cancelled := offlineEvent(1, nil)
	cancelled.Status = ingest.StatusCancelled
	pub.Publish(ctx, cancelled)

	// The open event published after the stored cancel (out of order) opens nothing either.
	if _, err := e.pool.Exec(`
		INSERT INTO emergencies (emergency_id, device_id, status, seq, sent_at, channels, first_received_at, updated_at, cancelled_at)
		VALUES ($1, $2, 'cancelled', 1, $3, '{mesh}', $3, $3, $3)`, emergencyID, deviceID, at(1)); err != nil {
		t.Fatal(err)
	}
	pub.Publish(ctx, offlineEvent(0, fix(1048000, -6690000, 0)))

	if n := e.count(`SELECT count(*) FROM incidents`); n != 0 {
		t.Fatalf("incidents = %d, want 0", n)
	}
	if len(e.fake.notifies) != 0 {
		t.Fatalf("notifies = %d, want 0", len(e.fake.notifies))
	}
}

func TestOfflineEmergencyOfUnknownDeviceOpensNothing(t *testing.T) {
	e := newEnv(t)
	ev := offlineEvent(0, fix(1048000, -6690000, 0))
	ev.DeviceID = "00000000-0000-4000-8000-000000000000"
	e.publisher().Publish(context.Background(), ev)
	if n := e.count(`SELECT count(*) FROM incidents`); n != 0 {
		t.Fatalf("incidents = %d, want 0", n)
	}
}

func TestOfflineIncidentIDIsAStableUUID(t *testing.T) {
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a := OfflineIncidentID("00c0ffee12345678")
	if !uuid.MatchString(a) {
		t.Fatalf("%q is not a version 8 UUID", a)
	}
	if OfflineIncidentID("00c0ffee12345678") != a {
		t.Fatal("not stable")
	}
	if OfflineIncidentID("00c0ffee12345679") == a {
		t.Fatal("two emergencies share an incident id")
	}
}
