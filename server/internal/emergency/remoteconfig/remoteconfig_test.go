package remoteconfig

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
)

func newKeys(t *testing.T) serverkeys.Keys {
	t.Helper()
	k, err := serverkeys.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func do(t *testing.T, number string, keys serverkeys.Keys, method string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	New(number, keys).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, "/config", nil))
	return rec
}

func TestServesConfig(t *testing.T) {
	keys := newKeys(t)
	rec := do(t, "+15555550100", keys, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	var got contract.RemoteConfig
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.SMSNumber != "+15555550100" ||
		!bytes.Equal(got.ServerEd25519PublicKey, keys.SigningPublic()) ||
		!bytes.Equal(got.ServerX25519PublicKey, keys.SealingPublic()) {
		t.Errorf("config = %+v", got)
	}
}

func TestOmitsUnsetNumber(t *testing.T) {
	rec := do(t, "", newKeys(t), http.MethodGet)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["sms_number"]; ok {
		t.Error("sms_number present, want omitted")
	}
	if len(raw) != 2 {
		t.Errorf("keys = %d, want 2", len(raw))
	}
}

func TestPostIs405(t *testing.T) {
	if rec := do(t, "", newKeys(t), http.MethodPost); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /config = %d, want 405", rec.Code)
	}
}

func TestCheckSMSNumber(t *testing.T) {
	for _, ok := range []string{"", "+15555550100", "+584141234567"} {
		if err := CheckSMSNumber(ok); err != nil {
			t.Errorf("CheckSMSNumber(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"5555550100", "+0123456", "+1 555 555 0100", "+12"} {
		if err := CheckSMSNumber(bad); err == nil {
			t.Errorf("CheckSMSNumber(%q) = nil, want error", bad)
		}
	}
}
