package ingest

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
)

// DBStore is the Postgres Store (table emergencies, migration 0003).
type DBStore struct{ pool *sql.DB }

var _ Store = DBStore{}

// NewDBStore returns a Store on pool.
func NewDBStore(pool *sql.DB) DBStore { return DBStore{pool: pool} }

// Device looks up the registered device with the payload's key id.
func (s DBStore) Device(ctx context.Context, keyID uint64) (devicekeys.Device, error) {
	return devicekeys.Lookup(ctx, s.pool, keyID)
}

const selectEmergency = `SELECT device_id, status, seq, sent_at, lat_e5, lon_e5, loc_age_s, location_sent_at,
	array_to_string(channels, ','), min_hops, first_received_at, updated_at, cancelled_at
	FROM emergencies WHERE emergency_id = $1`

const upsertEmergency = `INSERT INTO emergencies (emergency_id, device_id, status, seq, sent_at, lat_e5, lon_e5, loc_age_s,
	location_sent_at, channels, min_hops, first_received_at, updated_at, cancelled_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::text[], $11, $12, $13, $14)
	ON CONFLICT (emergency_id) DO UPDATE SET
	status = EXCLUDED.status, seq = EXCLUDED.seq, sent_at = EXCLUDED.sent_at,
	lat_e5 = EXCLUDED.lat_e5, lon_e5 = EXCLUDED.lon_e5, loc_age_s = EXCLUDED.loc_age_s,
	location_sent_at = EXCLUDED.location_sent_at, channels = EXCLUDED.channels, min_hops = EXCLUDED.min_hops,
	updated_at = EXCLUDED.updated_at, cancelled_at = EXCLUDED.cancelled_at
	WHERE emergencies.device_id = EXCLUDED.device_id`

// Update implements Store. One transaction holds pg_advisory_xact_lock on the
// id (this also serializes the first insert, where there is no row to lock),
// reads the row, runs apply, and upserts the result. Another device cannot
// overwrite a row: apply (merge) returns ErrConflict first and nothing is saved,
// and the upsert itself only updates a row with the same device_id (ErrConflict
// if it touched none).
//
// Any other writer of emergencies (for example the Kapso module setting
// acked) must take the same pg_advisory_xact_lock(emergency_id) in its
// transaction, or its update can be lost to a concurrent ingest upsert.
func (s DBStore) Update(ctx context.Context, emergencyID uint64, apply func(prev *Emergency) (Emergency, error)) (Emergency, error) {
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return Emergency{}, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(emergencyID)); err != nil {
		return Emergency{}, err
	}
	key := FormatID(emergencyID)

	var prev *Emergency
	var (
		e                      Emergency
		status, channels       string
		seq                    int
		lat, lon, age, minHops sql.NullInt64
		locAt, cancelledAt     sql.NullTime
	)
	err = tx.QueryRowContext(ctx, selectEmergency, key).Scan(&e.DeviceID, &status, &seq, &e.SentAt, &lat, &lon, &age, &locAt,
		&channels, &minHops, &e.FirstReceivedAt, &e.UpdatedAt, &cancelledAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Emergency{}, err
	default:
		e.ID, e.Status, e.Seq = emergencyID, Status(status), uint16(seq)
		for _, c := range strings.Split(channels, ",") {
			e.Channels = append(e.Channels, Channel(c))
		}
		e.MinHops = HopsUnknown
		if minHops.Valid {
			e.MinHops = int(minHops.Int64)
		}
		if lat.Valid {
			e.Location = &Location{LatE5: int32(lat.Int64), LonE5: int32(lon.Int64), AgeS: uint16(age.Int64), SentAt: locAt.Time}
		}
		if cancelledAt.Valid {
			t := cancelledAt.Time
			e.CancelledAt = &t
		}
		prev = &e
	}

	next, err := apply(prev)
	if err != nil {
		return Emergency{}, err
	}

	var latV, lonV, ageV, locAtV, hopsV, cancelV any
	if l := next.Location; l != nil {
		latV, lonV, ageV, locAtV = l.LatE5, l.LonE5, int(l.AgeS), l.SentAt
	}
	if next.MinHops != HopsUnknown {
		hopsV = next.MinHops
	}
	if next.CancelledAt != nil {
		cancelV = *next.CancelledAt
	}
	chans := make([]string, len(next.Channels))
	for i, c := range next.Channels {
		chans[i] = string(c)
	}
	res, err := tx.ExecContext(ctx, upsertEmergency, key, next.DeviceID, string(next.Status), int(next.Seq), next.SentAt,
		latV, lonV, ageV, locAtV, chans, hopsV, next.FirstReceivedAt, next.UpdatedAt, cancelV)
	if err != nil {
		return Emergency{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Emergency{}, err
	} else if n == 0 {
		return Emergency{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return Emergency{}, err
	}
	return next, nil
}
