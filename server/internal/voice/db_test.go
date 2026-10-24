package voice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// freshDevice migrates a throwaway database and registers dev with its key.
func freshDevice(t *testing.T, dev testDevice) *sql.DB {
	t.Helper()
	pool := dbtest.Fresh(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	keyID := sha256.Sum256(dev.pub)
	if _, err := pool.ExecContext(ctx, `
		INSERT INTO devices (id, phone_number, duress_phrase, ed25519_public_key, key_id)
		VALUES ($1, '+584141234567', 'dile a la tía Carmen que llego tarde', $2, $3)`,
		dev.id, []byte(dev.pub), keyID[:8]); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return pool
}

// nextCallVoice opens an incident for the device and returns the voice the
// call engine would place its call with: the device's, else the global one.
func nextCallVoice(t *testing.T, pool *sql.DB, deviceID string) string {
	t.Helper()
	ctx := context.Background()
	incident := fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1e12)
	if _, err := pool.ExecContext(ctx, `
		INSERT INTO incidents (id, device_id, trigger, branch, triggered_at) VALUES ($1, $2, 'widget', 'call', now())`,
		incident, deviceID); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	_, callee, err := call.NewDBStore(pool).Begin(ctx, incident)
	if err != nil {
		t.Fatal(err)
	}
	// What the engine sends: Client.Place puts the voice in the TTS override.
	var sent struct {
		ClientData struct {
			Override struct {
				TTS struct {
					VoiceID string `json:"voice_id"`
				} `json:"tts"`
			} `json:"conversation_config_override"`
		} `json:"conversation_initiation_client_data"`
	}
	el := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sent)
		_, _ = w.Write([]byte(`{"success":true,"conversation_id":"conv-1","callSid":"CA1"}`))
	}))
	defer el.Close()
	c := &call.Client{BaseURL: el.URL, APIKey: "k", AgentID: "a", PhoneNumberID: "p", VoiceID: "global-voice"}
	if _, err := c.Place(ctx, call.Call{To: callee.Phone, VoiceID: callee.VoiceID}); err != nil {
		t.Fatal(err)
	}
	return sent.ClientData.Override.TTS.VoiceID
}

func TestDBUploadNextCallUsesVoiceThenDeleteGoesGlobal(t *testing.T) {
	dev := newTestDevice(t, devID)
	pool := freshDevice(t, dev)
	el := newFakeElevenLabs(t)
	h := NewHandler(NewDBStore(pool), el.client())
	h.now = func() time.Time { return testNow }
	h.svc.now = h.now
	mux := http.NewServeMux()
	h.Register(mux)
	hs := &harness{t: t, el: el, h: h, mux: mux, dev: dev}

	if got := nextCallVoice(t, pool, devID); got != "global-voice" {
		t.Fatalf("before upload the call speaks with %q, want global-voice", got)
	}

	audio := fakeMP3("db-raw-audio-marker-107")
	wantStatus(t, hs.upload(audio), http.StatusCreated)

	var voice string
	var consentAt time.Time
	var sum []byte
	if err := pool.QueryRow(`SELECT voice_id, voice_consent_at, voice_consent_sha256 FROM devices WHERE id = $1`, devID).
		Scan(&voice, &consentAt, &sum); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(audio)
	if voice != "voice-1" || !consentAt.Equal(testNow) || !bytes.Equal(sum, want[:]) {
		t.Fatalf("devices row: voice=%q consent_at=%v sha=%x", voice, consentAt, sum)
	}
	if got := nextCallVoice(t, pool, devID); got != "voice-1" {
		t.Fatalf("after upload the call speaks with %q, want voice-1", got)
	}
	assertNoRawAudio(t, pool, audio, "db-raw-audio-marker-107")

	// Replacing deletes the old voice in ElevenLabs.
	testNow = testNow.Add(UploadInterval)
	defer func() { testNow = testNow.Add(-UploadInterval) }()
	wantStatus(t, hs.upload(fakeMP3("second")), http.StatusCreated)
	if got := nextCallVoice(t, pool, devID); got != "voice-2" {
		t.Fatalf("after the replacement the call speaks with %q, want voice-2", got)
	}
	if _, deleted, _ := el.snapshot(); len(deleted) != 1 || deleted[0] != "voice-1" {
		t.Fatalf("deleted %v, want [voice-1]", deleted)
	}

	wantStatus(t, hs.remove(), http.StatusNoContent)
	var cleared bool
	if err := pool.QueryRow(`SELECT voice_id IS NULL AND voice_consent_at IS NULL AND voice_consent_sha256 IS NULL FROM devices WHERE id = $1`, devID).
		Scan(&cleared); err != nil || !cleared {
		t.Fatalf("after DELETE the voice columns are not cleared (err %v)", err)
	}
	if got := nextCallVoice(t, pool, devID); got != "global-voice" {
		t.Fatalf("after DELETE the call speaks with %q, want global-voice", got)
	}
}

// assertNoRawAudio checks no bytea column holds the audio and no text-like
// column holds its marker, in every table of the schema.
func assertNoRawAudio(t *testing.T, pool *sql.DB, audio []byte, marker string) {
	t.Helper()
	rows, err := pool.Query(`
		SELECT table_name, column_name, data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND data_type IN ('bytea', 'text', 'jsonb', 'character varying', 'ARRAY')`)
	if err != nil {
		t.Fatal(err)
	}
	type col struct{ table, column, kind string }
	var cols []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.table, &c.column, &c.kind); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	rows.Close()
	if len(cols) == 0 {
		t.Fatal("found no columns to check")
	}
	for _, c := range cols {
		var q string
		var arg any
		if c.kind == "bytea" {
			q = fmt.Sprintf(`SELECT count(*) FROM %q WHERE position($1::bytea IN %q) > 0`, c.table, c.column)
			arg = audio[:64]
		} else {
			q = fmt.Sprintf(`SELECT count(*) FROM %q WHERE %q::text LIKE '%%' || $1 || '%%'`, c.table, c.column)
			arg = marker
		}
		var n int
		if err := pool.QueryRow(q, arg).Scan(&n); err != nil {
			t.Fatalf("%s.%s: %v", c.table, c.column, err)
		}
		if n != 0 {
			t.Fatalf("%s.%s holds the raw audio", c.table, c.column)
		}
	}
}
