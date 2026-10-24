package ingest

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

const storeDeviceID = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"

func storePub() ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
}

// storePool returns a migrated database with one registered device.
func storePool(t *testing.T) *sql.DB {
	t.Helper()
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pub := storePub()
	if _, err := pool.Exec(`INSERT INTO devices (id, phone_number, duress_phrase, ed25519_public_key, key_id) VALUES ($1, '+584141234567', 'p', $2, $3)`,
		storeDeviceID, []byte(pub), devicekeys.KeyID(pub)); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestDBStoreDevice(t *testing.T) {
	pool := storePool(t)
	s := NewDBStore(pool)
	pub := storePub()
	d, err := s.Device(context.Background(), payload.DeviceID(pub))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != storeDeviceID || !d.PublicKey.Equal(pub) {
		t.Fatalf("Device = %+v", d)
	}
	if _, err := s.Device(context.Background(), 0x0102030405060708); !errors.Is(err, devicekeys.ErrNotFound) {
		t.Fatalf("unknown key id: err = %v, want ErrNotFound", err)
	}
}

func sameEmergency(t *testing.T, got, want Emergency) {
	t.Helper()
	if got.ID != want.ID || got.DeviceID != want.DeviceID || got.Status != want.Status || got.Seq != want.Seq || got.MinHops != want.MinHops {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.SentAt.Equal(want.SentAt) || !got.FirstReceivedAt.Equal(want.FirstReceivedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times differ: got %+v, want %+v", got, want)
	}
	if len(got.Channels) != len(want.Channels) {
		t.Fatalf("channels = %v, want %v", got.Channels, want.Channels)
	}
	for i := range want.Channels {
		if got.Channels[i] != want.Channels[i] {
			t.Fatalf("channels = %v, want %v", got.Channels, want.Channels)
		}
	}
	if (got.Location == nil) != (want.Location == nil) {
		t.Fatalf("location = %+v, want %+v", got.Location, want.Location)
	}
	if want.Location != nil {
		g, w := *got.Location, *want.Location
		if g.LatE5 != w.LatE5 || g.LonE5 != w.LonE5 || g.AgeS != w.AgeS || !g.SentAt.Equal(w.SentAt) {
			t.Fatalf("location = %+v, want %+v", g, w)
		}
	}
	if (got.CancelledAt == nil) != (want.CancelledAt == nil) || (want.CancelledAt != nil && !got.CancelledAt.Equal(*want.CancelledAt)) {
		t.Fatalf("cancelled_at = %v, want %v", got.CancelledAt, want.CancelledAt)
	}
}

func TestDBStoreRoundTrip(t *testing.T) {
	pool := storePool(t)
	s := NewDBStore(pool)
	ctx := context.Background()
	const id uint64 = 0xfedcba9876543210
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

	full := Emergency{
		ID: id, DeviceID: storeDeviceID, Status: StatusOpen, Seq: 3, SentAt: now.Add(-time.Minute),
		Location:        &Location{LatE5: 1043000, LonE5: -6690000, AgeS: 12, SentAt: now.Add(-time.Minute)},
		Channels:        []Channel{ChannelMesh, ChannelHTTP},
		MinHops:         2,
		FirstReceivedAt: now, UpdatedAt: now.Add(time.Second),
	}
	roundTrip := func(id uint64, rec Emergency) {
		t.Helper()
		if _, err := s.Update(ctx, id, func(prev *Emergency) (Emergency, error) {
			if prev != nil {
				t.Fatalf("first Update saw prev %+v", prev)
			}
			return rec, nil
		}); err != nil {
			t.Fatal(err)
		}
		var seen *Emergency
		if _, err := s.Update(ctx, id, func(prev *Emergency) (Emergency, error) {
			seen = prev
			return *prev, nil
		}); err != nil {
			t.Fatal(err)
		}
		if seen == nil {
			t.Fatal("second Update saw no prev")
		}
		sameEmergency(t, *seen, rec)
	}
	roundTrip(id, full)
	var hexID string
	if err := pool.QueryRow(`SELECT emergency_id FROM emergencies`).Scan(&hexID); err != nil || hexID != "fedcba9876543210" {
		t.Fatalf("emergency_id = %q, err %v", hexID, err)
	}

	cancelledAt := now.Add(2 * time.Second)
	bare := Emergency{
		ID: 7, DeviceID: storeDeviceID, Status: StatusCancelled, Seq: 1, SentAt: now,
		Channels: []Channel{ChannelSMS}, MinHops: HopsUnknown,
		FirstReceivedAt: now, UpdatedAt: cancelledAt, CancelledAt: &cancelledAt,
	}
	roundTrip(7, bare)
	var lat, hops sql.NullInt64
	if err := pool.QueryRow(`SELECT lat_e5, min_hops FROM emergencies WHERE emergency_id = '0000000000000007'`).Scan(&lat, &hops); err != nil {
		t.Fatal(err)
	}
	if lat.Valid || hops.Valid {
		t.Fatalf("lat_e5 %v min_hops %v, want NULL", lat, hops)
	}
}

func TestDBStoreApplyErrorSavesNothing(t *testing.T) {
	pool := storePool(t)
	s := NewDBStore(pool)
	_, err := s.Update(context.Background(), 1, func(*Emergency) (Emergency, error) {
		return Emergency{}, ErrConflict
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	var n int
	if err := pool.QueryRow(`SELECT count(*) FROM emergencies`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("count = %d, err %v", n, err)
	}
}

func TestDBStoreUpdateSerializesPerID(t *testing.T) {
	pool := storePool(t)
	s := NewDBStore(pool)
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	const id uint64 = 0xabcdef0123456789
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Update(context.Background(), id, func(prev *Emergency) (Emergency, error) {
				if prev == nil {
					return Emergency{ID: id, DeviceID: storeDeviceID, Status: StatusOpen, Seq: 0, SentAt: now,
						Channels: []Channel{ChannelHTTP}, MinHops: HopsUnknown, FirstReceivedAt: now, UpdatedAt: now}, nil
				}
				next := *prev
				next.Seq = prev.Seq + 1
				return next, nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var seq, n int
	if err := pool.QueryRow(`SELECT max(seq), count(*) FROM emergencies`).Scan(&seq, &n); err != nil {
		t.Fatal(err)
	}
	if seq != 9 || n != 1 {
		t.Fatalf("seq = %d rows = %d, want 9 and 1", seq, n)
	}
}

func TestMigrationChecks(t *testing.T) {
	pool := storePool(t)
	const base = `INSERT INTO emergencies (emergency_id, device_id, status, seq, sent_at, lat_e5, lon_e5, loc_age_s, location_sent_at, channels, first_received_at, updated_at, cancelled_at)
		VALUES ($1, $2, $3, 0, now(), $4, $5, $6, $7, $8::text[], now(), now(), $9)`
	cases := []struct {
		name                            string
		id, status, channels            string
		lat, lon, age, locAt, cancelled any
	}{
		{"bad id", "XYZ", "open", "{http}", nil, nil, nil, nil, nil},
		{"bad status", "0000000000000001", "closed", "{http}", nil, nil, nil, nil, nil},
		{"bad channel", "0000000000000001", "open", "{fax}", nil, nil, nil, nil, nil},
		{"lat without lon", "0000000000000001", "open", "{http}", 1, nil, 0, time.Now(), nil},
		{"cancelled without cancelled_at", "0000000000000001", "cancelled", "{http}", nil, nil, nil, nil, nil},
	}
	for _, c := range cases {
		if _, err := pool.Exec(base, c.id, storeDeviceID, c.status, c.lat, c.lon, c.age, c.locAt, c.channels, c.cancelled); err == nil {
			t.Errorf("%s: insert was accepted", c.name)
		}
	}
	// A valid row still goes in, so the failures above are the checks' doing.
	if _, err := pool.Exec(base, "0000000000000001", storeDeviceID, "open", nil, nil, nil, nil, "{http,sms}", nil); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}
}
