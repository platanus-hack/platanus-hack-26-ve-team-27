package voice

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrUnknownDevice means no devices row has that id.
var ErrUnknownDevice = errors.New("voice: unknown device")

// Device is what the voice routes read from a devices row.
type Device struct {
	PublicKey []byte // ed25519_public_key; nil while the device has no bound key
	VoiceID   string // "" uses the global voice
}

// Store reads and writes the voice columns of devices.
type Store interface {
	Device(ctx context.Context, deviceID string) (Device, error)
	// SetVoice stores the new voice and its consent and returns the voice it
	// replaced ("" if none).
	SetVoice(ctx context.Context, deviceID, voiceID string, consentAt time.Time, sha256 []byte) (old string, err error)
	// ClearVoice empties the voice columns, but only while voice_id is still
	// voiceID, so a voice set meanwhile is kept.
	ClearVoice(ctx context.Context, deviceID, voiceID string) error
}

// DBStore is the Store on Postgres.
type DBStore struct{ pool *sql.DB }

var _ Store = DBStore{}

// NewDBStore returns the Store on pool. pool must not be nil.
func NewDBStore(pool *sql.DB) DBStore { return DBStore{pool: pool} }

func (s DBStore) Device(ctx context.Context, deviceID string) (Device, error) {
	var d Device
	var voice sql.NullString
	err := s.pool.QueryRowContext(ctx,
		`SELECT ed25519_public_key, voice_id FROM devices WHERE id = $1`, deviceID).Scan(&d.PublicKey, &voice)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrUnknownDevice
	}
	d.VoiceID = voice.String
	return d, err
}

func (s DBStore) SetVoice(ctx context.Context, deviceID, voiceID string, consentAt time.Time, sha256 []byte) (string, error) {
	// The subquery locks the row and reads the voice being replaced, since
	// RETURNING sees only the new values.
	var old sql.NullString
	err := s.pool.QueryRowContext(ctx, `
		UPDATE devices d SET voice_id = $2, voice_consent_at = $3, voice_consent_sha256 = $4
		FROM (SELECT id, voice_id FROM devices WHERE id = $1 FOR UPDATE) prev
		WHERE d.id = prev.id
		RETURNING prev.voice_id`, deviceID, voiceID, consentAt, sha256).Scan(&old)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnknownDevice
	}
	return old.String, err
}

func (s DBStore) ClearVoice(ctx context.Context, deviceID, voiceID string) error {
	_, err := s.pool.ExecContext(ctx, `
		UPDATE devices SET voice_id = NULL, voice_consent_at = NULL, voice_consent_sha256 = NULL
		WHERE id = $1 AND voice_id = $2`, deviceID, voiceID)
	return err
}
