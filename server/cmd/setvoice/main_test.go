package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

const device = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"

// fakeElevenLabs answers POST /v1/voices/add and DELETE /v1/voices/{id}.
type fakeElevenLabs struct {
	mu      sync.Mutex
	n       int
	fields  []map[string]string // form fields of each add, files as its size
	deleted []string
}

func (f *fakeElevenLabs) start(t *testing.T) string {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/voices/add", func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		got := map[string]string{}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			got[p.FormName()] = string(b)
			if p.FormName() == "files" {
				got["files"] = fmt.Sprint(len(b))
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.n++
		f.fields = append(f.fields, got)
		fmt.Fprintf(w, `{"voice_id":"voice-%d","requires_verification":false}`, f.n)
	})
	mux.HandleFunc("DELETE /v1/voices/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.deleted = append(f.deleted, r.PathValue("id"))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func writeAudio(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mama.mp3")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUsageAndEnv(t *testing.T) {
	full := env(map[string]string{"DATABASE_URL": "postgres://x", "ELEVENLABS_API_KEY": "k"})
	cases := []struct {
		args   []string
		getenv func(string) string
		code   int
		stderr string
	}{
		{nil, full, 2, "usage"},
		{[]string{"--device", device}, full, 2, "usage"},
		{[]string{"--device", device, "--file", "a.mp3", "--delete"}, full, 2, "usage"},
		{[]string{"--device", device, "--delete"}, env(nil), 1, "DATABASE_URL and ELEVENLABS_API_KEY not set"},
		{[]string{"--device", device, "--file", writeAudio(t, "%PDF")}, full, 1, "is not mp3"},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if code := run(context.Background(), c.args, c.getenv, &out, &errb, "http://127.0.0.1:1"); code != c.code || !strings.Contains(errb.String(), c.stderr) {
			t.Errorf("%v: code %d, stderr %q; want %d and %q", c.args, code, errb.String(), c.code, c.stderr)
		}
	}
}

func voiceOf(t *testing.T, pool *sql.DB) (voice sql.NullString, consent bool) {
	t.Helper()
	if err := pool.QueryRow(`SELECT voice_id, voice_consent_at IS NOT NULL AND voice_consent_sha256 IS NOT NULL FROM devices WHERE id = $1`, device).
		Scan(&voice, &consent); err != nil {
		t.Fatal(err)
	}
	return voice, consent
}

func TestSetReplaceDeleteAgainstFake(t *testing.T) {
	pool := dbtest.Fresh(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(`INSERT INTO devices (id, phone_number, duress_phrase) VALUES ($1, '+584141234567', 'x')`, device); err != nil {
		t.Fatal(err)
	}
	// dbtest gives a URL-less pool; setvoice needs the URL, so rebuild it
	// from the same database name.
	var name string
	if err := pool.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	dbURL := strings.Replace(os.Getenv("TEST_DATABASE_URL"), "/postgres?", "/"+name+"?", 1)
	if !strings.Contains(dbURL, name) {
		t.Skip("TEST_DATABASE_URL does not end in /postgres?...: cannot point setvoice at the test database")
	}
	fake := &fakeElevenLabs{}
	elURL := fake.start(t)
	getenv := env(map[string]string{"DATABASE_URL": dbURL, "ELEVENLABS_API_KEY": "k"})
	audio := writeAudio(t, "ID3\x04\x00 fake mp3")

	var out, errb bytes.Buffer
	if code := run(ctx, []string{"--device", device, "--file", audio, "--description", "demo"}, getenv, &out, &errb, elURL); code != 0 {
		t.Fatalf("set: code %d, stderr %s", code, errb.String())
	}
	if v, consent := voiceOf(t, pool); v.String != "voice-1" || !consent {
		t.Fatalf("after set: voice %v consent %v", v, consent)
	}
	got := fake.fields[0]
	if got["name"] != "mama-3f1c9a52" || got["remove_background_noise"] != "false" || got["description"] != "demo" || got["files"] == "" {
		t.Fatalf("ElevenLabs got %v", got)
	}
	if !strings.Contains(out.String(), "voice_id voice-1") {
		t.Fatalf("stdout %q", out.String())
	}

	if code := run(ctx, []string{"--device", device, "--file", audio, "--remove-background-noise", "--name", "mama-test-107-x"}, getenv, &out, &errb, elURL); code != 0 {
		t.Fatalf("replace: code %d, stderr %s", code, errb.String())
	}
	if v, _ := voiceOf(t, pool); v.String != "voice-2" {
		t.Fatalf("after replace: voice %v", v)
	}
	if got := fake.fields[1]; got["name"] != "mama-test-107-x" || got["remove_background_noise"] != "true" {
		t.Fatalf("ElevenLabs got %v", got)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "voice-1" {
		t.Fatalf("deleted %v, want [voice-1]", fake.deleted)
	}

	if code := run(ctx, []string{"--device", device, "--delete"}, getenv, &out, &errb, elURL); code != 0 {
		t.Fatalf("delete: code %d, stderr %s", code, errb.String())
	}
	if v, consent := voiceOf(t, pool); v.Valid || consent {
		t.Fatalf("after delete: voice %v consent %v", v, consent)
	}
	if len(fake.deleted) != 2 || fake.deleted[1] != "voice-2" {
		t.Fatalf("deleted %v, want voice-2 too", fake.deleted)
	}

	errb.Reset()
	if code := run(ctx, []string{"--device", "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", "--file", audio}, getenv, &out, &errb, elURL); code != 1 || !strings.Contains(errb.String(), "register the device first") {
		t.Fatalf("unknown device: code %d, stderr %q", code, errb.String())
	}
}
