package report

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// ErrNotFound means no live report: the token is unknown, malformed or
// expired. The page answers 404 for all three alike.
var ErrNotFound = errors.New("report: not found")

// ErrUnknownIncident means the incident is not stored.
var ErrUnknownIncident = errors.New("report: unknown incident")

// uuidRE matches an incident_id; incidents.id is a UUID column.
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Link is an incident's report link.
type Link struct {
	Token     string
	ExpiresAt time.Time
}

// URL is the page's absolute URL on base (the server's public URL; blank
// gives the bare path).
func (l Link) URL(base string) string { return PageURL(base, l.Token) }

// DBStore is the Postgres store of reports, recordings and seals.
type DBStore struct {
	pool *sql.DB
}

// NewDBStore returns the store on pool. pool must not be nil.
func NewDBStore(pool *sql.DB) DBStore { return DBStore{pool: pool} }

// Ensure returns the incident's live link, creating one valid for ttlDays
// when there is none or the last one expired (a fresh token then replaces
// it). Concurrent calls get the same link. ErrUnknownIncident when the
// incident is not stored.
func (s DBStore) Ensure(ctx context.Context, incidentID string, ttlDays int) (Link, error) {
	if !uuidRE.MatchString(incidentID) {
		return Link{}, ErrUnknownIncident
	}
	if ttlDays <= 0 {
		return Link{}, fmt.Errorf("report: ttlDays must be positive, got %d", ttlDays)
	}
	token, err := NewToken(nil)
	if err != nil {
		return Link{}, err
	}
	var l Link
	err = s.pool.QueryRowContext(ctx, `
		INSERT INTO incident_reports (incident_id, token, expires_at)
		SELECT id, $2, now() + make_interval(days => $3) FROM incidents WHERE id = $1
		ON CONFLICT (incident_id) DO UPDATE SET
			token      = EXCLUDED.token,
			created_at = now(),
			expires_at = EXCLUDED.expires_at
		WHERE incident_reports.expires_at <= now()
		RETURNING token, expires_at`,
		incidentID, token, ttlDays).Scan(&l.Token, &l.ExpiresAt)
	if err == nil {
		return l, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Link{}, err
	}
	// Nothing inserted: a live link exists, or the incident does not.
	err = s.pool.QueryRowContext(ctx,
		`SELECT token, expires_at FROM incident_reports WHERE incident_id = $1`, incidentID).Scan(&l.Token, &l.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrUnknownIncident
	}
	return l, err
}

