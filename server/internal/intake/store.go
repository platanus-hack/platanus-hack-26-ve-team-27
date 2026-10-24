package intake

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
)

var (
	errUnknownDevice   = errors.New("intake: device not registered")
	errUnknownIncident = errors.New("intake: incident not found")
	errDeviceMismatch  = errors.New("intake: incident belongs to another device")
	errKeyTaken        = errors.New("intake: ed25519_public_key is registered to another device")
	errKeyRequired     = errors.New("intake: device has a registered key; registration must carry it")
)

// execer is the part of *sql.DB and *sql.Tx that insertPoints needs.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// upsertDevice inserts the device or overwrites its setup, bumping updated_at.
// An empty user_name or safe_phrase (an older app) keeps the stored one.
// Once a device has a key bound, only a registration carrying that same key
// (its proof is checked before this) may update it; anything else returns
// errKeyRequired and changes nothing.
func upsertDevice(ctx context.Context, pool *sql.DB, reg contract.DeviceRegistration) error {
	family := reg.FamilyNumbers
	if family == nil {
		family = []string{}
	}
	// Untyped nil, not a nil []byte, so an absent key is NULL.
	var key, keyID any
	if len(reg.Ed25519PublicKey) > 0 {
		key = []byte(reg.Ed25519PublicKey)
		keyID = devicekeys.KeyID(ed25519.PublicKey(reg.Ed25519PublicKey))
	}
	res, err := pool.ExecContext(ctx, `
		INSERT INTO devices (id, phone_number, family_numbers, duress_phrase, app_version, ed25519_public_key, key_id, user_name, safe_phrase)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, NULLIF($8, ''), NULLIF($9, ''))
		ON CONFLICT (id) DO UPDATE SET
			phone_number       = EXCLUDED.phone_number,
			family_numbers     = EXCLUDED.family_numbers,
			duress_phrase      = EXCLUDED.duress_phrase,
			app_version        = EXCLUDED.app_version,
			ed25519_public_key = EXCLUDED.ed25519_public_key,
			key_id             = EXCLUDED.key_id,
			user_name          = COALESCE(EXCLUDED.user_name, devices.user_name),
			safe_phrase        = COALESCE(EXCLUDED.safe_phrase, devices.safe_phrase),
			updated_at         = now()
		WHERE devices.ed25519_public_key IS NULL
		   OR devices.ed25519_public_key = EXCLUDED.ed25519_public_key`,
		reg.DeviceID, reg.PhoneNumber, family, reg.DuressPhrase, reg.AppVersion, key, keyID, reg.UserName, reg.SafePhrase)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errKeyTaken
	}
	if err != nil {
		return err
	}
	// The WHERE above skipped the update: the stored key is not the request's.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errKeyRequired
	}
	return nil
}

// createIncident stores the incident with branch and speed, plus its points,
// in one transaction. For an incident_id already stored it keeps the stored
// decision, adds any new points and returns created = false. A concurrent
// retry waits on the primary key, so exactly one caller sees created = true.
func createIncident(ctx context.Context, pool *sql.DB, ev contract.AlertEvent, branch contract.Branch, speed *float64) (resp contract.AlertResponse, created bool, err error) {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return resp, false, err
	}
	defer tx.Rollback()

	var known bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1)`, ev.DeviceID).Scan(&known); err != nil {
		return resp, false, err
	}
	if !known {
		return resp, false, errUnknownDevice
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO incidents (id, device_id, trigger, branch, speed_mps, has_audio, triggered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		ev.IncidentID, ev.DeviceID, string(ev.Trigger), string(branch), speed, ev.HasAudio, ev.TriggeredAt)
	if err != nil {
		return resp, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return resp, false, err
	}
	created = n == 1

	resp = contract.AlertResponse{IncidentID: ev.IncidentID, Branch: branch, SpeedMPS: speed}
	if !created {
		var device, stored string
		var storedSpeed sql.NullFloat64
		if err := tx.QueryRowContext(ctx, `SELECT device_id, branch, speed_mps FROM incidents WHERE id = $1`, ev.IncidentID).
			Scan(&device, &stored, &storedSpeed); err != nil {
			return resp, false, err
		}
		if device != ev.DeviceID {
			return resp, false, errDeviceMismatch
		}
		resp.Branch = contract.Branch(stored)
		resp.SpeedMPS = nil
		if storedSpeed.Valid {
			resp.SpeedMPS = &storedSpeed.Float64
		}
	}

	if _, err := insertPoints(ctx, tx, ev.IncidentID, ev.Points); err != nil {
		return resp, false, err
	}
	return resp, created, tx.Commit()
}

// addPoints appends points to the incident, which must belong to deviceID.
// A resent fix (same recorded_at) is ignored. It returns how many were new.
func addPoints(ctx context.Context, pool *sql.DB, incidentID, deviceID string, points []contract.Point) (int, error) {
	var owner string
	err := pool.QueryRowContext(ctx, `SELECT device_id FROM incidents WHERE id = $1`, incidentID).Scan(&owner)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, errUnknownIncident
	case err != nil:
		return 0, err
	case owner != deviceID:
		return 0, errDeviceMismatch
	}
	return insertPoints(ctx, pool, incidentID, points)
}

func insertPoints(ctx context.Context, db execer, incidentID string, points []contract.Point) (int, error) {
	added := 0
	for _, p := range points {
		res, err := db.ExecContext(ctx, `
			INSERT INTO gps_points (incident_id, lat, lng, accuracy_m, speed_mps, recorded_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (incident_id, recorded_at) DO NOTHING`,
			incidentID, p.Lat, p.Lng, p.AccuracyM, p.SpeedMPS, p.RecordedAt)
		if err != nil {
			return added, err
		}
		if n, err := res.RowsAffected(); err == nil {
			added += int(n)
		}
	}
	return added, nil
}
