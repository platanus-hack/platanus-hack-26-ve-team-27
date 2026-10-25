package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// DBStore is the Postgres Store.
type DBStore struct {
	pool *sql.DB
}

var _ Store = DBStore{}

// NewDBStore returns the Store on pool. pool must not be nil.
func NewDBStore(pool *sql.DB) DBStore { return DBStore{pool: pool} }

// Message reads the family numbers and own phone number of the incident's
// device, its newest GPS fix (inc.Location when none is stored) and its
// verdict: duress if any call says duress, otherwise the newest judged call's
// verdict, with that call's context (calls.ficha, #106) when it has one.
func (s DBStore) Message(ctx context.Context, inc intake.Incident) (contract.NotifierMessage, error) {
	msg := contract.NotifierMessage{
		IncidentID:  inc.ID,
		To:          []string{},
		Branch:      inc.Branch,
		Location:    inc.Location,
		TriggeredAt: inc.TriggeredAt,
	}

	err := s.pool.QueryRowContext(ctx, `
		SELECT d.phone_number
		FROM incidents i JOIN devices d ON d.id = i.device_id
		WHERE i.id = $1`, inc.ID).Scan(&msg.Reporter)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// Only NOTIFY_SELF needs it: the family alert goes out without it.
		log.Printf("notify: incident %s: load the device's own number: %v", inc.ID, err)
	}

	rows, err := s.pool.QueryContext(ctx, `
		SELECT unnest(d.family_numbers)
		FROM incidents i JOIN devices d ON d.id = i.device_id
		WHERE i.id = $1`, inc.ID)
	if err != nil {
		return msg, err
	}
	defer rows.Close()
	for rows.Next() {
		var to string
		if err := rows.Scan(&to); err != nil {
			return msg, err
		}
		msg.To = append(msg.To, to)
	}
	if err := rows.Err(); err != nil {
		return msg, err
	}

	var p contract.Point
	var accuracy, speed sql.NullFloat64
	err = s.pool.QueryRowContext(ctx, `
		SELECT lat, lng, accuracy_m, speed_mps, recorded_at FROM gps_points
		WHERE incident_id = $1 ORDER BY recorded_at DESC LIMIT 1`, inc.ID).
		Scan(&p.Lat, &p.Lng, &accuracy, &speed, &p.RecordedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return msg, err
	default:
		p.AccuracyM = nullFloat(accuracy)
		p.SpeedMPS = nullFloat(speed)
		msg.Location = p
	}

	var verdict, reason string
	var ficha []byte
	err = s.pool.QueryRowContext(ctx, `
		SELECT verdict, COALESCE(verdict_reason, ''), ficha FROM calls
		WHERE incident_id = $1 AND verdict IS NOT NULL
		ORDER BY verdict = 'duress' DESC, requested_at DESC, id DESC LIMIT 1`, inc.ID).
		Scan(&verdict, &reason, &ficha)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return msg, err
	default:
		msg.Verdict = contract.Verdict(verdict)
		msg.Reason = reason
		if len(ficha) > 0 {
			var c contract.CallContext
			if err := json.Unmarshal(ficha, &c); err != nil {
				// The alert goes out without the context.
				log.Printf("notify: incident %s: read the call context: %v", inc.ID, err)
			} else {
				msg.Ficha = &c
			}
		}
	}
	return msg, nil
}

// Claim runs the contract's claim: it sends only if this UPDATE changed a row.
func (s DBStore) Claim(ctx context.Context, incidentID string) (bool, error) {
	res, err := s.pool.ExecContext(ctx,
		`UPDATE incidents SET notified_at = now() WHERE id = $1 AND notified_at IS NULL`, incidentID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Release clears notified_at after a send that reached nobody.
func (s DBStore) Release(ctx context.Context, incidentID string) error {
	_, err := s.pool.ExecContext(ctx, `UPDATE incidents SET notified_at = NULL WHERE id = $1`, incidentID)
	return err
}

func nullFloat(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}
