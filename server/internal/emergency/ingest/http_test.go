package ingest

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
)

type vectorFile struct {
	Keys struct {
		DeviceEd25519Public string `json:"device_ed25519_public"`
		ServerEd25519Seed   string `json:"server_ed25519_seed"`
		ServerX25519Private string `json:"server_x25519_private"`
	} `json:"keys"`
	Payloads []struct {
		Name string `json:"name"`
		SMS  string `json:"sms"`
		Mesh string `json:"mesh"`
	} `json:"payloads"`
	Acks []struct {
		Name string `json:"name"`
		Mesh string `json:"mesh"`
	} `json:"acks"`
	Invalid []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	} `json:"invalid"`
}

type httpFixture struct {
	vec   vectorFile
	store *fakeStore
	pub   *fakePublisher
	mux   *http.ServeMux
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newHTTPFixture(t *testing.T, registerDevice bool) *httpFixture {
	t.Helper()
	raw, err := os.ReadFile("../payload/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	sealing, err := ecdh.X25519().NewPrivateKey(mustHex(t, v.Keys.ServerX25519Private))
	if err != nil {
		t.Fatal(err)
	}
	keys := serverkeys.Keys{Signing: ed25519.NewKeyFromSeed(mustHex(t, v.Keys.ServerEd25519Seed)), Sealing: sealing}
	store := newFakeStore()
	if registerDevice {
		store.addDevice(svcDeviceUUID, ed25519.PublicKey(mustHex(t, v.Keys.DeviceEd25519Public)))
	}
	pub := &fakePublisher{}
	svc := New(store, keys, pub)
	svc.spawn = func(f func()) { f() }
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return &httpFixture{vec: v, store: store, pub: pub, mux: mux}
}

func (f *httpFixture) payload(t *testing.T, name string) (sms, mesh string) {
	t.Helper()
	for _, p := range f.vec.Payloads {
		if p.Name == name {
			return p.SMS, p.Mesh
		}
	}
	t.Fatalf("no payload vector %q", name)
	return
}

func (f *httpFixture) ack(t *testing.T, name string) string {
	t.Helper()
	for _, a := range f.vec.Acks {
		if a.Name == name {
			return a.Mesh
		}
	}
	t.Fatalf("no ack vector %q", name)
	return ""
}

func (f *httpFixture) invalid(t *testing.T, name string) string {
	t.Helper()
	for _, i := range f.vec.Invalid {
		if i.Name == name {
			return i.Input
		}
	}
	t.Fatalf("no invalid vector %q", name)
	return ""
}

func (f *httpFixture) do(method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func relayBody(text, extra string) string {
	b, _ := json.Marshal(text)
	return `{"text":` + string(b) + extra + `}`
}

func TestVectorsGiveVectorAck(t *testing.T) {
	f := newHTTPFixture(t, true)
	sms, mesh := f.payload(t, "alert_caracas")
	want := f.ack(t, "ack_alert")

	rec := f.do(http.MethodPost, "/emergencies/relay", relayBody(mesh, `,"hops":2`))
	if rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("relay = %d %q, want 200 %q", rec.Code, rec.Body.String(), want)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	rec = f.do(http.MethodPost, "/emergencies", sms)
	if rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("http = %d %q, want 200 %q", rec.Code, rec.Body.String(), want)
	}
	if len(f.store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(f.store.records))
	}
	for _, e := range f.store.records {
		if len(e.Channels) != 2 || e.Channels[0] != ChannelMesh || e.Channels[1] != ChannelHTTP {
			t.Fatalf("channels = %v, want [mesh http]", e.Channels)
		}
	}
}

func TestStatusCodes(t *testing.T) {
	probe := newHTTPFixture(t, true)
	sms, mesh := probe.payload(t, "alert_caracas")
	flipped := probe.invalid(t, "sms_flipped_signature")
	big := strings.Repeat("a", 2048)

	cases := []struct {
		name         string
		method, path string
		body         string
		want         int
		noDevice     bool
		conflict     bool
	}{
		{"bad signature", "POST", "/emergencies", flipped, 403, false, false},
		{"unregistered device", "POST", "/emergencies", sms, 422, true, false},
		{"garbage sms", "POST", "/emergencies", "MAMA1 !!!", 400, false, false},
		{"relay bad text", "POST", "/emergencies/relay", `{"text":"x"}`, 400, false, false},
		{"relay unknown field", "POST", "/emergencies/relay", relayBody(mesh, `,"extra":1`), 400, false, false},
		{"relay hops too big", "POST", "/emergencies/relay", relayBody(mesh, `,"hops":256`), 400, false, false},
		{"relay negative hops", "POST", "/emergencies/relay", relayBody(mesh, `,"hops":-1`), 400, false, false},
		{"relay bad json", "POST", "/emergencies/relay", `{`, 400, false, false},
		{"relay trailing data", "POST", "/emergencies/relay", relayBody(mesh, ``) + `{}`, 400, false, false},
		{"conflict", "POST", "/emergencies", sms, 409, false, true},
		{"too large http", "POST", "/emergencies", big, 413, false, false},
		{"too large relay", "POST", "/emergencies/relay", relayBody(big, ``), 413, false, false},
		{"get not allowed", "GET", "/emergencies", "", 405, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHTTPFixture(t, !c.noDevice)
			if c.conflict {
				f.store.records[0x0123456789abcdef] = Emergency{ID: 0x0123456789abcdef, DeviceID: "someone-else", Status: StatusOpen, Channels: []Channel{ChannelSMS}}
			}
			rec := f.do(c.method, c.path, c.body)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
			if c.want == 405 {
				return
			}
			var e map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e["error"] == "" {
				t.Fatalf("body %q is not a JSON error", rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q", ct)
			}
		})
	}
}

func TestRelayRejectsNonSealTexts(t *testing.T) {
	f := newHTTPFixture(t, true)
	sms, _ := f.payload(t, "alert_caracas")
	for name, text := range map[string]string{"ack": f.ack(t, "ack_alert"), "sms": sms} {
		rec := f.do(http.MethodPost, "/emergencies/relay", relayBody(text, ``))
		if rec.Code != 400 {
			t.Fatalf("%s: status = %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestRelayWithoutHopsIsUnknown(t *testing.T) {
	f := newHTTPFixture(t, true)
	_, mesh := f.payload(t, "alert_caracas")
	rec := f.do(http.MethodPost, "/emergencies/relay", relayBody(mesh, ``))
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	evs := f.pub.all()
	if len(evs) != 1 || evs[0].Hops != HopsUnknown {
		t.Fatalf("events = %+v, want one with HopsUnknown", evs)
	}
}

func TestNoDatabaseIs503(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(nil).Register(mux)
	for _, path := range []string{"/emergencies", "/emergencies/relay"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("MAMA1 x")))
		if rec.Code != 503 {
			t.Fatalf("%s = %d, want 503", path, rec.Code)
		}
	}
}
