// Package devicekeys maps the offline payload's 8-byte device_id to the
// registered device (#55); #57 verifies signatures with it.
package devicekeys

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/binary"
	"errors"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

// ErrNotFound means no registered device has the key_id.
var ErrNotFound = errors.New("devicekeys: no device has this key_id")

// Device is a registered device and its Ed25519 public key.
type Device struct {
	ID        string
	PublicKey ed25519.PublicKey
}

// QueryRower is the part of *sql.DB and *sql.Tx that Lookup needs.
type QueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// KeyID is the devices.key_id value for pub: the 8 bytes of
// payload.DeviceID(pub), big-endian.
func KeyID(pub ed25519.PublicKey) []byte {
	id := make([]byte, 8)
	binary.BigEndian.PutUint64(id, payload.DeviceID(pub))
	return id
}

// Lookup returns the device registered with keyID (a payload's device_id),
// or ErrNotFound.
func Lookup(ctx context.Context, db QueryRower, keyID uint64) (Device, error) {
	id := make([]byte, 8)
	binary.BigEndian.PutUint64(id, keyID)
	var d Device
	var pub []byte
	err := db.QueryRowContext(ctx, `SELECT id, ed25519_public_key FROM devices WHERE key_id = $1`, id).Scan(&d.ID, &pub)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	d.PublicKey = ed25519.PublicKey(pub)
	return d, nil
}
