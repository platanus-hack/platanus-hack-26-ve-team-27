package call

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// calls.status values (server/migrations/0001_init.sql).
const (
	StatusRequested  = "requested"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// uuidRE matches an incident_id; calls.incident_id is a UUID column.
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ErrUnknownIncident means the incident is not stored, or a webhook names no
// incident and matches no call: there is no row to write the call to.
var ErrUnknownIncident = errors.New("call: unknown incident")

// Store keeps the calls rows. DBStore is the real one.
type Store interface {
	// Begin inserts a requested call for the incident and returns its id and
	// the device it goes to, before the call is placed. A cancelled incident
	// is ErrUnknownIncident, like one that is not stored: no call is made.
	Begin(ctx context.Context, incidentID string) (callID int64, to Callee, err error)
	// Placed stores the ids ElevenLabs answered with and moves a requested
	// call to in_progress.
	Placed(ctx context.Context, callID int64, p Placed) error
	// Failed marks the call failed, unless a webhook already finished it.
	Failed(ctx context.Context, callID int64) error
	// FailedConversation marks the call with conversationID failed, unless
	// a webhook already finished it. When no row has conversationID yet
	// (the failure beat Placed), it applies the SaveTranscript fallback: the
	// newest row of incidentID with no conversation_id. Finding no row is no
	// error.
	FailedConversation(ctx context.Context, conversationID, incidentID string) error
	// SaveTranscript stores a post-call webhook (docs/contract.md
	// "CallWebhook"). A repeated webhook updates the same row.
	SaveTranscript(ctx context.Context, hook contract.CallWebhook) error
	// ActiveCallSIDs returns the Twilio CallSIDs of the incident's calls that
	// were placed and not finished (requested or in_progress), for a hang-up.
	ActiveCallSIDs(ctx context.Context, incidentID string) ([]string, error)
}

// DBStore is the Postgres Store.
type DBStore struct {
	pool *sql.DB
}

var _ Store = DBStore{}

// NewDBStore returns the Store on pool. pool must not be nil.
func NewDBStore(pool *sql.DB) DBStore { return DBStore{pool: pool} }

func (s DBStore) Begin(ctx context.Context, incidentID string) (int64, Callee, error) {
	var id int64
	var to Callee
	err := s.pool.QueryRowContext(ctx, `
		WITH c AS (
			INSERT INTO calls (incident_id) SELECT id FROM incidents WHERE id = $1 AND cancelled_at IS NULL RETURNING id, incident_id
		)
		SELECT c.id, d.phone_number, COALESCE(d.user_name, ''), d.duress_phrase,
			COALESCE(d.safe_phrase, ''), COALESCE(d.voice_id, '')
		FROM c JOIN incidents i ON i.id = c.incident_id JOIN devices d ON d.id = i.device_id`,
		incidentID).Scan(&id, &to.Phone, &to.UserName, &to.DuressPhrase, &to.SafePhrase, &to.VoiceID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, Callee{}, ErrUnknownIncident
	}
	return id, to, err
}

// ActiveCallSIDs lists the calls of the incident a hang-up can still reach.
func (s DBStore) ActiveCallSIDs(ctx context.Context, incidentID string) ([]string, error) {
	rows, err := s.pool.QueryContext(ctx, `
		SELECT call_sid FROM calls
		WHERE incident_id = $1 AND call_sid IS NOT NULL AND status IN ('requested', 'in_progress')
		ORDER BY id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sids []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		sids = append(sids, sid)
	}
	return sids, rows.Err()
}

func (s DBStore) Placed(ctx context.Context, callID int64, p Placed) error {
	_, err := s.pool.ExecContext(ctx, `
		UPDATE calls SET
			conversation_id = NULLIF($2, ''),
			call_sid        = NULLIF($3, ''),
			status          = CASE WHEN status = 'requested' THEN 'in_progress' ELSE status END
		WHERE id = $1`,
		callID, p.ConversationID, p.CallSID)
	return err
}

func (s DBStore) Failed(ctx context.Context, callID int64) error {
	_, err := s.pool.ExecContext(ctx,
		`UPDATE calls SET status = 'failed', ended_at = now() WHERE id = $1 AND status IN ('requested', 'in_progress')`, callID)
	return err
}

func (s DBStore) FailedConversation(ctx context.Context, conversationID, incidentID string) error {
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM calls WHERE conversation_id = $1)`, conversationID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		_, err = tx.ExecContext(ctx,
			`UPDATE calls SET status = 'failed', ended_at = now() WHERE conversation_id = $1 AND status IN ('requested', 'in_progress')`,
			conversationID)
	} else if uuidRE.MatchString(incidentID) {
		// Placed later keeps the failed status and only adds the ids.
		_, err = tx.ExecContext(ctx, `
			UPDATE calls SET conversation_id = $1, status = 'failed', ended_at = now()
			WHERE id = (
				SELECT id FROM calls WHERE incident_id = $2 AND conversation_id IS NULL
				ORDER BY requested_at DESC, id DESC LIMIT 1 FOR UPDATE
			)`,
			conversationID, incidentID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SaveTranscript follows the contract's matching rule, in one transaction:
// update the row with the conversation_id; else the newest row of the
// incident whose conversation_id is still NULL (the webhook beat Placed);
// only if there is none, insert. The insert upserts on conversation_id, so
// two concurrent copies of the same webhook still leave one row.
func (s DBStore) SaveTranscript(ctx context.Context, hook contract.CallWebhook) error {
	d := hook.Data
	if d.ConversationID == "" {
		// Nothing to dedupe on: a second one would collide on the unique key.
		return ErrUnknownIncident
	}
	transcript := d.Transcript
	if transcript == nil {
		transcript = []contract.TranscriptTurn{}
	}
	turns, err := json.Marshal(transcript)
	if err != nil {
		return err
	}
	status := webhookStatus(d.Status)
	ended := endedAt(d.Metadata)
	duration := d.Metadata.CallDurationSecs

	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	n, err := affected(tx.ExecContext(ctx, `
		UPDATE calls SET status = $2, transcript = $3, duration_secs = $4, ended_at = $5
		WHERE conversation_id = $1`,
		d.ConversationID, status, turns, duration, ended))
	if err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}

	incident := hook.IncidentID()
	if !uuidRE.MatchString(incident) {
		return ErrUnknownIncident
	}
	n, err = affected(tx.ExecContext(ctx, `
		UPDATE calls SET conversation_id = $1, status = $2, transcript = $3, duration_secs = $4, ended_at = $5
		WHERE id = (
			SELECT id FROM calls WHERE incident_id = $6 AND conversation_id IS NULL
			ORDER BY requested_at DESC, id DESC LIMIT 1 FOR UPDATE
		)`,
		d.ConversationID, status, turns, duration, ended, incident))
	if err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}

	n, err = affected(tx.ExecContext(ctx, `
		INSERT INTO calls (incident_id, conversation_id, status, transcript, duration_secs, ended_at)
		SELECT id, $1, $2, $3, $4, $5 FROM incidents WHERE id = $6
		ON CONFLICT (conversation_id) DO UPDATE SET
			status        = EXCLUDED.status,
			transcript    = EXCLUDED.transcript,
			duration_secs = EXCLUDED.duration_secs,
			ended_at      = EXCLUDED.ended_at`,
		d.ConversationID, status, turns, duration, ended, incident))
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUnknownIncident
	}
	return tx.Commit()
}

// affected returns how many rows an Exec changed.
func affected(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// webhookStatus maps the ElevenLabs conversation status to calls.status. The
// post-call webhook comes after the call ended, so anything but "failed" is
// done.
func webhookStatus(s string) string {
	if s == "failed" {
		return StatusFailed
	}
	return StatusDone
}

// endedAt is when the call ended, from the webhook metadata; nil (NULL) when
// ElevenLabs sent no start time.
func endedAt(m contract.CallMetadata) *time.Time {
	if m.StartTimeUnixSecs <= 0 {
		return nil
	}
	t := time.Unix(m.StartTimeUnixSecs+int64(m.CallDurationSecs), 0).UTC()
	return &t
}
