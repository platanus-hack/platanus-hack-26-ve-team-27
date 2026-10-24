package report

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// CaseItem is one incident as the central dashboard's list sums it up: who,
// when, the newest point and call, the headline verdict and the family
// messages. DBStore.Cases reads it in one query; caseItemOf builds the same
// from a full Record, so the list and the detail never disagree on a state.
type CaseItem struct {
	ID          string
	Trigger     contract.Trigger
	Branch      contract.Branch
	SpeedMPS    *float64
	TriggeredAt time.Time  // phone clock
	CreatedAt   time.Time  // server clock
	NotifiedAt  *time.Time // server clock
	Phone       string     // E.164; shown masked only
	UserName    string     // devices.user_name when the column exists (#104)
	// Cancelled is an explicit cancellation stored on the incident (a
	// cancelled_at column, when one exists). A no_duress verdict with no
	// family message reads as cancelled too (#106: the "todo bien" phrase).
	Cancelled bool
	Last      *Point // the newest point; nil without one
	// Call is the newest call (Status, RequestedAt, EndedAt, RingingAt,
	// PickedUpAt; no transcript); nil without one.
	Call          *Call
	Verdict       contract.Verdict // duress if any call says so, else the newest judged call's
	VerdictReason string
	SealedAt      *time.Time
	Deliveries    int // family messages recorded
	Delivered     int // of them, delivered or read
	LastAt        time.Time
}

// CaseExtras is what the central detail reads beyond the Record: columns
// other tickets may add, read so their absence is no error. (The calls'
// ficha, #106, is in the Record: Call.Context.)
type CaseExtras struct {
	Cancelled bool
}

// casesQuery is the central list: the newest incidents with their newest
// point and call, the headline verdict (duress first, as the notifier
// rules), the seal and the family message counts. user_name and
// cancelled_at go through to_jsonb so the query works before the tickets
// that add those columns land.
const casesQuery = `
SELECT i.id, i.trigger, i.branch, i.speed_mps, i.triggered_at, i.created_at, i.notified_at,
       d.phone_number, COALESCE(to_jsonb(d) ->> 'user_name', ''),
       (to_jsonb(i) ->> 'cancelled_at') IS NOT NULL,
       p.lat, p.lng, p.accuracy_m, p.speed_mps, p.recorded_at, p.received_at,
       c.status, c.requested_at, c.ended_at, cp.ringing_at, cp.answered_at,
       COALESCE(v.verdict, ''), COALESCE(v.verdict_reason, ''),
       s.sealed_at,
       COALESCE(nd.total, 0), COALESCE(nd.delivered, 0),
       GREATEST(i.triggered_at, i.created_at, i.notified_at, p.recorded_at, p.received_at, c.requested_at, c.ended_at)
FROM incidents i
JOIN devices d ON d.id = i.device_id
LEFT JOIN LATERAL (
	SELECT lat, lng, accuracy_m, speed_mps, recorded_at, received_at
	FROM gps_points WHERE incident_id = i.id ORDER BY recorded_at DESC, id DESC LIMIT 1) p ON true
LEFT JOIN LATERAL (
	SELECT id, status, requested_at, ended_at
	FROM calls WHERE incident_id = i.id ORDER BY requested_at DESC, id DESC LIMIT 1) c ON true
LEFT JOIN call_progress cp ON cp.call_id = c.id
LEFT JOIN LATERAL (
	SELECT verdict, verdict_reason
	FROM calls WHERE incident_id = i.id AND verdict IS NOT NULL
	ORDER BY verdict = 'duress' DESC, requested_at DESC, id DESC LIMIT 1) v ON true
LEFT JOIN incident_seals s ON s.incident_id = i.id
LEFT JOIN LATERAL (
	SELECT count(*) AS total, count(*) FILTER (WHERE status IN ('delivered', 'read')) AS delivered
	FROM notify_deliveries WHERE incident_id = i.id) nd ON true
ORDER BY i.created_at DESC, i.id DESC
LIMIT $1`

