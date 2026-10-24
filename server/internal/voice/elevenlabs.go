// Package voice gives a device its own "Mamá" voice (#107): an audio file
// of the mother's voice becomes an ElevenLabs instant voice clone, stored as
// devices.voice_id, and every later call of that device speaks with it (the
// call engine reads the column, #104).
//
// Routes (docs/contract.md "Device voice"), registered by Handler.Register:
//
//	POST   /devices/{id}/voice           201 {"voice_id"}; 400 bad form, no consent=true or not
//	                                     mp3/wav/m4a/ogg/webm; 401 missing or bad signature;
//	                                     404 unknown device; 409 device has no bound key;
//	                                     413 audio over 10 MB; 429 one upload per minute;
//	                                     502 ElevenLabs failed (the old voice stays)
//	DELETE /devices/{id}/voice           204 removed (or there was none); 401, 404, 409 as above;
//	                                     502 ElevenLabs failed (nothing changed)
//	GET    /devices/{id}/voice/preview   200 audio/mpeg; 404 unknown device or no voice; 502
//
// The audio is read into memory and sent to ElevenLabs; it is never written
// to disk or to the database. The server keeps only its SHA-256 and the
// consent time. Every route answers 503 without a database or without
// ELEVENLABS_API_KEY. Errors are {"error": "..."}.
//
// Service does the work for both the routes and go run ./cmd/setvoice.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

// BaseURL is the ElevenLabs API.
const BaseURL = "https://api.elevenlabs.io"

// ErrAuth means ElevenLabs refused the key (401 or 403): it is wrong, or it
// lacks the voices write permission (README "Secrets").
var ErrAuth = errors.New("elevenlabs: the API key was refused (401/403): check it has the Voices write permission")

// ErrVoiceNotFound means ElevenLabs has no voice with that id.
var ErrVoiceNotFound = errors.New("elevenlabs: voice not found")

// Client talks to the ElevenLabs voices and text-to-speech APIs. Create it
// with a struct literal; BaseURL and HTTP default to BaseURL and
// http.DefaultClient.
type Client struct {
	BaseURL string // tests point it at an httptest.Server
	APIKey  string // sent as xi-api-key
	HTTP    *http.Client
}

// Sample is the audio a voice is cloned from.
type Sample struct {
	Audio  []byte
	Format Format
}

// NewVoice is what Add sends besides the sample.
type NewVoice struct {
	Name        string
	Description string // optional
	// RemoveBackgroundNoise asks ElevenLabs to isolate the voice first. It
	// can deform the timbre of a clean recording.
	RemoveBackgroundNoise bool
}

// Add creates an instant voice clone and returns its voice_id:
// POST /v1/voices/add, multipart name, files, remove_background_noise and
// description (https://elevenlabs.io/docs/api-reference/voices/ivc/create).
func (c *Client) Add(ctx context.Context, v NewVoice, s Sample) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("name", v.Name)
	_ = mw.WriteField("remove_background_noise", strconv.FormatBool(v.RemoveBackgroundNoise))
	if v.Description != "" {
		_ = mw.WriteField("description", v.Description)
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="files"; filename="sample.`+s.Format.Ext+`"`)
	h.Set("Content-Type", s.Format.ContentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(s.Audio); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	raw, err := c.do(ctx, http.MethodPost, "/v1/voices/add", mw.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}
	var out struct {
		VoiceID string `json:"voice_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.VoiceID == "" {
		return "", fmt.Errorf("elevenlabs: add voice: no voice_id in the response")
	}
	return out.VoiceID, nil
}

// Delete removes a voice: DELETE /v1/voices/{voice_id}. A voice that is
// already gone is ErrVoiceNotFound.
func (c *Client) Delete(ctx context.Context, voiceID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/voices/"+url.PathEscape(voiceID), "", nil)
	return err
}

// Settings are the voice_settings of a text-to-speech request. A nil
// UseSpeakerBoost leaves it out (ElevenLabs default: on).
type Settings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	UseSpeakerBoost *bool   `json:"use_speaker_boost,omitempty"`
	Speed           float64 `json:"speed,omitempty"`
}

// Speech is one text-to-speech request.
type Speech struct {
	VoiceID  string
	Text     string
	ModelID  string
	Settings Settings
}

// Speak returns text said by the voice, as MP3 (mp3_44100_128):
// POST /v1/text-to-speech/{voice_id}
// (https://elevenlabs.io/docs/api-reference/text-to-speech/convert).
func (c *Client) Speak(ctx context.Context, s Speech) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Text          string   `json:"text"`
		ModelID       string   `json:"model_id,omitempty"`
		VoiceSettings Settings `json:"voice_settings"`
	}{s.Text, s.ModelID, s.Settings})
	if err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, "/v1/text-to-speech/"+url.PathEscape(s.VoiceID)+"?output_format=mp3_44100_128", "application/json", bytes.NewReader(payload))
}

// maxResponseBytes bounds a response; a preview MP3 is well under it.
const maxResponseBytes = 16 << 20

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader) ([]byte, error) {
	if c.APIKey == "" {
		return nil, errors.New("elevenlabs: ELEVENLABS_API_KEY is not set")
	}
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(base, "/")+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("xi-api-key", c.APIKey)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: read response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrAuth, snippet(raw))
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s", ErrVoiceNotFound, snippet(raw))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("elevenlabs: %s: %s", resp.Status, snippet(raw))
	}
	return raw, nil
}

// snippet trims a provider body to something short enough to log.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
