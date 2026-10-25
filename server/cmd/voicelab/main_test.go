package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeTTS answers POST /v1/text-to-speech/{id} with mp3 and records the
// requests.
type fakeTTS struct {
	mu   sync.Mutex
	reqs []map[string]any
}

func (f *fakeTTS) start(t *testing.T, mp3 []byte) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/text-to-speech/voice-x" || r.Header.Get("xi-api-key") != "k" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.reqs = append(f.reqs, body)
		f.mu.Unlock()
		_, _ = w.Write(mp3)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func env(k string) string {
	if k == "ELEVENLABS_API_KEY" {
		return "k"
	}
	return ""
}

func TestGridAndIndex(t *testing.T) {
	dir := t.TempDir()
	mp3 := []byte("ID3not-really-mp3")
	_, ffErr := exec.LookPath("ffmpeg")
	if ffErr == nil {
		// A real MP3, so the phone versions can be made from it.
		p := filepath.Join(dir, "tone.mp3")
		if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=f=300:d=0.5", p).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		mp3, _ = os.ReadFile(p)
	} else {
		t.Setenv("PATH", t.TempDir())
	}
	fake := &fakeTTS{}
	url := fake.start(t, mp3)
	out := filepath.Join(dir, "voicelab-out")
	orig := filepath.Join(dir, "muestra.mp3")
	if err := os.WriteFile(orig, mp3, 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--voice", "voice-x", "--models", "eleven_flash_v2_5", "--lines", "2", "--out", out, "--original", orig}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), args, env, &stdout, &stderr, url); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	// 3 stabilities × 2 similarities × 2 boosts × 2 lines.
	if len(fake.reqs) != 24 {
		t.Fatalf("%d TTS requests, want 24", len(fake.reqs))
	}
	if !strings.Contains(stdout.String(), "12 settings × 2 lines = 24 requests") {
		t.Fatalf("stdout %q", stdout.String())
	}
	first := fake.reqs[0]
	settings := first["voice_settings"].(map[string]any)
	if first["text"] != defaultLines[0] || first["model_id"] != "eleven_flash_v2_5" ||
		settings["stability"] != 0.35 || settings["similarity_boost"] != 0.75 || settings["use_speaker_boost"] != true || settings["speed"] != 1.0 {
		t.Fatalf("first request %v", first)
	}
	if off := fake.reqs[2]["voice_settings"].(map[string]any); off["use_speaker_boost"] != false {
		t.Fatalf("third request (boost off) %v", off)
	}
	for _, name := range []string{"eleven_flash_v2_5_st035_sim075_boost1_l1.mp3", "eleven_flash_v2_5_st065_sim090_boost0_l2.mp3", "original.mp3", "index.html"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	index, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if n := strings.Count(string(index), "<audio"); ffErr == nil && n != 2+24*2 || ffErr != nil && n != 1+24 {
		t.Fatalf("index.html has %d players", n)
	}
	if ffErr == nil {
		rate, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=sample_rate", "-of", "csv=p=0",
			filepath.Join(out, "eleven_flash_v2_5_st035_sim075_boost1_l1_tel.wav")).Output()
		if err != nil || strings.TrimSpace(string(rate)) != "8000" {
			t.Fatalf("phone version: rate %q, err %v", rate, err)
		}
	}

	// A second run reuses the files: no request, no credits.
	stdout.Reset()
	if code := run(context.Background(), args, env, &stdout, &stderr, url); code != 0 {
		t.Fatalf("second run: code %d: %s", code, stderr.String())
	}
	if len(fake.reqs) != 24 || !strings.Contains(stdout.String(), "this run: about 0 credits") {
		t.Fatalf("second run made %d requests; stdout %q", len(fake.reqs)-24, stdout.String())
	}
}

func TestDryRunCallsNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--voice", "v", "--dry-run", "--out", filepath.Join(t.TempDir(), "o")}, func(string) string { return "" }, &stdout, &stderr, "http://127.0.0.1:1")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	// 3 models × 12 settings; the three lines are 80 characters.
	if !strings.Contains(stdout.String(), "36 settings × 3 lines = 108 requests") {
		t.Fatalf("stdout %q", stdout.String())
	}
}
