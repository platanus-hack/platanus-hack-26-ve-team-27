package payload

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"
)

func serverX25519() *ecdh.PrivateKey {
	seed := sha256.Sum256([]byte("mama-test-server-x25519"))
	k, err := ecdh.X25519().NewPrivateKey(seed[:])
	if err != nil {
		panic(err)
	}
	return k
}

func signedPayloadBytes(t *testing.T) []byte {
	t.Helper()
	b, err := Sign(devicePayload(), deviceKey())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpenRoundTrip(t *testing.T) {
	pt := signedPayloadBytes(t)
	k := serverX25519()
	box, err := Seal(rand.Reader, k.PublicKey(), pt)
	if err != nil {
		t.Fatal(err)
	}
	if len(box) != 146 {
		t.Fatalf("len = %d", len(box))
	}
	got, err := Open(k, box)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatalf("got %x, %v", got, err)
	}
	box2, _ := Seal(rand.Reader, k.PublicKey(), pt)
	if bytes.Equal(box, box2) {
		t.Fatal("two seals are identical")
	}
}

func TestSealDeterministicWithFixedEphemeral(t *testing.T) {
	eph := bytes.Repeat([]byte{7}, 32)
	pt := signedPayloadBytes(t)
	pub := serverX25519().PublicKey()
	a, err := Seal(bytes.NewReader(eph), pub, pt)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Seal(bytes.NewReader(eph), pub, pt)
	if !bytes.Equal(a, b) {
		t.Fatal("not deterministic")
	}
	ek, _ := ecdh.X25519().NewPrivateKey(eph)
	if !bytes.Equal(a[:32], ek.PublicKey().Bytes()) {
		t.Fatal("first 32 bytes are not the ephemeral public key")
	}
}

func TestSealLayout(t *testing.T) {
	eph := bytes.Repeat([]byte{9}, 32)
	pt := []byte("hello sealed box")
	rk := serverX25519().PublicKey()
	ek, _ := ecdh.X25519().NewPrivateKey(eph)
	shared, err := ek.ECDH(rk)
	if err != nil {
		t.Fatal(err)
	}
	salt := append(ek.PublicKey().Bytes(), rk.Bytes()...)
	okm, err := hkdf.Key(sha256.New, shared, salt, SealedBoxInfo, 44)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(okm[:32])
	gcm, _ := cipher.NewGCM(block)
	want := append(ek.PublicKey().Bytes(), gcm.Seal(nil, okm[32:44], pt, nil)...)
	got, err := Seal(bytes.NewReader(eph), rk, pt)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("got %x\nwant %x (%v)", got, want, err)
	}
}

func TestOpenRejects(t *testing.T) {
	k := serverX25519()
	box, _ := Seal(rand.Reader, k.PublicKey(), signedPayloadBytes(t))
	flip := func(i int) []byte {
		c := append([]byte{}, box...)
		c[i] ^= 1
		return c
	}
	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	zeroEph := append(make([]byte, 32), box[32:]...)
	cases := []struct {
		name string
		key  *ecdh.PrivateKey
		box  []byte
		want error
	}{
		{"ciphertext flipped", k, flip(40), ErrDecrypt},
		{"tag flipped", k, flip(len(box) - 1), ErrDecrypt},
		{"other key", other, box, ErrDecrypt},
		{"zero ephemeral", k, zeroEph, ErrDecrypt},
		{"empty", k, nil, ErrFormat},
		{"47 bytes", k, box[:47], ErrFormat},
	}
	for _, c := range cases {
		if _, err := Open(c.key, c.box); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	empty, err := Seal(rand.Reader, k.PublicKey(), nil)
	if err != nil || len(empty) != 48 {
		t.Fatalf("seal empty: %d, %v", len(empty), err)
	}
	got, err := Open(k, empty)
	if err != nil || len(got) != 0 {
		t.Fatalf("open empty: %x, %v", got, err)
	}
}

func TestOpenRejectsBadRecipient(t *testing.T) {
	k := serverX25519()
	box, _ := Seal(rand.Reader, k.PublicKey(), []byte("x"))
	p, _ := ecdh.P256().GenerateKey(rand.Reader)
	for name, key := range map[string]*ecdh.PrivateKey{"nil": nil, "wrong curve": p} {
		if _, err := Open(key, box); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestSealRejectsWrongCurve(t *testing.T) {
	p, _ := ecdh.P256().GenerateKey(rand.Reader)
	if _, err := Seal(rand.Reader, p.PublicKey(), []byte("x")); err == nil {
		t.Fatal("want error")
	}
}
