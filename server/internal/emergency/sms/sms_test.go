package sms

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
)

const testDeviceUUID = "11111111-2222-4333-8444-555555555555"

type vectorFile struct {
	Keys struct {
		DeviceEd25519Public string `json:"device_ed25519_public"`
		ServerEd25519Seed   string `json:"server_ed25519_seed"`
		ServerX25519Private string `json:"server_x25519_private"`
	} `json:"keys"`
	Payloads []struct {
		Name string `json:"name"`
		SMS  string `json:"sms"`
	} `json:"payloads"`
	Acks []struct {
		Name string `json:"name"`
		SMS  string `json:"sms"`
	} `json:"acks"`
	Invalid []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	} `json:"invalid"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("../payload/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (v vectorFile) payload(t *testing.T, name string) string {
	t.Helper()
	for _, p := range v.Payloads {
		if p.Name == name {
			return p.SMS
		}
	}
	t.Fatalf("no payload vector %q", name)
	return ""
}

func (v vectorFile) ack(t *testing.T, name string) string {
	t.Helper()
	for _, a := range v.Acks {
		if a.Name == name {
			return a.SMS
		}
	}
	t.Fatalf("no ack vector %q", name)
	return ""
}

func (v vectorFile) invalid(t *testing.T, name string) string {
	t.Helper()
	for _, i := range v.Invalid {
		if i.Name == name {
			return i.Input
		}
	}
	t.Fatalf("no invalid vector %q", name)
	return ""
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeStore is an in-memory ingest.Store. failWith makes every call fail.
type fakeStore struct {
	mu       sync.Mutex
	records  map[uint64]ingest.Emergency
	devices  map[uint64]devicekeys.Device
	failWith error
}

func (s *fakeStore) Device(_ context.Context, keyID uint64) (devicekeys.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWith != nil {
		return devicekeys.Device{}, s.failWith
	}
	d, ok := s.devices[keyID]
	if !ok {
		return devicekeys.Device{}, devicekeys.ErrNotFound
	}
	return d, nil
}

func (s *fakeStore) Update(_ context.Context, id uint64, apply func(*ingest.Emergency) (ingest.Emergency, error)) (ingest.Emergency, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev *ingest.Emergency
	if e, ok := s.records[id]; ok {
		prev = &e
	}
	next, err := apply(prev)
	if err != nil {
		return ingest.Emergency{}, err
	}
	s.records[id] = next
	return next, nil
}

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, ingest.EmergencyReceived) {}

// fakeProvider stands for any provider: the signature is the header
// X-Fake-Signature: ok, the form has from and body, and the reply is the text
// prefixed with "reply:".
type fakeProvider struct{}

func (fakeProvider) Parse(r *http.Request) (InboundSMS, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return InboundSMS{}, err
	}
	if r.Header.Get("X-Fake-Signature") != "ok" {
		return InboundSMS{}, ErrSignature
	}
	form, _ := url.ParseQuery(string(raw))
	if form.Get("from") == "" || form.Get("body") == "" {
		return InboundSMS{}, ErrFormat
	}
	return InboundSMS{From: form.Get("from"), Body: form.Get("body")}, nil
}

func (fakeProvider) Reply(w http.ResponseWriter, text string) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "reply:"+text)
}

type smsFixture struct {
	vec   vectorFile
	keys  serverkeys.Keys
	store *fakeStore
	mux   *http.ServeMux
}

func newSMSFixture(t *testing.T, registerDevice bool) *smsFixture {
	t.Helper()
	v := loadVectors(t)
	sealing, err := ecdh.X25519().NewPrivateKey(mustHex(t, v.Keys.ServerX25519Private))
	if err != nil {
		t.Fatal(err)
	}
	keys := serverkeys.Keys{Signing: ed25519.NewKeyFromSeed(mustHex(t, v.Keys.ServerEd25519Seed)), Sealing: sealing}
	store := &fakeStore{records: map[uint64]ingest.Emergency{}, devices: map[uint64]devicekeys.Device{}}
	if registerDevice {
		pub := ed25519.PublicKey(mustHex(t, v.Keys.DeviceEd25519Public))
		store.devices[payload.DeviceID(pub)] = devicekeys.Device{ID: testDeviceUUID, PublicKey: pub}
	}
	mux := http.NewServeMux()
	providers := map[string]InboundSMSProvider{"fake": fakeProvider{}, "twilio": mustTwilio(t)}
	NewHandler(ingest.New(store, keys, nopPublisher{}), providers).Register(mux)
	return &smsFixture{vec: v, keys: keys, store: store, mux: mux}
}

func (f *smsFixture) post(path, body, sig string) *httptest.ResponseRecorder {
	form := url.Values{"from": {testFrom}, "body": {body}}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	if sig != "" {
		r.Header.Set("X-Fake-Signature", sig)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, r)
	return rec
}

func TestInboundAcksAndDuplicateGetsSameAck(t *testing.T) {
	f := newSMSFixture(t, true)
	sms := f.vec.payload(t, "alert_caracas")
	want := "reply:" + f.vec.ack(t, "ack_alert")

	for _, try := range []string{"first", "resend"} {
		rec := f.post("/sms/inbound/fake", sms, "ok")
		if rec.Code != 200 || rec.Body.String() != want {
			t.Fatalf("%s = %d %q, want 200 %q", try, rec.Code, rec.Body.String(), want)
		}
		ack, err := payload.ParseSMSText(strings.TrimPrefix(rec.Body.String(), "reply:"))
		if err != nil {
			t.Fatal(err)
		}
		if a, err := payload.VerifyAck(ack, f.keys.SigningPublic()); err != nil || a.Seq != 0 {
			t.Fatalf("%s: ack %+v, %v", try, a, err)
		}
	}
	if len(f.store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(f.store.records))
	}
	for _, e := range f.store.records {
		if len(e.Channels) != 1 || e.Channels[0] != ingest.ChannelSMS {
			t.Fatalf("channels = %v, want [sms]", e.Channels)
		}
	}
}

func TestInboundStatusCodes(t *testing.T) {
	probe := loadVectors(t)
	sms := probe.payload(t, "alert_caracas")
	flipped := probe.invalid(t, "sms_flipped_signature")

	cases := []struct {
		name      string
		path      string
		body, sig string
		want      int
		wantReply string // "" = a JSON error, not a provider reply
		noDevice  bool
		conflict  bool
		failWith  error
	}{
		{"not a MAMA text", "/sms/inbound/fake", "hola, ¿cómo estás?", "ok", 200, "reply:", false, false, nil},
		{"bad payload signature", "/sms/inbound/fake", flipped, "ok", 200, "reply:", false, false, nil},
		{"unregistered device", "/sms/inbound/fake", sms, "ok", 200, "reply:", true, false, nil},
		{"conflict", "/sms/inbound/fake", sms, "ok", 200, "reply:", false, true, nil},
		{"bad webhook signature", "/sms/inbound/fake", sms, "forged", 403, "", false, false, nil},
		{"missing webhook signature", "/sms/inbound/fake", sms, "", 403, "", false, false, nil},
		{"signed without body", "/sms/inbound/fake", "", "ok", 400, "", false, false, nil},
		{"unknown provider", "/sms/inbound/nope", sms, "ok", 404, "", false, false, nil},
		{"too large", "/sms/inbound/fake", strings.Repeat("a", 20<<10), "ok", 413, "", false, false, nil},
		{"store down", "/sms/inbound/fake", sms, "ok", 500, "", false, false, errors.New("connection refused")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSMSFixture(t, !c.noDevice)
			f.store.failWith = c.failWith
			if c.conflict {
				f.store.records[0x0123456789abcdef] = ingest.Emergency{ID: 0x0123456789abcdef, DeviceID: "someone-else", Status: ingest.StatusOpen, Channels: []ingest.Channel{ingest.ChannelHTTP}}
			}
			rec := f.post(c.path, c.body, c.sig)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
			if c.wantReply != "" {
				if rec.Body.String() != c.wantReply {
					t.Fatalf("reply = %q, want %q", rec.Body.String(), c.wantReply)
				}
			} else {
				var e map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e["error"] == "" {
					t.Fatalf("body %q is not a JSON error", rec.Body.String())
				}
			}
			if !c.conflict && len(f.store.records) != 0 {
				t.Fatalf("records = %d, want nothing stored", len(f.store.records))
			}
		})
	}
}

func TestInboundNoDatabaseIs503(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(nil, map[string]InboundSMSProvider{"fake": fakeProvider{}}).Register(mux)
	for _, path := range []string{"/sms/inbound/fake", "/sms/inbound/nope"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("body=x")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("POST %s = %d, want 503", path, rec.Code)
		}
	}
}

func TestInboundGetNotAllowed(t *testing.T) {
	f := newSMSFixture(t, true)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sms/inbound/fake", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d, want 405", rec.Code)
	}
}

// End to end through the Twilio adapter: the signed sample webhook carries
// the alert_caracas vector and the TwiML reply carries its ack.
func TestTwilioWebhookEndToEnd(t *testing.T) {
	f := newSMSFixture(t, true)
	wantAck := f.vec.ack(t, "ack_alert")
	if testBody != f.vec.payload(t, "alert_caracas") {
		t.Fatal("testBody is not the alert_caracas vector")
	}

	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, twilioRequest(testPath, sampleWebhook(), testSignature))
	want := `<?xml version="1.0" encoding="UTF-8"?><Response><Message>` + wantAck + `</Message></Response>`
	if rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("webhook = %d %q, want 200 %q", rec.Code, rec.Body.String(), want)
	}
	if len(f.store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(f.store.records))
	}

	forged := httptest.NewRecorder()
	f.mux.ServeHTTP(forged, twilioRequest(testPath, sampleWebhook(), sign("other-token", testBaseURL+testPath, sampleWebhook())))
	if forged.Code != http.StatusForbidden {
		t.Fatalf("forged webhook = %d, want 403", forged.Code)
	}
}
