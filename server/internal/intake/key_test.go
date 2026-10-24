package intake

import (
	"bytes"
	"crypto/ed25519"
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

// vectorKey is the private key behind vectorKeyB64 (payload testdata
// device_ed25519_seed), the key device_registration.json carries.
var vectorKey = func() ed25519.PrivateKey {
	seed, _ := hex.DecodeString("960d0a0436f8c133345291c51cb393064c5e47113c65f8a1b04a8bd4c38eda74")
	return ed25519.NewKeyFromSeed(seed)
}()

// signedReg is a registration of id carrying key's public half and its
// proof of possession.
func signedReg(id string, key ed25519.PrivateKey) contract.DeviceRegistration {
	return contract.DeviceRegistration{
		DeviceID:         id,
		PhoneNumber:      "+584141234567",
		DuressPhrase:     "p",
		Ed25519PublicKey: key.Public().(ed25519.PublicKey),
		Ed25519Signature: ed25519.Sign(key, contract.RegistrationSignedMessage(id)),
	}
}

func TestValidateDeviceKey(t *testing.T) {
	reg := contract.DeviceRegistration{DeviceID: "x", PhoneNumber: "+584141234567"}
	if err := validateDevice(reg); err != nil {
		t.Errorf("no key: %v", err)
	}
	if err := validateDevice(signedReg("x", vectorKey)); err != nil {
		t.Errorf("signed 32-byte key: %v", err)
	}
	for _, n := range []int{31, 33} {
		reg.Ed25519PublicKey = make([]byte, n)
		if err := validateDevice(reg); err == nil || !strings.Contains(err.Error(), "ed25519_public_key") {
			t.Errorf("%d-byte key: err = %v", n, err)
		}
	}
}

func TestValidateDeviceKeyProof(t *testing.T) {
	good := signedReg("x", vectorKey)
	other := signedReg("y", vectorKey)
	flipped := bytes.Clone(good.Ed25519Signature)
	flipped[0] ^= 1
	cases := map[string]contract.DeviceRegistration{
		"key without signature":        {DeviceID: "x", PhoneNumber: "+1", Ed25519PublicKey: good.Ed25519PublicKey},
		"signature without key":        {DeviceID: "x", PhoneNumber: "+1", Ed25519Signature: good.Ed25519Signature},
		"bad signature":                {DeviceID: "x", PhoneNumber: "+1", Ed25519PublicKey: good.Ed25519PublicKey, Ed25519Signature: flipped},
		"signature of another device":  {DeviceID: "x", PhoneNumber: "+1", Ed25519PublicKey: good.Ed25519PublicKey, Ed25519Signature: other.Ed25519Signature},
		"signature 63 bytes":           {DeviceID: "x", PhoneNumber: "+1", Ed25519PublicKey: good.Ed25519PublicKey, Ed25519Signature: good.Ed25519Signature[:63]},
		"signature by another key":     {DeviceID: "x", PhoneNumber: "+1", Ed25519PublicKey: good.Ed25519PublicKey, Ed25519Signature: signedReg("x", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))).Ed25519Signature},
		"context missing from message": {DeviceID: "x", PhoneNumber: "+1", Ed25519PublicKey: good.Ed25519PublicKey, Ed25519Signature: ed25519.Sign(vectorKey, []byte("x"))},
	}
	for name, reg := range cases {
		if err := validateDevice(reg); err == nil || !strings.Contains(err.Error(), "ed25519_signature") {
			t.Errorf("%s: err = %v, want an ed25519_signature error", name, err)
		}
	}
}

// A malformed key or a missing or wrong proof of possession is a 400 before
// any query, so a lazy pool is enough.
func TestMalformedKeyIs400WithoutDB(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://invalid.invalid/none")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mux := http.NewServeMux()
	New(pool, LogHandoff{}, LogHandoff{}).Register(mux)

	b64 := base64.StdEncoding.EncodeToString
	good := signedReg("x", vectorKey)
	key, sig := b64(good.Ed25519PublicKey), b64(good.Ed25519Signature)
	otherSig := b64(signedReg("y", vectorKey).Ed25519Signature)
	body := func(fields string) string {
		return `{"device_id":"x","phone_number":"+584141234567","family_numbers":[],"duress_phrase":"p"` + fields + `}`
	}
	cases := map[string]string{
		"key not Base64":              body(`,"ed25519_public_key":"!!!","ed25519_signature":"` + sig + `"`),
		"key 31 bytes":                body(`,"ed25519_public_key":"` + b64(make([]byte, 31)) + `","ed25519_signature":"` + sig + `"`),
		"key without signature":       body(`,"ed25519_public_key":"` + key + `"`),
		"signature without key":       body(`,"ed25519_signature":"` + sig + `"`),
		"signature not Base64":        body(`,"ed25519_public_key":"` + key + `","ed25519_signature":"!!!"`),
		"bad signature":               body(`,"ed25519_public_key":"` + key + `","ed25519_signature":"` + b64(make([]byte, 64)) + `"`),
		"signature of another device": body(`,"ed25519_public_key":"` + key + `","ed25519_signature":"` + otherSig + `"`),
	}
	for name, b := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(b)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d %s, want 400", name, rec.Code, rec.Body)
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

// newEnv registers device_registration.json, whose key comes with a valid
// signature: the key is stored with its key_id.
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
	delete(m, "ed25519_signature")
	body, _ := json.Marshal(m)
	if rec := e.post("/devices", body); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if _, kid := e.keyOf(deviceID); kid != "6e664c27c4356d73" {
		t.Fatalf("key_id = %q after re-registration without a key", kid)
	}
}

func TestDeviceKeyRegistration(t *testing.T) {
	onesKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	ones := onesKey.Public().(ed25519.PublicKey)
	reg := func(id string, key ed25519.PrivateKey) []byte {
		raw, _ := json.Marshal(signedReg(id, key))
		return raw
	}
	const other = "11111111-1111-4111-8111-111111111111"

	t.Run("same key again", func(t *testing.T) {
		e := newEnv(t)
		if rec := e.post("/devices", sample(t, "device_registration.json")); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
	})
	t.Run("new key replaces", func(t *testing.T) {
		e := newEnv(t)
		if rec := e.post("/devices", reg(deviceID, onesKey)); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		key, kid := e.keyOf(deviceID)
		if !bytes.Equal(key, ones) || kid != hex.EncodeToString(devicekeys.KeyID(ones)) {
			t.Fatalf("key = %x, key_id = %s", key, kid)
		}
	})
	t.Run("key of another device", func(t *testing.T) {
		e := newEnv(t)
		// A correctly signed registration still cannot move a key that
		// another device_id holds.
		if rec := e.post("/devices", reg(other, vectorKey)); rec.Code != http.StatusConflict {
			t.Fatalf("status = %d %s, want 409", rec.Code, rec.Body)
		}
		if n := e.count(`SELECT count(*) FROM devices WHERE id = $1`, other); n != 0 {
			t.Fatalf("device row created: %d", n)
		}
	})
	t.Run("bad signature stores nothing", func(t *testing.T) {
		e := newEnv(t)
		r := signedReg(other, onesKey)
		r.Ed25519Signature = signedReg(deviceID, onesKey).Ed25519Signature
		raw, _ := json.Marshal(r)
		if rec := e.post("/devices", raw); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d %s, want 400", rec.Code, rec.Body)
		}
		if n := e.count(`SELECT count(*) FROM devices WHERE id = $1`, other); n != 0 {
			t.Fatalf("device row created: %d", n)
		}
	})
}
