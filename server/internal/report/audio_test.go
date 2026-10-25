package report

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeElevenLabs answers the conversation and audio endpoints. hasAudio
// says what GET /v1/convai/conversations/{id} reports.
type fakeElevenLabs struct {
	hasAudio    atomic.Bool
	audio       []byte
	audioType   string
	audioStatus int
	details     atomic.Int32
	downloads   atomic.Int32
	key         atomic.Value
}

func (f *fakeElevenLabs) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.key.Store(r.Header.Get("xi-api-key"))
		switch {
		case r.URL.Path == "/v1/convai/conversations/conv_01":
			f.details.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if f.hasAudio.Load() {
				_, _ = w.Write([]byte(`{"conversation_id":"conv_01","status":"done","has_audio":true,"has_user_audio":true,"has_response_audio":true}`))
			} else {
				_, _ = w.Write([]byte(`{"conversation_id":"conv_01","status":"processing","has_audio":false}`))
			}
		case r.URL.Path == "/v1/convai/conversations/conv_01/audio":
			f.downloads.Add(1)
			if f.audioStatus != 0 {
				w.WriteHeader(f.audioStatus)
				return
			}
			typ := f.audioType
			if typ == "" {
				typ = "audio/mpeg"
			}
			w.Header().Set("Content-Type", typ)
			_, _ = w.Write(f.audio)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestElevenLabsAudioFetchesWhenHasAudio(t *testing.T) {
	f := &fakeElevenLabs{audio: []byte("ID3 fake mp3")}
	f.hasAudio.Store(true)
	srv := f.server(t)
	e := &ElevenLabsAudio{BaseURL: srv.URL, APIKey: "xi-test", Client: srv.Client()}

	audio, typ, err := e.Fetch(context.Background(), "conv_01")
	if err != nil || string(audio) != "ID3 fake mp3" || typ != "audio/mpeg" {
		t.Fatalf("Fetch = %q %q %v", audio, typ, err)
	}
	if f.key.Load() != "xi-test" || f.details.Load() != 1 || f.downloads.Load() != 1 {
		t.Errorf("key %v, details %d, downloads %d", f.key.Load(), f.details.Load(), f.downloads.Load())
	}
}

func TestElevenLabsAudioNotReady(t *testing.T) {
	f := &fakeElevenLabs{audio: []byte("x")}
	srv := f.server(t)
	e := &ElevenLabsAudio{BaseURL: srv.URL, APIKey: "k", Client: srv.Client()}
	if _, _, err := e.Fetch(context.Background(), "conv_01"); !errors.Is(err, ErrAudioNotReady) || f.downloads.Load() != 0 {
		t.Errorf("has_audio false: err %v, downloads %d; want not ready and no download", err, f.downloads.Load())
	}
	if _, _, err := e.Fetch(context.Background(), "conv_unknown"); !errors.Is(err, ErrAudioNotReady) {
		t.Errorf("404 conversation: err %v, want not ready", err)
	}
}

func TestElevenLabsAudioErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		typ       string
		audio     string
		retryable bool
	}{
		{"server error", http.StatusBadGateway, "", "", true},
		{"rate limited", http.StatusTooManyRequests, "", "", true},
		{"bad key", http.StatusUnauthorized, "", "", false},
		{"not audio", 0, "text/html", "<html>", false},
		{"empty", 0, "audio/mpeg", "", true},
	} {
		f := &fakeElevenLabs{audio: []byte(tc.audio), audioType: tc.typ, audioStatus: tc.status}
		f.hasAudio.Store(true)
		srv := f.server(t)
		_, _, err := (&ElevenLabsAudio{BaseURL: srv.URL, APIKey: "k", Client: srv.Client()}).Fetch(context.Background(), "conv_01")
		if err == nil || errors.Is(err, ErrAudioNotReady) != tc.retryable {
			t.Errorf("%s: err %v, want retryable=%t", tc.name, err, tc.retryable)
		}
	}
	if _, _, err := (&ElevenLabsAudio{}).Fetch(context.Background(), "conv_01"); err == nil || errors.Is(err, ErrAudioNotReady) {
		t.Errorf("no key: err %v, want a permanent error", err)
	}
}

// scriptedFetcher returns errs in order, then the audio.
type scriptedFetcher struct {
	errs  []error
	calls int
}

func (s *scriptedFetcher) Fetch(context.Context, string) ([]byte, string, error) {
	s.calls++
	if s.calls <= len(s.errs) {
		return nil, "", s.errs[s.calls-1]
	}
	return []byte("mp3"), "audio/mpeg", nil
}

func TestFetchWithRetry(t *testing.T) {
	short := []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

	f := &scriptedFetcher{errs: []error{ErrAudioNotReady, ErrAudioNotReady}}
	if audio, _, err := fetchWithRetry(context.Background(), f, "c", short); err != nil || string(audio) != "mp3" || f.calls != 3 {
		t.Errorf("not ready twice: %q %v after %d calls", audio, err, f.calls)
	}

	f = &scriptedFetcher{errs: []error{errors.New("bad key")}}
	if _, _, err := fetchWithRetry(context.Background(), f, "c", short); err == nil || f.calls != 1 {
		t.Errorf("permanent error: %v after %d calls, want 1", err, f.calls)
	}

	f = &scriptedFetcher{errs: []error{ErrAudioNotReady, ErrAudioNotReady, ErrAudioNotReady, ErrAudioNotReady, ErrAudioNotReady}}
	if _, _, err := fetchWithRetry(context.Background(), f, "c", short); !errors.Is(err, ErrAudioNotReady) || f.calls != 4 {
		t.Errorf("never ready: %v after %d calls, want 4 (1 + 3 retries)", err, f.calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f = &scriptedFetcher{errs: []error{ErrAudioNotReady, ErrAudioNotReady}}
	if _, _, err := fetchWithRetry(ctx, f, "c", []time.Duration{time.Hour}); err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Errorf("canceled: %v", err)
	}
}

func TestDefaultBackoffIsAboutTwoMinutes(t *testing.T) {
	var total time.Duration
	for _, d := range DefaultBackoff {
		total += d
	}
	if total < 100*time.Second || total > 150*time.Second {
		t.Errorf("DefaultBackoff waits %s in all, want about 2 min", total)
	}
}
