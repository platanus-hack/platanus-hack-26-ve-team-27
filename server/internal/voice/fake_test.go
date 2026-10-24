package voice

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeElevenLabs is an httptest ElevenLabs with the three endpoints the
// package uses. It keeps the voices it created and what it was sent.
type fakeElevenLabs struct {
	srv *httptest.Server

	mu       sync.Mutex
	next     int
	voices   map[string]bool // live voice ids
	added    []addedVoice
	deleted  []string
	spoken   []spokenText
	failAdd  int // status to answer POST /v1/voices/add with; 0 = success
	failDel  int // status to answer DELETE with; 0 = success
	failTTS  int
	lastBody []byte
}

type addedVoice struct {
	Name, Description, RemoveNoise, Filename, ContentType string
	Audio                                                 []byte
}

type spokenText struct {
	VoiceID, Query string
	Body           map[string]any
}

func newFakeElevenLabs(t *testing.T) *fakeElevenLabs {
	t.Helper()
	f := &fakeElevenLabs{voices: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/voices/add", f.add)
	mux.HandleFunc("DELETE /v1/voices/{id}", f.del)
	mux.HandleFunc("POST /v1/text-to-speech/{id}", f.tts)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != "test-key" {
			http.Error(w, `{"detail":"invalid api key"}`, http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeElevenLabs) client() *Client {
	return &Client{BaseURL: f.srv.URL, APIKey: "test-key"}
}

// addVoice makes a voice exist, as if created before the test.
func (f *fakeElevenLabs) addVoice(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.voices[id] = true
}

func (f *fakeElevenLabs) add(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAdd != 0 {
		http.Error(w, `{"detail":{"status":"voice_limit_reached"}}`, f.failAdd)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	var v addedVoice
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		b, _ := io.ReadAll(p)
		switch p.FormName() {
		case "name":
			v.Name = string(b)
		case "description":
			v.Description = string(b)
		case "remove_background_noise":
			v.RemoveNoise = string(b)
		case "files":
			v.Audio, v.Filename, v.ContentType = b, p.FileName(), p.Header.Get("Content-Type")
		}
	}
	if v.Name == "" || v.Audio == nil {
		http.Error(w, `{"detail":"name and files are required"}`, http.StatusUnprocessableEntity)
		return
	}
	f.next++
	id := fmt.Sprintf("voice-%d", f.next)
	f.voices[id] = true
	f.added = append(f.added, v)
	_ = json.NewEncoder(w).Encode(map[string]any{"voice_id": id, "requires_verification": false})
}

func (f *fakeElevenLabs) del(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := r.PathValue("id")
	f.deleted = append(f.deleted, id)
	if f.failDel != 0 {
		http.Error(w, `{"detail":"boom"}`, f.failDel)
		return
	}
	if !f.voices[id] {
		http.Error(w, `{"detail":"voice_not_found"}`, http.StatusNotFound)
		return
	}
	delete(f.voices, id)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (f *fakeElevenLabs) tts(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTTS != 0 {
		http.Error(w, `{"detail":"boom"}`, f.failTTS)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.spoken = append(f.spoken, spokenText{VoiceID: r.PathValue("id"), Query: r.URL.RawQuery, Body: body})
	w.Header().Set("Content-Type", "audio/mpeg")
	_, _ = w.Write([]byte("ID3fake-mp3-" + r.PathValue("id")))
}

func (f *fakeElevenLabs) snapshot() (added []addedVoice, deleted []string, live map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	live = map[string]bool{}
	for k, v := range f.voices {
		live[k] = v
	}
	return append([]addedVoice(nil), f.added...), append([]string(nil), f.deleted...), live
}

// memStore is the Store in memory.
type memStore struct {
	mu      sync.Mutex
	devices map[string]*memDevice
}

type memDevice struct {
	Device
	consentAt time.Time
	sha       []byte
}

func newMemStore() *memStore { return &memStore{devices: map[string]*memDevice{}} }

func (m *memStore) put(id string, key ed25519.PublicKey, voiceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[id] = &memDevice{Device: Device{PublicKey: key, VoiceID: voiceID}}
}

func (m *memStore) get(id string) memDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.devices[id]
}

func (m *memStore) Device(_ context.Context, id string) (Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return Device{}, ErrUnknownDevice
	}
	return d.Device, nil
}

func (m *memStore) SetVoice(_ context.Context, id, voiceID string, at time.Time, sha []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return "", ErrUnknownDevice
	}
	old := d.VoiceID
	d.VoiceID, d.consentAt, d.sha = voiceID, at, sha
	return old, nil
}

func (m *memStore) ClearVoice(_ context.Context, id, voiceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.devices[id]; ok && d.VoiceID == voiceID {
		d.VoiceID, d.consentAt, d.sha = "", time.Time{}, nil
	}
	return nil
}

// testDevice is a device id and its key pair.
type testDevice struct {
	id   string
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newTestDevice(t *testing.T, id string) testDevice {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return testDevice{id: id, pub: pub, priv: priv}
}

// sign sets the signature headers on r, by d's key, at signedAt.
func (d testDevice) sign(r *http.Request, signedAt time.Time, audioSHA256 []byte) {
	at := signedAt.Unix()
	sig := ed25519.Sign(d.priv, SignedMessage(r.Method, d.id, at, audioSHA256))
	r.Header.Set(HeaderSignedAt, fmt.Sprint(at))
	r.Header.Set(HeaderSignature, base64.StdEncoding.EncodeToString(sig))
}

// uploadRequest builds POST /devices/{id}/voice with the given form fields;
// an empty consent leaves the field out.
func uploadRequest(t *testing.T, id string, audio []byte, consent string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if consent != "" {
		_ = mw.WriteField("consent", consent)
	}
	if audio != nil {
		fw, err := mw.CreateFormFile("audio", "mama.mp3")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(audio)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/devices/"+id+"/voice", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

// fakeMP3 is a tiny file that sniffs as MP3 and carries a marker, so a test
// can look for the raw audio anywhere it might have been kept.
func fakeMP3(marker string) []byte {
	return []byte("ID3\x04\x00\x00\x00\x00\x00\x00" + marker + strings.Repeat("\x00\xff", 256))
}