// incidentByToken returns the incident behind a live token.
func (s DBStore) incidentByToken(ctx context.Context, token string) (string, error) {
	if !ValidToken(token) {
		return "", ErrNotFound
	}
	var id string
	err := s.pool.QueryRowContext(ctx,
		`SELECT incident_id FROM incident_reports WHERE token = $1 AND expires_at > now()`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// ByToken loads the record behind a live token; ErrNotFound for an unknown,
// malformed or expired one.
func (s DBStore) ByToken(ctx context.Context, token string) (Record, error) {
	id, err := s.incidentByToken(ctx, token)
	if err != nil {
		return Record{}, err
	}
	rec, err := s.Load(ctx, id)
	if errors.Is(err, ErrUnknownIncident) {
		return Record{}, ErrNotFound
	}
	return rec, err
}

// Load reads the incident's record in one read-only snapshot, so a seal
// never mixes two states. ErrUnknownIncident when it is not stored.
func (s DBStore) Load(ctx context.Context, incidentID string) (Record, error) {
	if !uuidRE.MatchString(incidentID) {
		return Record{}, ErrUnknownIncident
	}
	tx, err := s.pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()

	var rec Record
	inc := &rec.Incident
	var speed sql.NullFloat64
	var notified sql.NullTime
	var trigger, branch string
	var deviceState []byte
	err = tx.QueryRowContext(ctx, `
		SELECT i.id, i.device_id, i.trigger, i.branch, i.speed_mps, i.triggered_at, i.created_at, i.notified_at,
		       d.phone_number, cardinality(d.family_numbers), d.duress_phrase,
		       COALESCE(to_jsonb(d) ->> 'user_name', ''), i.device_state
		FROM incidents i JOIN devices d ON d.id = i.device_id
		WHERE i.id = $1`, incidentID).
		Scan(&inc.ID, &inc.DeviceID, &trigger, &branch, &speed, &inc.TriggeredAt, &inc.CreatedAt, &notified,
			&rec.Device.PhoneNumber, &rec.Device.FamilyNumbers, &rec.Device.DuressPhrase, &rec.Device.UserName, &deviceState)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrUnknownIncident
	}
	if err != nil {
		return Record{}, err
	}
	inc.Trigger, inc.Branch = contract.Trigger(trigger), contract.Branch(branch)
	inc.SpeedMPS = nullFloat(speed)
	inc.NotifiedAt = nullTime(notified)
	rec.DeviceState = parseDeviceState(deviceState)

	if rec.Points, err = loadPoints(ctx, tx, incidentID); err != nil {
		return Record{}, err
	}
	if rec.Calls, err = loadCalls(ctx, tx, incidentID); err != nil {
		return Record{}, err
	}
	if err = loadLiveTurns(ctx, tx, incidentID, rec.Calls); err != nil {
		return Record{}, err
	}
	if rec.Seal, err = loadSeal(ctx, tx, incidentID); err != nil {
		return Record{}, err
	}
	if rec.Deliveries, err = loadDeliveries(ctx, tx, incidentID); err != nil {
		return Record{}, err
	}
	from, to := rec.span()
	if rec.Emergencies, err = loadEmergencies(ctx, tx, inc.DeviceID, from.Add(-emergencySlack), to.Add(emergencySlack)); err != nil {
		return Record{}, err
	}
	if rec.PermissionEvents, err = loadPermissionEvents(ctx, tx, inc.DeviceID); err != nil {
		return Record{}, err
	}
	return rec, tx.Commit()
}

// parseDeviceState decodes incidents.device_state; nil when NULL or empty.
// The intake stored it from contract.DeviceState, whose decoding never fails.
func parseDeviceState(raw []byte) *contract.DeviceState {
	if len(raw) == 0 {
		return nil
	}
	var state contract.DeviceState
	if err := json.Unmarshal(raw, &state); err != nil || len(state.Permissions) == 0 {
		return nil
	}
	return &state
}

// maxPermissionEvents bounds the permission events a record loads. The
// phone sends a handful per device (one per permission at setup, then
// changes), but the route has no auth.
const maxPermissionEvents = 200

// loadPermissionEvents reads the device's newest maxPermissionEvents
// permission events, oldest first; nil when it has none.
func loadPermissionEvents(ctx context.Context, tx *sql.Tx, deviceID string) ([]PermissionEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT permission, granted, at, received_at FROM (
			SELECT permission, granted, at, received_at FROM device_permission_events
			WHERE device_id = $1 ORDER BY at DESC, permission DESC LIMIT $2
		) newest ORDER BY at, permission`, deviceID, maxPermissionEvents)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PermissionEvent
	for rows.Next() {
		var e PermissionEvent
		if err := rows.Scan(&e.Permission, &e.Granted, &e.At, &e.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// emergencySlack widens the incident's time span when linking offline
// emergencies: the phone may text before the alert reaches the server, or
// keep sending after the call.
const emergencySlack = 10 * time.Minute

// span is the first and last time stored about the incident.
func (rec Record) span() (from, to time.Time) {
	from, to = rec.Incident.TriggeredAt, rec.Incident.CreatedAt
	see := func(t time.Time) {
		if t.Before(from) {
			from = t
		}
		if t.After(to) {
			to = t
		}
	}
	see(rec.Incident.CreatedAt)
	if rec.Incident.NotifiedAt != nil {
		see(*rec.Incident.NotifiedAt)
	}
	for _, p := range rec.Points {
		see(p.RecordedAt)
		see(p.ReceivedAt)
	}
	for _, c := range rec.Calls {
		see(c.RequestedAt)
		if c.EndedAt != nil {
			see(*c.EndedAt)
		}
	}
	return from, to
}

// loadEmergencies reads the device's offline emergencies whose span
// [first_received_at, updated_at] overlaps [from, to].
func loadEmergencies(ctx context.Context, tx *sql.Tx, deviceID string, from, to time.Time) ([]Emergency, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT emergency_id, status, seq, channels, min_hops, lat_e5, lon_e5,
		       sent_at, first_received_at, updated_at, cancelled_at
		FROM emergencies
		WHERE device_id = $1 AND first_received_at <= $3 AND updated_at >= $2
		ORDER BY first_received_at, emergency_id`, deviceID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Emergency
	for rows.Next() {
		var e Emergency
		var channels string
		var hops, lat, lon sql.NullInt64
		var cancelled sql.NullTime
		if err := rows.Scan(&e.ID, &e.Status, &e.Seq, &channels, &hops, &lat, &lon,
			&e.SentAt, &e.FirstReceivedAt, &e.UpdatedAt, &cancelled); err != nil {
			return nil, err
		}
		e.Channels = pgTextArray(channels)
		if hops.Valid {
			h := int(hops.Int64)
			e.MinHops = &h
		}
		if lat.Valid && lon.Valid {
			la, lo := float64(lat.Int64)/1e5, float64(lon.Int64)/1e5
			e.Lat, e.Lng = &la, &lo
		}
		e.CancelledAt = nullTime(cancelled)
		out = append(out, e)
	}
	return out, rows.Err()
}

// pgTextArray parses a Postgres text[] of plain words, "{http,sms}".
func pgTextArray(s string) []string {
	s = strings.Trim(s, "{}")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// SaveProgress records a Twilio status the call engine saw for callID at
// at: the first ringing (or later) status stamps ringing_at, the first
// in-progress answered_at, and the last status is kept.
func (s DBStore) SaveProgress(ctx context.Context, callID int64, twilioStatus string, at time.Time) error {
	_, err := s.pool.ExecContext(ctx, `
		INSERT INTO call_progress (call_id, twilio_status, ringing_at, answered_at, updated_at)
		SELECT id, $2,
		       CASE WHEN $2 IN ('ringing', 'in-progress', 'completed') THEN $3::timestamptz END,
		       CASE WHEN $2 = 'in-progress' THEN $3::timestamptz END,
		       $3
		FROM calls WHERE id = $1
		ON CONFLICT (call_id) DO UPDATE SET
			twilio_status = EXCLUDED.twilio_status,
			ringing_at    = COALESCE(call_progress.ringing_at, EXCLUDED.ringing_at),
			answered_at   = COALESCE(call_progress.answered_at, EXCLUDED.answered_at),
			updated_at    = EXCLUDED.updated_at`,
		callID, twilioStatus, at)
	return err
}

// SaveLiveTurn stores one final phrase Twilio's real-time transcription
// heard on the call with callSID (#143). It reports whether it stored it: a
// repeated callback (same track and SequenceId) and an unknown callSID store
// nothing.
func (s DBStore) SaveLiveTurn(ctx context.Context, callSID string, t LiveTurn) (bool, error) {
	res, err := s.pool.ExecContext(ctx, `
		INSERT INTO call_live_turns (call_id, track, sequence_id, message, said_at)
		SELECT id, $2, $3, $4, $5 FROM calls WHERE call_sid = $1 ORDER BY id DESC LIMIT 1
		ON CONFLICT (call_id, track, sequence_id) DO NOTHING`,
		callSID, t.Track, t.SequenceID, t.Message, t.SaidAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// NewestIncident is the most recently received incident; ErrUnknownIncident
// when there is none.
func (s DBStore) NewestIncident(ctx context.Context, after time.Time) (string, error) {
	var id string
	err := s.pool.QueryRowContext(ctx,
		`SELECT id FROM incidents WHERE created_at > $1 ORDER BY created_at DESC, id LIMIT 1`, after).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnknownIncident
	}
	return id, err
}

func loadPoints(ctx context.Context, tx *sql.Tx, incidentID string) ([]Point, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT lat, lng, accuracy_m, speed_mps, recorded_at, received_at
		FROM gps_points WHERE incident_id = $1 ORDER BY recorded_at, id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var accuracy, speed sql.NullFloat64
		if err := rows.Scan(&p.Lat, &p.Lng, &accuracy, &speed, &p.RecordedAt, &p.ReceivedAt); err != nil {
			return nil, err
		}
		p.AccuracyM, p.SpeedMPS = nullFloat(accuracy), nullFloat(speed)
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadCalls(ctx context.Context, tx *sql.Tx, incidentID string) ([]Call, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, COALESCE(c.conversation_id, ''), c.status, c.transcript, c.duration_secs,
		       c.requested_at, c.ended_at, COALESCE(c.verdict, ''), COALESCE(c.verdict_reason, ''), c.verdict_at,
		       r.content_type, r.sha256, octet_length(r.audio), r.stored_at,
		       COALESCE(p.twilio_status, ''), p.ringing_at, p.answered_at,
		       to_jsonb(c) -> 'ficha'
		FROM calls c
		LEFT JOIN call_recordings r ON r.call_id = c.id
		LEFT JOIN call_progress p ON p.call_id = c.id
		WHERE c.incident_id = $1 ORDER BY c.requested_at, c.id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		var c Call
		var transcript []byte
		var duration sql.NullInt64
		var ended, verdictAt, storedAt sql.NullTime
		var verdict string
		var contentType sql.NullString
		var sum []byte
		var size sql.NullInt64
		var ringing, answered sql.NullTime
		var ficha []byte // calls.ficha (#106) through to_jsonb: NULL until that column exists
		if err := rows.Scan(&c.ID, &c.ConversationID, &c.Status, &transcript, &duration,
			&c.RequestedAt, &ended, &verdict, &c.VerdictReason, &verdictAt,
			&contentType, &sum, &size, &storedAt,
			&c.TwilioStatus, &ringing, &answered, &ficha); err != nil {
			return nil, err
		}
		c.Context = parseCallContext(ficha)
		c.RingingAt, c.PickedUpAt = nullTime(ringing), nullTime(answered)
		if len(transcript) > 0 {
			if err := json.Unmarshal(transcript, &c.Transcript); err != nil {
				return nil, fmt.Errorf("report: call %d transcript: %w", c.ID, err)
			}
		}
		if duration.Valid {
			d := int(duration.Int64)
			c.DurationSecs = &d
		}
		c.EndedAt, c.VerdictAt = nullTime(ended), nullTime(verdictAt)
		c.Verdict = contract.Verdict(verdict)
		if contentType.Valid && len(sum) == 32 {
			r := &Recording{ContentType: contentType.String, Size: size.Int64, StoredAt: storedAt.Time}
			copy(r.SHA256[:], sum)
			c.Recording = r
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// loadLiveTurns fills LiveTurns (#143) of the calls with no post-call
// transcript yet, ordered by Twilio's Timestamp across both tracks
// (SequenceId orders one track only), counted from when Twilio said the call
// was answered.
func loadLiveTurns(ctx context.Context, tx *sql.Tx, incidentID string, calls []Call) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT t.call_id, t.track, t.message, t.said_at
		FROM call_live_turns t JOIN calls c ON c.id = t.call_id
		WHERE c.incident_id = $1 AND c.transcript IS NULL
		ORDER BY t.call_id, t.said_at, t.track, t.sequence_id`, incidentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := make(map[int64]*Call, len(calls))
	for i := range calls {
		byID[calls[i].ID] = &calls[i]
	}
	for rows.Next() {
		var id int64
		var t LiveTurn
		if err := rows.Scan(&id, &t.Track, &t.Message, &t.SaidAt); err != nil {
			return err
		}
		if c := byID[id]; c != nil {
			c.LiveTurns = append(c.LiveTurns, t.transcriptTurn(c.liveStart()))
		}
	}
	return rows.Err()
}

func loadSeal(ctx context.Context, tx *sql.Tx, incidentID string) (*Seal, error) {
	var s Seal
	var pub []byte
	err := tx.QueryRowContext(ctx,
		`SELECT sealed_at, document, signature, public_key FROM incident_seals WHERE incident_id = $1`, incidentID).
		Scan(&s.SealedAt, &s.Document, &s.Signature, &pub)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.PublicKey = ed25519.PublicKey(pub)
	return &s, nil
}

func nullFloat(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// ErrUnknownCall means no call has the conversation_id.
var ErrUnknownCall = errors.New("report: unknown call")

// HasRecording reports whether the call with conversationID has its
// recording stored.
func (s DBStore) HasRecording(ctx context.Context, conversationID string) (bool, error) {
	var has bool
	err := s.pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM call_recordings r JOIN calls c ON c.id = r.call_id WHERE c.conversation_id = $1)`,
		conversationID).Scan(&has)
	return has, err
}

// SaveRecording stores the recording of the call with conversationID and
// its SHA-256, once: a recording already stored is kept as it is.
// ErrUnknownCall when no call has conversationID.
func (s DBStore) SaveRecording(ctx context.Context, conversationID, contentType string, audio []byte) error {
	sum := sha256.Sum256(audio)
	res, err := s.pool.ExecContext(ctx, `
		INSERT INTO call_recordings (call_id, content_type, audio, sha256)
		SELECT id, $2, $3, $4 FROM calls WHERE conversation_id = $1
		ON CONFLICT (call_id) DO NOTHING`,
		conversationID, contentType, audio, sum[:])
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	if has, err := s.HasRecording(ctx, conversationID); err != nil || has {
		return err
	}
	return ErrUnknownCall
}

// Audio returns the recording of the n-th call (1-based, in the order the
// calls were requested) of the incident behind a live token; n = 0 picks
// the newest call that has one. ErrNotFound when there is none.
func (s DBStore) Audio(ctx context.Context, token string, n int) ([]byte, Recording, error) {
	id, err := s.incidentByToken(ctx, token)
	if err != nil {
		return nil, Recording{}, err
	}
	var audio, sum []byte
	var r Recording
	err = s.pool.QueryRowContext(ctx, `
		WITH c AS (
			SELECT id, row_number() OVER (ORDER BY requested_at, id) AS n
			FROM calls WHERE incident_id = $1
		)
		SELECT r.audio, r.content_type, r.sha256, r.stored_at
		FROM c JOIN call_recordings r ON r.call_id = c.id
		WHERE $2 = 0 OR c.n = $2
		ORDER BY c.n DESC LIMIT 1`, id, n).Scan(&audio, &r.ContentType, &sum, &r.StoredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, Recording{}, ErrNotFound
	}
	if err != nil {
		return nil, Recording{}, err
	}
	copy(r.SHA256[:], sum)
	r.Size = int64(len(audio))
	return audio, r, nil
}

// SaveSeal stores the incident's seal unless it already has one, and
// reports whether this one was stored.
func (s DBStore) SaveSeal(ctx context.Context, incidentID string, seal Seal) (bool, error) {
	res, err := s.pool.ExecContext(ctx, `
		INSERT INTO incident_seals (incident_id, sealed_at, document, signature, public_key)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (incident_id) DO NOTHING`,
		incidentID, seal.SealedAt, seal.Document, seal.Signature, []byte(seal.PublicKey))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Links gives the family message its report link (notify.ReportLinks).
type Links struct {
	Store   DBStore
	BaseURL string // the server's public URL (PUBLIC_URL); blank sends no link
	TTLDays int    // how long a new link lives
}

// ReportURL ensures the incident has a live link and returns its absolute
// URL. Without a public URL it returns "": a bare path is no use in a
// WhatsApp message.
func (l Links) ReportURL(ctx context.Context, incidentID string) (string, error) {
	if l.BaseURL == "" {
		return "", nil
	}
	link, err := l.Store.Ensure(ctx, incidentID, l.TTLDays)
	if err != nil {
		return "", err
	}
	return link.URL(l.BaseURL), nil
}

// Now is the database clock.
func (s DBStore) Now(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := s.pool.QueryRowContext(ctx, `SELECT now()`).Scan(&t)
	return t, err
}

func loadDeliveries(ctx context.Context, tx *sql.Tx, incidentID string) ([]Delivery, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT recipient, channel, COALESCE(message_id, ''), status, COALESCE(error, ''), sent_at, delivered_at, read_at
		FROM notify_deliveries WHERE incident_id = $1 ORDER BY id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var delivered, read sql.NullTime
		if err := rows.Scan(&d.Recipient, &d.Channel, &d.MessageID, &d.Status, &d.Error, &d.SentAt, &delivered, &read); err != nil {
			return nil, err
		}
		d.DeliveredAt, d.ReadAt = nullTime(delivered), nullTime(read)
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveDelivery records one family message the notifier sent: accepted with
// the provider's message id, or failed with the error.
func (s DBStore) SaveDelivery(ctx context.Context, incidentID, recipient, channel, messageID, status, errText string) error {
	_, err := s.pool.ExecContext(ctx, `
		INSERT INTO notify_deliveries (incident_id, recipient, channel, message_id, status, error)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''))
		ON CONFLICT (message_id) DO NOTHING`,
		incidentID, recipient, channel, messageID, status, errText)
	return err
}

// UpdateDelivery applies a provider status to the message with messageID,
// only forward (accepted < sent < delivered < read; failed unless read), so
// a late or repeated webhook never moves it back. It reports whether a row
// changed; an unknown id is no error.
func (s DBStore) UpdateDelivery(ctx context.Context, messageID, status string, at time.Time, errText string) (bool, error) {
	res, err := s.pool.ExecContext(ctx, `
		UPDATE notify_deliveries SET
			status       = $2,
			delivered_at = CASE WHEN $2 IN ('delivered', 'read') THEN COALESCE(delivered_at, $3) ELSE delivered_at END,
			read_at      = CASE WHEN $2 = 'read' THEN COALESCE(read_at, $3) ELSE read_at END,
			error        = CASE WHEN $2 = 'failed' THEN NULLIF($4, '') ELSE error END,
			updated_at   = $3
		WHERE message_id = $1 AND (
			($2 = 'failed' AND status NOT IN ('failed', 'read')) OR
			(CASE $2 WHEN 'sent' THEN 1 WHEN 'delivered' THEN 2 WHEN 'read' THEN 3 ELSE -1 END) >
			(CASE status WHEN 'accepted' THEN 0 WHEN 'sent' THEN 1 WHEN 'delivered' THEN 2 WHEN 'read' THEN 3 ELSE 99 END))`,
		messageID, status, at, errText)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeliveryLog records the notifier's sends (notify.DeliveryLog) for the
// report, with the number masked.
type DeliveryLog struct {
	Store   DBStore
	Channel string // whatsapp or sms, when the sender does not say
}

// Sent records one send; a store failure is only logged, never the alert's.
func (l DeliveryLog) Sent(ctx context.Context, incidentID, to, channel, messageID string, sendErr error) {
	if channel == "" {
		channel = l.Channel
	}
	status, errText := "accepted", ""
	if sendErr != nil {
		status, errText = "failed", sendErr.Error()
		if len(errText) > 300 {
			errText = errText[:300]
		}
	}
	if err := l.Store.SaveDelivery(ctx, incidentID, MaskRecipient(to), channel, messageID, status, errText); err != nil {
		log.Printf("report: incident %s: record delivery to %s: %v", incidentID, MaskRecipient(to), err)
	}
}

// MaskRecipient shows a family number the way the report does: the country
// and area codes and the last two digits of a +58 number, "+58 424 ···33";
// only the last two of any other.
func MaskRecipient(phone string) string {
	if len(phone) < 4 {
		return "···"
	}
	last := phone[len(phone)-2:]
	if strings.HasPrefix(phone, "+58") && len(phone) >= 9 {
		return "+58 " + phone[3:6] + " ···" + last
	}
	return "···" + last
}

// DeliveryIncident is the incident of the family message with messageID,
// for the trace of a Kapso status; "" when no row has it.
func (s DBStore) DeliveryIncident(ctx context.Context, messageID string) (string, error) {
	var id string
	err := s.pool.QueryRowContext(ctx, `SELECT incident_id FROM notify_deliveries WHERE message_id = $1`, messageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
