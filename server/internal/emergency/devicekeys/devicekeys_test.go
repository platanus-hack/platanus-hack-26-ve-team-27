package devicekeys

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

const (
	vectorPub = "4238f4c4c81c39cb58ccf3f5fa499b8225c39e36ef32f83e0a6d6fa69876357a"
	deviceID  = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"
)

func vectorKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	raw, err := hex.DecodeString(vectorPub)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestKeyIDMatchesVector(t *testing.T) {
	pub := vectorKey(t)
	id := KeyID(pub)
	if got := hex.EncodeToString(id); got != "6e664c27c4356d73" {
		t.Fatalf("KeyID = %s, want 6e664c27c4356d73", got)
	}
	if got := binary.BigEndian.Uint64(id); got != payload.DeviceID(pub) {
		t.Fatalf("KeyID = %x, payload.DeviceID = %x", got, payload.DeviceID(pub))
	}
}

func migrated(t *testing.T) *sql.DB {
	t.Helper()
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestLookup(t *testing.T) {
	pool := migrated(t)
	pub := vectorKey(t)
	insert := func(id string, key, keyID any) {
		t.Helper()
		if _, err := pool.Exec(`INSERT INTO devices (id, phone_number, duress_phrase, ed25519_public_key, key_id) VALUES ($1, '+584141234567', 'p', $2, $3)`, id, key, keyID); err != nil {
			t.Fatal(err)
		}
	}
	insert(deviceID, []byte(pub), KeyID(pub))
	insert("no-key-device", nil, nil)

	got, err := Lookup(context.Background(), pool, 0x6e664c27c4356d73)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != deviceID || !got.PublicKey.Equal(pub) {
		t.Fatalf("Lookup = %+v", got)
	}
	if _, err := Lookup(context.Background(), pool, 0x0102030405060708); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown key_id: err = %v, want ErrNotFound", err)
	}
}

func TestMigrationRejectsHalfAKey(t *testing.T) {
	pool := migrated(t)
	pub := vectorKey(t)
	const q = `INSERT INTO devices (id, phone_number, duress_phrase, ed25519_public_key, key_id) VALUES ($1, '+584141234567', 'p', $2, $3)`
	if _, err := pool.Exec(q, "half", []byte(pub), nil); err == nil {
		t.Error("key without key_id was accepted")
	}
	if _, err := pool.Exec(q, "short", make([]byte, 31), KeyID(pub)); err == nil {
		t.Error("31-byte key was accepted")
	}
}
