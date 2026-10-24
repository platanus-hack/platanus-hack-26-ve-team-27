package intake

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
)

const vectorKeyB64 = "Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo="

func TestValidateDeviceKey(t *testing.T) {
	reg := contract.DeviceRegistration{DeviceID: "x", PhoneNumber: "+584141234567"}
	if err := validateDevice(reg); err != nil {
		t.Errorf("no key: %v", err)
	}
	reg.Ed25519PublicKey = make([]byte, 32)
	if err := validateDevice(reg); err != nil {
		t.Errorf("32-byte key: %v", err)
	}
	for _, n := range []int{31, 33} {
		reg.Ed25519PublicKey = make([]byte, n)
		if err := validateDevice(reg); err == nil || !strings.Contains(err.Error(), "ed25519_public_key") {
			t.Errorf("%d-byte key: err = %v", n, err)
		}
	}
}

// A malformed key is a 400 before any query, so a lazy pool is enough.
func TestMalformedKeyIs400WithoutDB(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://invalid.invalid/none")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mux := http.NewServeMux()
	New(pool, LogHandoff{}, LogHandoff{}).Register(mux)
	keys := map[string]string{"not Base64": "!!!", "31 bytes": base64.StdEncoding.EncodeToString(make([]byte, 31))}
	for name, key := range keys {
		body := `{"device_id":"x","phone_number":"+584141234567","family_numbers":[],"duress_phrase":"p","ed25519_public_key":"` + key + `"}`
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
}

func (e *env) keyOf(id string) (key []byte, keyID string) {
	e.t.Helper()
	var kid sql.NullString
	if err := e.pool.QueryRow(`SELECT ed25519_public_key, encode(key_id, 'hex') FROM devices WHERE id = $1`, id).Scan(&key, &kid); err != nil {
		e.t.Fatal(err)
	}
	return key, kid.String
}

func TestDeviceRegistrationStoresKey(t *testing.T) {
	e := newEnv(t)
	key, kid := e.keyOf(deviceID)
	want, _ := base64.StdEncoding.DecodeString(vectorKeyB64)
	if !bytes.Equal(key, want) || kid != "6e664c27c4356d73" {
		t.Fatalf("key = %x, key_id = %s", key, kid)
	}
}

func TestReRegistrationWithoutKeyKeepsKey(t *testing.T) {
	e := newEnv(t)
	var m map[string]any
	if err := json.Unmarshal(sample(t, "device_registration.json"), &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "ed25519_public_key")
	body, _ := json.Marshal(m)
	if rec := e.post("/devices", body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if _, kid := e.keyOf(deviceID); kid != "6e664c27c4356d73" {
		t.Fatalf("key_id = %q after re-registration without a key", kid)
	}
}

func TestDeviceKeyRegistration(t *testing.T) {
	ones := bytes.Repeat([]byte{1}, 32)
	reg := func(id string, key []byte) []byte {
		raw, _ := json.Marshal(contract.DeviceRegistration{DeviceID: id, PhoneNumber: "+584141234567", DuressPhrase: "p", Ed25519PublicKey: key})
		return raw
	}
	const other = "11111111-1111-4111-8111-111111111111"
	vector, _ := base64.StdEncoding.DecodeString(vectorKeyB64)

	t.Run("same key again", func(t *testing.T) {
		e := newEnv(t)
		if rec := e.post("/devices", sample(t, "device_registration.json")); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
	})
	t.Run("new key replaces", func(t *testing.T) {
		e := newEnv(t)
		if rec := e.post("/devices", reg(deviceID, ones)); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		key, kid := e.keyOf(deviceID)
		if !bytes.Equal(key, ones) || kid != hex.EncodeToString(devicekeys.KeyID(ones)) {
			t.Fatalf("key = %x, key_id = %s", key, kid)
		}
	})
	t.Run("key of another device", func(t *testing.T) {
		e := newEnv(t)
		if rec := e.post("/devices", reg(other, vector)); rec.Code != http.StatusConflict {
			t.Fatalf("status = %d %s, want 409", rec.Code, rec.Body)
		}
		if n := e.count(`SELECT count(*) FROM devices WHERE id = $1`, other); n != 0 {
			t.Fatalf("device row created: %d", n)
		}
	})
}