// Cases reads the limit newest incidents for the central list, newest
// first (by arrival at the server), in one query.
func (s DBStore) Cases(ctx context.Context, limit int) ([]CaseItem, error) {
	rows, err := s.pool.QueryContext(ctx, casesQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CaseItem
	for rows.Next() {
		var c CaseItem
		var trigger, branch string
		var speed sql.NullFloat64
		var notified, sealed sql.NullTime
		var lat, lng, accuracy, pSpeed sql.NullFloat64
		var recorded, received sql.NullTime
		var status sql.NullString
		var requested, ended, ringing, answered sql.NullTime
		var verdict string
		if err := rows.Scan(&c.ID, &trigger, &branch, &speed, &c.TriggeredAt, &c.CreatedAt, &notified,
			&c.Phone, &c.UserName, &c.Cancelled,
			&lat, &lng, &accuracy, &pSpeed, &recorded, &received,
			&status, &requested, &ended, &ringing, &answered,
			&verdict, &c.VerdictReason, &sealed, &c.Deliveries, &c.Delivered, &c.LastAt); err != nil {
			return nil, err
		}
		c.Trigger, c.Branch, c.Verdict = contract.Trigger(trigger), contract.Branch(branch), contract.Verdict(verdict)
		c.SpeedMPS, c.NotifiedAt, c.SealedAt = nullFloat(speed), nullTime(notified), nullTime(sealed)
		if lat.Valid && lng.Valid && recorded.Valid {
			c.Last = &Point{Lat: lat.Float64, Lng: lng.Float64, AccuracyM: nullFloat(accuracy), SpeedMPS: nullFloat(pSpeed),
				RecordedAt: recorded.Time, ReceivedAt: received.Time}
		}
		if status.Valid {
			c.Call = &Call{Status: status.String, RequestedAt: requested.Time, EndedAt: nullTime(ended),
				RingingAt: nullTime(ringing), PickedUpAt: nullTime(answered)}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Extras reads the incident's CaseExtras. ErrUnknownIncident when it is not
// stored.
func (s DBStore) Extras(ctx context.Context, incidentID string) (CaseExtras, error) {
	if !uuidRE.MatchString(incidentID) {
		return CaseExtras{}, ErrUnknownIncident
	}
	var x CaseExtras
	err := s.pool.QueryRowContext(ctx,
		`SELECT (to_jsonb(i) ->> 'cancelled_at') IS NOT NULL FROM incidents i WHERE i.id = $1`, incidentID).Scan(&x.Cancelled)
	if errors.Is(err, sql.ErrNoRows) {
		return CaseExtras{}, ErrUnknownIncident
	}
	return x, err
}

// caseItemOf sums up a full record the way DBStore.Cases does.
func caseItemOf(rec Record, x CaseExtras) CaseItem {
	inc := rec.Incident
	c := CaseItem{
		ID: inc.ID, Trigger: inc.Trigger, Branch: inc.Branch, SpeedMPS: inc.SpeedMPS,
		TriggeredAt: inc.TriggeredAt, CreatedAt: inc.CreatedAt, NotifiedAt: inc.NotifiedAt,
		Phone: rec.Device.PhoneNumber, UserName: rec.Device.UserName, Cancelled: x.Cancelled,
		Deliveries: len(rec.Deliveries),
	}
	if n := len(rec.Points); n > 0 {
		p := rec.Points[n-1]
		c.Last = &p
	}
	if n := len(rec.Calls); n > 0 {
		cl := rec.Calls[n-1]
		c.Call = &cl
	}
	for _, cl := range rec.Calls {
		if cl.Verdict == "" {
			continue
		}
		if c.Verdict != contract.VerdictDuress {
			c.Verdict, c.VerdictReason = cl.Verdict, cl.VerdictReason
		}
	}
	if rec.Seal != nil {
		t := rec.Seal.SealedAt
		c.SealedAt = &t
	}
	for _, d := range rec.Deliveries {
		if d.Status == "delivered" || d.Status == "read" {
			c.Delivered++
		}
	}
	_, c.LastAt = rec.span()
	return c
}
