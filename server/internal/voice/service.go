package voice

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call/agent"
)

// PreviewText is what GET /devices/{id}/voice/preview says.
const PreviewText = "¡Hola, mi amor! ¿Cómo vas?"

// ErrElevenLabs wraps every ElevenLabs failure, so callers answer 502.
var ErrElevenLabs = errors.New("voice: ElevenLabs failed")

// ElevenLabs is the part of Client that Service uses.
type ElevenLabs interface {
	Add(ctx context.Context, v NewVoice, s Sample) (string, error)
	Delete(ctx context.Context, voiceID string) error
	Speak(ctx context.Context, s Speech) ([]byte, error)
}

var _ ElevenLabs = (*Client)(nil)

// Service sets, removes and previews a device's voice. Create it with
// NewService.
type Service struct {
	store Store
	el    ElevenLabs
	now   func() time.Time

	mu       sync.Mutex
	previews map[string][]byte // voice_id -> MP3; a voice's preview never changes
}

// NewService returns the Service on store and el.
func NewService(store Store, el ElevenLabs) *Service {
	return &Service{store: store, el: el, now: time.Now, previews: map[string][]byte{}}
}

// DefaultName is the ElevenLabs name of a device's voice: mama-<first 8
// characters of the device id>.
func DefaultName(deviceID string) string {
	short := deviceID
	if len(short) > 8 {
		short = short[:8]
	}
	return "mama-" + short
}

// Set clones sample into a new voice and makes it the device's voice. The
// owner's consent is recorded now, with the SHA-256 of the audio; the audio
// itself is not kept. A blank v.Name is DefaultName. The voice it replaces
// is deleted from ElevenLabs only after the new one is stored; if that
// delete fails it is logged and the new voice stays. If ElevenLabs fails to
// create the voice, the error wraps ErrElevenLabs and the old voice stays.
func (s *Service) Set(ctx context.Context, deviceID string, v NewVoice, sample Sample) (string, error) {
	if _, err := s.store.Device(ctx, deviceID); err != nil {
		return "", err
	}
	if v.Name == "" {
		v.Name = DefaultName(deviceID)
	}
	sum := sha256.Sum256(sample.Audio)
	voiceID, err := s.el.Add(ctx, v, sample)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrElevenLabs, err)
	}
	// The cleanup deletes below run even if the caller went away, but not
	// forever.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	old, err := s.store.SetVoice(ctx, deviceID, voiceID, s.now().UTC(), sum[:])
	if err != nil {
		// Not stored: delete the clone so it does not hold a voice slot.
		if derr := s.el.Delete(cleanup, voiceID); derr != nil {
			log.Printf("voice: orphaned voice %s after a failed store: %v", voiceID, derr)
		}
		return "", err
	}
	if old != "" && old != voiceID {
		if err := s.el.Delete(cleanup, old); err != nil && !errors.Is(err, ErrVoiceNotFound) {
			log.Printf("voice: could not delete replaced voice %s: %v", old, err)
		}
		s.forget(old)
	}
	return voiceID, nil
}

// Remove deletes the device's voice from ElevenLabs and clears its columns,
// so its calls go back to the global voice. With no voice it does nothing.
// If ElevenLabs fails (other than "already gone"), the error wraps
// ErrElevenLabs and nothing changes.
func (s *Service) Remove(ctx context.Context, deviceID string) error {
	d, err := s.store.Device(ctx, deviceID)
	if err != nil || d.VoiceID == "" {
		return err
	}
	if err := s.el.Delete(ctx, d.VoiceID); err != nil && !errors.Is(err, ErrVoiceNotFound) {
		return fmt.Errorf("%w: %w", ErrElevenLabs, err)
	}
	s.forget(d.VoiceID)
	return s.store.ClearVoice(ctx, deviceID, d.VoiceID)
}

// ErrNoVoice means the device has no voice of its own.
var ErrNoVoice = errors.New("voice: device has no voice")

// maxPreviews bounds the preview cache; past it the cache starts over.
const maxPreviews = 64

// Preview returns PreviewText said by the device's voice as MP3, with the
// agent's TTS model and settings (call/agent/tts.json), so it sounds like
// the call. It is cached per voice, so playing it again costs no credits.
func (s *Service) Preview(ctx context.Context, deviceID string) ([]byte, error) {
	d, err := s.store.Device(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if d.VoiceID == "" {
		return nil, ErrNoVoice
	}
	s.mu.Lock()
	mp3, ok := s.previews[d.VoiceID]
	s.mu.Unlock()
	if ok {
		return mp3, nil
	}
	tts, err := agent.LoadTTS()
	if err != nil {
		return nil, err
	}
	mp3, err = s.el.Speak(ctx, Speech{
		VoiceID:  d.VoiceID,
		Text:     PreviewText,
		ModelID:  tts.ModelID,
		Settings: Settings{Stability: tts.Stability, SimilarityBoost: tts.SimilarityBoost, Speed: tts.Speed},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrElevenLabs, err)
	}
	s.mu.Lock()
	if len(s.previews) >= maxPreviews {
		s.previews = map[string][]byte{}
	}
	s.previews[d.VoiceID] = mp3
	s.mu.Unlock()
	return mp3, nil
}

func (s *Service) forget(voiceID string) {
	s.mu.Lock()
	delete(s.previews, voiceID)
	s.mu.Unlock()
}
