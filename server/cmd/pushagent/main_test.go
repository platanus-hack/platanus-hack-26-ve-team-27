package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call/agent"
)

const current = `{"agent_id":"agent_x","conversation_config":{"agent":{"first_message":"old","prompt":{"prompt":"old prompt"}}}}`

// fakeAgent serves GET and PATCH on /v1/convai/agents/agent_x and records
// the order of the calls and the PATCH body.
type fakeAgent struct {
	calls       []string
	patch       []byte
	patchStatus int
	backupDir   string
	backedUp    bool // a backup file existed when PATCH arrived
}

func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/convai/agents/agent_x" || r.Header.Get("xi-api-key") != "key" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.calls = append(f.calls, r.Method)
	switch r.Method {
	case http.MethodGet:
		io.WriteString(w, current)
	case http.MethodPatch:
		entries, _ := os.ReadDir(f.backupDir)
		f.backedUp = len(entries) == 1
		f.patch, _ = io.ReadAll(r.Body)
		w.WriteHeader(f.patchStatus)
		io.WriteString(w, `{}`)
	}
}

func newPusher(t *testing.T, f *fakeAgent) *pusher {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.backupDir = filepath.Join(t.TempDir(), "backup")
	return &pusher{
		BaseURL: srv.URL, APIKey: "key", AgentID: "agent_x", BackupDir: f.backupDir,
		Now: func() time.Time { return time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC) },
	}
}

func TestPushBacksUpBeforeWriting(t *testing.T) {
	f := &fakeAgent{patchStatus: http.StatusOK}
	p := newPusher(t, f)
	body, _ := json.Marshal(agent.NewUpdate())

	path, err := p.Push(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.calls, ",") != "GET,PATCH" {
		t.Fatalf("calls = %v, want GET then PATCH", f.calls)
	}
	if !f.backedUp {
		t.Error("PATCH arrived before the backup was written")
	}
	if want := filepath.Join(f.backupDir, "agent_x-20261024T090000Z.json"); path != want {
		t.Errorf("backup path = %q, want %q", path, want)
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != current {
		t.Errorf("backup = %q, %v; want the GET body", saved, err)
	}
	if !bytes.Equal(f.patch, body) {
		t.Errorf("PATCH body = %s", f.patch)
	}
}

func TestPushFailedPatchKeepsBackup(t *testing.T) {
	f := &fakeAgent{patchStatus: http.StatusUnprocessableEntity}
	p := newPusher(t, f)
	path, err := p.Push(context.Background(), []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("err = %v, want the 422", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("backup missing after failed PATCH: %v", statErr)
	}
}

func TestPushNeedsConfig(t *testing.T) {
	f := &fakeAgent{patchStatus: http.StatusOK}
	p := newPusher(t, f)
	p.APIKey = ""
	if _, err := p.Push(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("Push without ELEVENLABS_API_KEY succeeded")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %v, want none", f.calls)
	}
}

func TestDryRunPrintsBody(t *testing.T) {
	var out bytes.Buffer
	if err := printBody(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"prompt": "# Quién eres`,
		`"first_message": "¡Hola, {{nombre}}! ¿Cómo vas, mi amor?"`,
		`"language": "es"`,
		`"system_tool_type": "end_call"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run output misses %s", want)
		}
	}
}

func TestRestoreBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(path, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := restoreBody(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"conversation_config":{"agent":{"first_message":"old","prompt":{"prompt":"old prompt"}}}}`
	if string(got) != want {
		t.Errorf("restore body = %s, want %s", got, want)
	}
}
