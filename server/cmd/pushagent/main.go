// Command pushagent uploads the "Mamá" prompt and first message
// (server/internal/call/agent) to the ElevenLabs agent:
//
//	cd server && go run ./cmd/pushagent            # backup, then PATCH
//	cd server && go run ./cmd/pushagent --dry-run  # print the body only
//	cd server && go run ./cmd/pushagent --restore cmd/pushagent/backup/<file>.json
//
// It reads ELEVENLABS_API_KEY and ELEVENLABS_AGENT_ID. Before writing, it
// saves the agent's current configuration (GET) under --backup-dir
// (gitignored), so --restore can put its agent section back.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call/agent"
)

// pusher talks to /v1/convai/agents/{agent_id}.
type pusher struct {
	BaseURL   string // call.BaseURL; tests point it at an httptest.Server
	APIKey    string // sent as xi-api-key
	AgentID   string
	BackupDir string
	Client    *http.Client // defaults to http.DefaultClient
	Now       func() time.Time
}

func main() {
	dryRun := flag.Bool("dry-run", false, "print the PATCH body without sending it")
	backupDir := flag.String("backup-dir", "cmd/pushagent/backup", "where to save the current agent configuration before writing")
	restore := flag.String("restore", "", "backup file whose agent section is sent back instead of the prompt")
	flag.Parse()

	if *dryRun {
		if err := printBody(os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	var body []byte
	var err error
	if *restore != "" {
		body, err = restoreBody(*restore)
	} else {
		body, err = json.Marshal(agent.NewUpdate())
	}
	if err != nil {
		log.Fatal(err)
	}
	p := &pusher{
		BaseURL:   call.BaseURL,
		APIKey:    os.Getenv("ELEVENLABS_API_KEY"),
		AgentID:   os.Getenv("ELEVENLABS_AGENT_ID"),
		BackupDir: *backupDir,
		Client:    &http.Client{Timeout: 30 * time.Second},
		Now:       time.Now,
	}
	backup, err := p.Push(context.Background(), body)
	if backup != "" {
		log.Printf("pushagent: previous configuration saved to %s", backup)
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("pushagent: agent updated")
}

// printBody writes the PATCH body, indented, to w.
func printBody(w io.Writer) error {
	raw, err := json.MarshalIndent(agent.NewUpdate(), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", raw)
	return err
}

// Push saves the current agent configuration under BackupDir and then
// PATCHes body. It returns the backup path; it never PATCHes without one.
func (p *pusher) Push(ctx context.Context, body []byte) (string, error) {
	if p.APIKey == "" || p.AgentID == "" {
		return "", errors.New("pushagent: ELEVENLABS_API_KEY and ELEVENLABS_AGENT_ID are required")
	}
	current, err := p.do(ctx, http.MethodGet, nil)
	if err != nil {
		return "", fmt.Errorf("pushagent: read current agent: %w", err)
	}
	if err := os.MkdirAll(p.BackupDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(p.BackupDir, fmt.Sprintf("%s-%s.json", p.AgentID, p.Now().UTC().Format("20060102T150405Z")))
	if err := os.WriteFile(path, current, 0o600); err != nil {
		return "", err
	}
	if _, err := p.do(ctx, http.MethodPatch, body); err != nil {
		return path, fmt.Errorf("pushagent: update agent: %w", err)
	}
	return path, nil
}

func (p *pusher) do(ctx context.Context, method string, body []byte) ([]byte, error) {
	url := strings.TrimSuffix(p.BaseURL, "/") + "/v1/convai/agents/" + p.AgentID
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", p.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Provider errors never echo our key, so the body is safe to show.
		return nil, fmt.Errorf("%s: %.500s", resp.Status, raw)
	}
	return raw, nil
}

// restoreBody reads a backup written by Push and returns a PATCH body with
// its conversation_config.agent, the only section pushagent changes.
func restoreBody(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var backup struct {
		ConversationConfig struct {
			Agent json.RawMessage `json:"agent"`
		} `json:"conversation_config"`
	}
	if err := json.Unmarshal(raw, &backup); err != nil {
		return nil, fmt.Errorf("pushagent: read backup %s: %w", path, err)
	}
	if len(backup.ConversationConfig.Agent) == 0 {
		return nil, fmt.Errorf("pushagent: backup %s has no conversation_config.agent", path)
	}
	return json.Marshal(map[string]any{"conversation_config": map[string]any{"agent": backup.ConversationConfig.Agent}})
}
