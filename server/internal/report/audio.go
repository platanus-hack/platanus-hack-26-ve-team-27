package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ElevenLabsBaseURL is the ElevenLabs API.
const ElevenLabsBaseURL = "https://api.elevenlabs.io"

// MaxAudioBytes bounds one recording. A minute of call is about 1 MB
// (16 kHz mono MP3), so this is over an hour.
const MaxAudioBytes = 64 << 20

// ErrAudioNotReady means ElevenLabs has no audio for the conversation yet
// (has_audio is false, or the audio endpoint is not ready): try again later.
var ErrAudioNotReady = errors.New("report: recording not ready")

// AudioFetcher downloads a call's recording. *ElevenLabsAudio is the real one.
type AudioFetcher interface {
	// Fetch returns the recording and its media type, ErrAudioNotReady when
	// it is not available yet, or another error when retrying won't help.
	Fetch(ctx context.Context, conversationID string) (audio []byte, contentType string, err error)
}

// ElevenLabsAudio fetches recordings from the ElevenLabs Agents API with the
// server's ELEVENLABS_API_KEY: GET /v1/convai/conversations/{id} tells
// whether the audio exists (has_audio), and .../audio returns it (audio/mpeg).
// Create it with a struct literal; BaseURL and Client default to
// ElevenLabsBaseURL and a client with a 60 s timeout.
type ElevenLabsAudio struct {
	BaseURL string
	APIKey  string // sent as xi-api-key
	Client  *http.Client
}

var _ AudioFetcher = (*ElevenLabsAudio)(nil)

type conversationDetails struct {
	Status   string `json:"status"`
	HasAudio bool   `json:"has_audio"`
}

// Fetch checks has_audio, then downloads the audio. A 404, 409, 425 or 5xx
// answer is "not ready yet"; 401 and 403 are a bad key and are not retried.
func (e *ElevenLabsAudio) Fetch(ctx context.Context, conversationID string) ([]byte, string, error) {
	if e.APIKey == "" {
		return nil, "", errors.New("report: ELEVENLABS_API_KEY is not set")
	}
	if conversationID == "" {
		return nil, "", errors.New("report: no conversation_id")
	}
	base := strings.TrimSuffix(e.BaseURL, "/")
	if base == "" {
		base = ElevenLabsBaseURL
	}
	path := base + "/v1/convai/conversations/" + url.PathEscape(conversationID)

	resp, err := e.get(ctx, path)
	if err != nil {
		return nil, "", err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return nil, "", err
	}
	var d conversationDetails
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, "", fmt.Errorf("elevenlabs: decode conversation: %w", err)
	}
	if !d.HasAudio {
		return nil, "", fmt.Errorf("%w: has_audio is false (status %q)", ErrAudioNotReady, d.Status)
	}

	resp, err = e.get(ctx, path+"/audio")
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return nil, "", err
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ == "" || typ == "application/octet-stream" {
		typ = "audio/mpeg"
	}
	if !strings.HasPrefix(typ, "audio/") {
		return nil, "", fmt.Errorf("elevenlabs: audio answered %q, not audio", typ)
	}
	audio, err := io.ReadAll(io.LimitReader(resp.Body, MaxAudioBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("elevenlabs: read audio: %w", err)
	}
	if len(audio) > MaxAudioBytes {
		return nil, "", fmt.Errorf("elevenlabs: audio is over %d bytes", MaxAudioBytes)
	}
	if len(audio) == 0 {
		return nil, "", fmt.Errorf("%w: empty audio", ErrAudioNotReady)
	}
	return audio, typ, nil
}

func (e *ElevenLabsAudio) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", e.APIKey)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// A network error may pass: retry it like "not ready".
		return nil, fmt.Errorf("%w: elevenlabs: %v", ErrAudioNotReady, err)
	}
	return resp, nil
}

// statusErr maps a non-2xx answer: retryable ones wrap ErrAudioNotReady.
func statusErr(resp *http.Response) error {
	switch c := resp.StatusCode; {
	case c >= 200 && c < 300:
		return nil
	case c == http.StatusNotFound, c == http.StatusConflict, c == http.StatusTooEarly, c == http.StatusTooManyRequests, c >= 500:
		return fmt.Errorf("%w: elevenlabs: %s", ErrAudioNotReady, resp.Status)
	default:
		return fmt.Errorf("elevenlabs: %s", resp.Status)
	}
}

// DefaultBackoff is the wait before each retry of a recording that is not
// ready: about two minutes in all, as #76 asks.
var DefaultBackoff = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}

// fetchWithRetry calls f until it returns something other than
// ErrAudioNotReady, waiting backoff[i] before retry i+1. It returns the
// last error when the waits run out or ctx ends.
func fetchWithRetry(ctx context.Context, f AudioFetcher, conversationID string, backoff []time.Duration) ([]byte, string, error) {
	for attempt := 0; ; attempt++ {
		audio, typ, err := f.Fetch(ctx, conversationID)
		if err == nil || !errors.Is(err, ErrAudioNotReady) || attempt >= len(backoff) {
			return audio, typ, err
		}
		t := time.NewTimer(backoff[attempt])
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, "", fmt.Errorf("%w (gave up: %v)", err, ctx.Err())
		case <-t.C:
		}
	}
}
