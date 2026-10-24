package voice

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const devID = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type harness struct {
	t     *testing.T
	el    *fakeElevenLabs
	store *memStore
	h     *Handler
	mux   *http.ServeMux
	dev   testDevice
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	el := newFakeElevenLabs(t)
	store := newMemStore()
	dev := newTestDevice(t, devID)
	store.put(devID, dev.pub, "")
	h := NewHandler(store, el.client())
	h.now = func() time.Time { return testNow }
	h.svc.now = h.now
	mux := http.NewServeMux()
	h.Register(mux)
	return &harness{t: t, el: el, store: store, h: h, mux: mux, dev: dev}
}

func (hs *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	hs.mux.ServeHTTP(rec, r)
	return rec
}

// upload sends a signed upload of audio with consent=true.
func (hs *harness) upload(audio []byte) *httptest.ResponseRecorder {
	r := uploadRequest(hs.t, devID, audio, "true")
	sum := sha256.Sum256(audio)
	hs.dev.sign(r, testNow, sum[:])
	return hs.serve(r)
}

func (hs *harness) remove() *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodDelete, "/devices/"+devID+"/voice", nil)
	hs.dev.sign(r, testNow, nil)
	return hs.serve(r)
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, want, rec.Body.String())
	}
}

func TestUploadCreatesVoice(t *testing.T) {
	hs := newHarness(t)
	audio := fakeMP3("marker-create")
	rec := hs.upload(audio)
	wantStatus(t, rec, http.StatusCreated)
	var out struct {
		VoiceID string `json:"voice_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.VoiceID != "voice-1" {
		t.Fatalf("body = %s, want voice_id voice-1", rec.Body.String())
	}
	added, _, _ := hs.el.snapshot()
	if len(added) != 1 {
		t.Fatalf("ElevenLabs got %d voices, want 1", len(added))
	}
	a := added[0]
	if a.Name != "mama-3f1c9a52" || a.RemoveNoise != "true" || a.Filename != "sample.mp3" || a.ContentType != "audio/mpeg" || !bytes.Equal(a.Audio, audio) {
		t.Fatalf("ElevenLabs got name=%q remove_background_noise=%q file=%q type=%q (%d bytes)", a.Name, a.RemoveNoise, a.Filename, a.ContentType, len(a.Audio))
	}
	d := hs.store.get(devID)
	sum := sha256.Sum256(audio)
	if d.VoiceID != "voice-1" || !d.consentAt.Equal(testNow) || !bytes.Equal(d.sha, sum[:]) {
		t.Fatalf("stored voice=%q consent_at=%v sha=%x", d.VoiceID, d.consentAt, d.sha)
	}
}

func TestUploadNeedsConsent(t *testing.T) {
	for _, consent := range []string{"", "false", "TRUE", "1"} {
		hs := newHarness(t)
		audio := fakeMP3("x")
		r := uploadRequest(t, devID, audio, consent)
		sum := sha256.Sum256(audio)
		hs.dev.sign(r, testNow, sum[:])
		rec := hs.serve(r)
		wantStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), "consent=true") {
			t.Errorf("consent %q: body %s does not name consent", consent, rec.Body.String())
		}
		if added, _, _ := hs.el.snapshot(); len(added) != 0 {
			t.Errorf("consent %q: ElevenLabs was called", consent)
		}
	}
}

func TestUploadSignature(t *testing.T) {
	audio := fakeMP3("sig")
	sum := sha256.Sum256(audio)
	other := sha256.Sum256([]byte("another file"))
	stranger := newTestDevice(t, devID)
	cases := []struct {
		name string
		sign func(hs *harness, r *http.Request)
	}{
		{"unsigned", func(*harness, *http.Request) {}},
		{"another key", func(_ *harness, r *http.Request) { stranger.sign(r, testNow, sum[:]) }},
		{"another file's hash", func(hs *harness, r *http.Request) { hs.dev.sign(r, testNow, other[:]) }},
		{"too old", func(hs *harness, r *http.Request) { hs.dev.sign(r, testNow.Add(-MaxClockSkew-time.Second), sum[:]) }},
		{"from the future", func(hs *harness, r *http.Request) { hs.dev.sign(r, testNow.Add(MaxClockSkew+time.Second), sum[:]) }},
		{"garbage", func(_ *harness, r *http.Request) {
			r.Header.Set(HeaderSignedAt, "soon")
			r.Header.Set(HeaderSignature, "!!")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs := newHarness(t)
			r := uploadRequest(t, devID, audio, "true")
			c.sign(hs, r)
			wantStatus(t, hs.serve(r), http.StatusUnauthorized)
			if added, _, _ := hs.el.snapshot(); len(added) != 0 {
				t.Fatal("ElevenLabs was called")
			}
		})
	}
}

func TestUploadWithoutBoundKeyIsConflict(t *testing.T) {
	hs := newHarness(t)
	hs.store.put(devID, nil, "")
	wantStatus(t, hs.upload(fakeMP3("k")), http.StatusConflict)
}

func TestUploadUnknownDevice(t *testing.T) {
	hs := newHarness(t)
	r := uploadRequest(t, "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", fakeMP3("u"), "true")
	wantStatus(t, hs.serve(r), http.StatusNotFound)
	r = uploadRequest(t, "not-a-uuid", fakeMP3("u"), "true")
	wantStatus(t, hs.serve(r), http.StatusNotFound)
}

func TestUploadRejectsOtherFormats(t *testing.T) {
	hs := newHarness(t)
	wantStatus(t, hs.upload([]byte("%PDF-1.7 not audio")), http.StatusBadRequest)
	r := uploadRequest(t, devID, nil, "true")
	wantStatus(t, hs.serve(r), http.StatusBadRequest)
}

func TestUploadOver10MB(t *testing.T) {
	hs := newHarness(t)
	audio := append(fakeMP3("big"), make([]byte, MaxAudioBytes)...)
	wantStatus(t, hs.upload(audio), http.StatusRequestEntityTooLarge)
	if added, _, _ := hs.el.snapshot(); len(added) != 0 {
		t.Fatal("ElevenLabs was called")
	}
}

// The audio is read into memory: no temp file is left (ParseMultipartForm
// would spill a large file to one), and the store gets only its hash.
func TestUploadKeepsNoRawAudio(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	if os.TempDir() != tmp {
		t.Skipf("os.TempDir() = %s, cannot redirect it", os.TempDir())
	}
	hs := newHarness(t)
	audio := append(fakeMP3("no-raw-audio-marker"), make([]byte, 9<<20)...) // over any in-memory threshold
	wantStatus(t, hs.upload(audio), http.StatusCreated)
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir has %d entries after the upload, e.g. %s", len(entries), entries[0].Name())
	}
	d := hs.store.get(devID)
	if len(d.sha) != sha256.Size {
		t.Fatalf("stored %d bytes of hash, want %d", len(d.sha), sha256.Size)
	}
}

func TestUploadReplacesAndDeletesOld(t *testing.T) {
	hs := newHarness(t)
	hs.el.addVoice("old-voice")
	hs.store.put(devID, hs.dev.pub, "old-voice")
	wantStatus(t, hs.upload(fakeMP3("new")), http.StatusCreated)
	if got := hs.store.get(devID).VoiceID; got != "voice-1" {
		t.Fatalf("voice_id = %q, want voice-1", got)
	}
	_, deleted, live := hs.el.snapshot()
	if len(deleted) != 1 || deleted[0] != "old-voice" || live["old-voice"] || !live["voice-1"] {
		t.Fatalf("deleted %v, live %v; want old-voice deleted and voice-1 live", deleted, live)
	}
}

func TestUploadElevenLabsFailureKeepsOldVoice(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusForbidden} {
		hs := newHarness(t)
		hs.el.addVoice("old-voice")
		hs.store.put(devID, hs.dev.pub, "old-voice")
		hs.el.failAdd = status
		rec := hs.upload(fakeMP3("fail"))
		wantStatus(t, rec, http.StatusBadGateway)
		if got := hs.store.get(devID).VoiceID; got != "old-voice" {
			t.Fatalf("ElevenLabs %d: voice_id = %q, want old-voice kept", status, got)
		}
		if _, deleted, _ := hs.el.snapshot(); len(deleted) != 0 {
			t.Fatalf("ElevenLabs %d: deleted %v, want nothing", status, deleted)
		}
		if status == http.StatusForbidden && !strings.Contains(rec.Body.String(), "Voices write permission") {
			t.Fatalf("a refused key must say so: %s", rec.Body.String())
		}
	}
}

func TestUploadOncePerMinute(t *testing.T) {
	hs := newHarness(t)
	wantStatus(t, hs.upload(fakeMP3("a")), http.StatusCreated)
	rec := hs.upload(fakeMP3("b"))
	wantStatus(t, rec, http.StatusTooManyRequests)
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}
	testNow = testNow.Add(UploadInterval)
	defer func() { testNow = testNow.Add(-UploadInterval) }()
	wantStatus(t, hs.upload(fakeMP3("c")), http.StatusCreated)
}

func TestDeleteGoesBackToGlobalVoice(t *testing.T) {
	hs := newHarness(t)
	wantStatus(t, hs.upload(fakeMP3("del")), http.StatusCreated)
	wantStatus(t, hs.remove(), http.StatusNoContent)
	d := hs.store.get(devID)
	if d.VoiceID != "" || !d.consentAt.IsZero() || d.sha != nil {
		t.Fatalf("after DELETE: voice=%q consent_at=%v sha=%x, want all cleared", d.VoiceID, d.consentAt, d.sha)
	}
	if _, deleted, live := hs.el.snapshot(); len(deleted) != 1 || deleted[0] != "voice-1" || len(live) != 0 {
		t.Fatalf("deleted %v, live %v", deleted, live)
	}
	// No voice left: DELETE again is a no-op.
	wantStatus(t, hs.remove(), http.StatusNoContent)
}

func TestDeleteAlreadyGoneInElevenLabs(t *testing.T) {
	hs := newHarness(t)
	hs.store.put(devID, hs.dev.pub, "deleted-by-hand")
	wantStatus(t, hs.remove(), http.StatusNoContent)
	if got := hs.store.get(devID).VoiceID; got != "" {
		t.Fatalf("voice_id = %q, want cleared", got)
	}
}

func TestDeleteElevenLabsFailureChangesNothing(t *testing.T) {
	hs := newHarness(t)
	hs.el.addVoice("kept")
	hs.store.put(devID, hs.dev.pub, "kept")
	hs.el.failDel = http.StatusInternalServerError
	wantStatus(t, hs.remove(), http.StatusBadGateway)
	if got := hs.store.get(devID).VoiceID; got != "kept" {
		t.Fatalf("voice_id = %q, want kept", got)
	}
}

func TestDeleteNeedsSignature(t *testing.T) {
	hs := newHarness(t)
	hs.store.put(devID, hs.dev.pub, "kept")
	r := httptest.NewRequest(http.MethodDelete, "/devices/"+devID+"/voice", nil)
	wantStatus(t, hs.serve(r), http.StatusUnauthorized)
	// A POST signature is not a DELETE signature.
	r = httptest.NewRequest(http.MethodDelete, "/devices/"+devID+"/voice", nil)
	sum := sha256.Sum256(nil)
	hs.dev.sign(r, testNow, sum[:])
	wantStatus(t, hs.serve(r), http.StatusUnauthorized)
	if got := hs.store.get(devID).VoiceID; got != "kept" {
		t.Fatalf("voice_id = %q, want kept", got)
	}
}

func TestPreview(t *testing.T) {
	hs := newHarness(t)
	get := func() *httptest.ResponseRecorder {
		return hs.serve(httptest.NewRequest(http.MethodGet, "/devices/"+devID+"/voice/preview", nil))
	}
	wantStatus(t, get(), http.StatusNotFound) // no voice yet

	hs.store.put(devID, hs.dev.pub, "voice-x")
	for range 2 {
		rec := get()
		wantStatus(t, rec, http.StatusOK)
		if rec.Header().Get("Content-Type") != "audio/mpeg" || rec.Body.String() != "ID3fake-mp3-voice-x" {
			t.Fatalf("preview: %s %q", rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
	hs.el.mu.Lock()
	spoken := hs.el.spoken
	hs.el.mu.Unlock()
	if len(spoken) != 1 {
		t.Fatalf("ElevenLabs spoke %d times, want 1 (cached)", len(spoken))
	}
	s := spoken[0]
	if s.Body["text"] != PreviewText || s.Body["model_id"] != "eleven_flash_v2_5" || s.Query != "output_format=mp3_44100_128" {
		t.Fatalf("TTS request: %+v", s)
	}

	hs.el.failTTS = http.StatusInternalServerError
	hs.store.put(devID, hs.dev.pub, "voice-y")
	wantStatus(t, get(), http.StatusBadGateway)
}

func TestNotConfigured(t *testing.T) {
	h := NewHandler(nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)
	for _, r := range []*http.Request{
		uploadRequest(t, devID, fakeMP3("x"), "true"),
		httptest.NewRequest(http.MethodDelete, "/devices/"+devID+"/voice", nil),
		httptest.NewRequest(http.MethodGet, "/devices/"+devID+"/voice/preview", nil),
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		wantStatus(t, rec, http.StatusServiceUnavailable)
	}
}

func TestSniff(t *testing.T) {
	cases := map[string]string{
		"ID3\x04rest":                  "mp3",
		"\xff\xfb\x90\x00":             "mp3",
		"RIFF\x00\x00\x00\x00WAVEfmt ": "wav",
		"\x00\x00\x00\x20ftypM4A \x00": "m4a",
		"OggS\x00\x02":                 "ogg",
		"\x1a\x45\xdf\xa3\x01":         "webm",
		"\xff\xf1\x50\x80":             "", // ADTS AAC, not mp3
		"%PDF-1.7":                     "",
		"":                             "",
	}
	for in, want := range cases {
		f, ok := Sniff([]byte(in))
		if f.Ext != want || ok != (want != "") {
			t.Errorf("Sniff(%q) = %q, %v; want %q", in, f.Ext, ok, want)
		}
	}
}

func TestSignedMessage(t *testing.T) {
	if got, want := string(SignedMessage("DELETE", "abc", 42, nil)), "MAMA-device-voice-v1 DELETE abc 42"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := string(SignedMessage("POST", "abc", 42, []byte{0xAB, 0x01})), "MAMA-device-voice-v1 POST abc 42 ab01"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClientRefusedKey(t *testing.T) {
	el := newFakeElevenLabs(t)
	c := &Client{BaseURL: el.srv.URL, APIKey: "wrong"}
	_, err := c.Add(t.Context(), NewVoice{Name: "x"}, Sample{Audio: fakeMP3("k"), Format: formatMP3})
	if err == nil || !strings.Contains(err.Error(), "Voices write permission") {
		t.Fatalf("err = %v, want it to name the Voices write permission", err)
	}
}
