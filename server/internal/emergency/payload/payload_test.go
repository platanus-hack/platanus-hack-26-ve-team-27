package payload

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

// testKey derives a deterministic Ed25519 key from a label.
func testKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func deviceKey() ed25519.PrivateKey { return testKey("mama-test-device-ed25519") }
func serverKey() ed25519.PrivateKey { return testKey("mama-test-server-ed25519") }

func pub(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

// rawBody builds a body with no validation, to feed ParseBody bad input.
func rawBody(version, kind byte, p Payload) []byte {
	b := make([]byte, BodySize)
	b[0], b[1] = version, kind
	binary.BigEndian.PutUint64(b[2:], p.DeviceID)
	binary.BigEndian.PutUint64(b[10:], p.EmergencyID)
	binary.BigEndian.PutUint16(b[18:], p.Seq)
	binary.BigEndian.PutUint32(b[20:], uint32(p.LatE5))
	binary.BigEndian.PutUint32(b[24:], uint32(p.LonE5))
	binary.BigEndian.PutUint16(b[28:], p.LocAgeS)
	binary.BigEndian.PutUint32(b[30:], p.Timestamp)
	return b
}

func TestBodyRoundTrip(t *testing.T) {
	cases := map[string]Payload{
		"alert with fix": {Kind: KindAlert, DeviceID: 1, EmergencyID: 2, Seq: 0, LatE5: -3344889, LonE5: -7064000, LocAgeS: 12, Timestamp: 1_700_000_000},
		"cancel no fix":  {Kind: KindCancel, DeviceID: 1, EmergencyID: 2, Seq: 3, LocAgeS: NoLocation, Timestamp: 1_700_000_100},
		"extremes":       {Kind: KindUpdate, DeviceID: math.MaxUint64, EmergencyID: 0xfedcba9876543210, Seq: 65535, LatE5: -9000000, LonE5: 18000000, LocAgeS: 65534, Timestamp: math.MaxUint32},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := p.MarshalBody()
			if err != nil {
				t.Fatal(err)
			}
			if len(b) != BodySize {
				t.Fatalf("len = %d", len(b))
			}
			got, err := ParseBody(b)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, p) {
				t.Fatalf("got %+v, want %+v", got, p)
			}
		})
	}
}

func TestBodyLayout(t *testing.T) {
	p := Payload{Kind: KindAlert, DeviceID: 0x0102030405060708, EmergencyID: 0x1112131415161718, Seq: 0x2122, LatE5: -1, LonE5: 0x00313233, LocAgeS: 0x4142, Timestamp: 0x51525354}
	b, err := p.MarshalBody()
	if err != nil {
		t.Fatal(err)
	}
	want := "0101" + "0102030405060708" + "1112131415161718" + "2122" + "ffffffff" + "00313233" + "4142" + "51525354"
	if got := hex.EncodeToString(b); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestBodyRejects(t *testing.T) {
	ok := Payload{Kind: KindAlert, DeviceID: 1, EmergencyID: 2, LatE5: 1, LonE5: 1, LocAgeS: 5}
	with := func(f func(p *Payload)) Payload { p := ok; f(&p); return p }
	cases := []struct {
		name    string
		p       Payload
		version byte
	}{
		{"kind 0", with(func(p *Payload) { p.Kind = 0 }), 1},
		{"kind 4", with(func(p *Payload) { p.Kind = KindAck }), 1},
		{"kind 5", with(func(p *Payload) { p.Kind = 5 }), 1},
		{"version 0", ok, 0},
		{"version 2", ok, 2},
		{"lat high", with(func(p *Payload) { p.LatE5 = 9000001 }), 1},
		{"lat low", with(func(p *Payload) { p.LatE5 = -9000001 }), 1},
		{"lon high", with(func(p *Payload) { p.LonE5 = 18000001 }), 1},
		{"lon low", with(func(p *Payload) { p.LonE5 = -18000001 }), 1},
		{"no fix with coords", with(func(p *Payload) { p.LocAgeS = NoLocation }), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Struct form: MarshalBody always writes version 1, so the
			// version cases only apply to the byte form.
			if c.version == 1 {
				if _, err := c.p.MarshalBody(); !errors.Is(err, ErrFormat) {
					t.Fatalf("MarshalBody err = %v", err)
				}
			}
			if _, err := ParseBody(rawBody(c.version, byte(c.p.Kind), c.p)); !errors.Is(err, ErrFormat) {
				t.Fatalf("ParseBody err = %v", err)
			}
		})
	}
	good, _ := ok.MarshalBody()
	for _, n := range []int{33, 35} {
		b := append(append([]byte{}, good...), make([]byte, 2)...)[:n]
		if _, err := ParseBody(b); !errors.Is(err, ErrFormat) {
			t.Fatalf("len %d: err = %v", n, err)
		}
	}
}

func TestToE5(t *testing.T) {
	good := map[float64]int32{10.48801: 1048801, -66.87919: -6687919, -33.44889: -3344889, 0: 0}
	for in, want := range good {
		got, err := ToE5(in)
		if err != nil || got != want {
			t.Errorf("ToE5(%v) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 180.000001, -180.000001} {
		if _, err := ToE5(in); err == nil {
			t.Errorf("ToE5(%v) should fail", in)
		}
	}
}

func TestDeviceID(t *testing.T) {
	sum := sha256.Sum256(pub(deviceKey()))
	if got, want := DeviceID(pub(deviceKey())), binary.BigEndian.Uint64(sum[:8]); got != want {
		t.Fatalf("got %x, want %x", got, want)
	}
	if DeviceID(pub(deviceKey())) == DeviceID(pub(serverKey())) {
		t.Fatal("device and server ids must differ")
	}
}

func devicePayload() Payload {
	return Payload{Kind: KindAlert, DeviceID: DeviceID(pub(deviceKey())), EmergencyID: 0xabcdef, Seq: 1, LatE5: -3344889, LonE5: -7064000, LocAgeS: 3, Timestamp: 1_700_000_000}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	p := devicePayload()
	signed, err := Sign(p, deviceKey())
	if err != nil {
		t.Fatal(err)
	}
	if len(signed) != SignedSize {
		t.Fatalf("len = %d", len(signed))
	}
	body, _ := p.MarshalBody()
	if !reflect.DeepEqual(signed[:BodySize], body) {
		t.Fatal("signed does not start with the body")
	}
	got, err := Verify(signed, pub(deviceKey()))
	if err != nil || got != p {
		t.Fatalf("got %+v, %v", got, err)
	}
	msg := append([]byte(SignatureContext), body...)
	if !ed25519.Verify(pub(deviceKey()), msg, signed[BodySize:]) {
		t.Fatal("signature context not pinned")
	}
}

func TestVerifyRejects(t *testing.T) {
	p := devicePayload()
	signed, _ := Sign(p, deviceKey())
	flip := func(i int) []byte { b := append([]byte{}, signed...); b[i] ^= 1; return b }

	other := p
	other.DeviceID ^= 1
	otherBody, _ := other.MarshalBody()
	forged := append(otherBody, ed25519.Sign(deviceKey(), append([]byte(SignatureContext), otherBody...))...)

	v2 := rawBody(2, byte(p.Kind), p)
	v2Signed := append(v2, ed25519.Sign(deviceKey(), append([]byte(SignatureContext), v2...))...)

	sig := map[string]struct {
		b   []byte
		pub ed25519.PublicKey
	}{
		"last byte":      {flip(SignedSize - 1), pub(deviceKey())},
		"byte 33":        {flip(33), pub(deviceKey())},
		"server key":     {signed, pub(serverKey())},
		"device id xor1": {forged, pub(deviceKey())},
	}
	for name, c := range sig {
		if _, err := Verify(c.b, c.pub); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Verify(signed[:97], pub(deviceKey())); !errors.Is(err, ErrFormat) {
		t.Errorf("len 97: err = %v", err)
	}
	if _, err := Verify(v2Signed, pub(deviceKey())); !errors.Is(err, ErrFormat) {
		t.Errorf("version 2: err = %v", err)
	}
}

func TestVerifyBadPublicKeyLength(t *testing.T) {
	signed, _ := Sign(devicePayload(), deviceKey())
	for _, k := range []ed25519.PublicKey{nil, pub(deviceKey())[:31]} {
		if _, err := Verify(signed, k); !errors.Is(err, ErrSignature) {
			t.Errorf("len %d: err = %v", len(k), err)
		}
	}
}

func TestSignRejectsMismatchedDeviceID(t *testing.T) {
	if _, err := Sign(devicePayload(), serverKey()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodersNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewPCG(54, 54))
	var inputs [][]byte
	for n := 0; n <= 120; n++ {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rng.UintN(256))
		}
		inputs = append(inputs, b)
	}
	for i := 0; i < 200; i++ {
		b := make([]byte, rng.IntN(121))
		for j := range b {
			b[j] = byte(rng.UintN(256))
		}
		inputs = append(inputs, b)
	}
	known := func(err error) bool {
		return errors.Is(err, ErrFormat) || errors.Is(err, ErrSignature) || errors.Is(err, ErrDecrypt)
	}
	for _, b := range inputs {
		if _, err := ParseBody(b); !known(err) {
			t.Fatalf("ParseBody len %d: %v", len(b), err)
		}
		if _, err := ParseSigned(b); !known(err) {
			t.Fatalf("ParseSigned len %d: %v", len(b), err)
		}
		if _, err := Verify(b, pub(deviceKey())); !known(err) {
			t.Fatalf("Verify len %d: %v", len(b), err)
		}
		if _, err := VerifyAck(b, pub(serverKey())); !known(err) {
			t.Fatalf("VerifyAck len %d: %v", len(b), err)
		}
	}
}

func TestLocAgeOf(t *testing.T) {
	for _, c := range []struct {
		age  time.Duration
		ok   bool
		want uint16
	}{
		{0, false, NoLocation},
		{-time.Second, true, 0},
		{12 * time.Second, true, 12},
		{100000 * time.Second, true, 65534},
	} {
		if got := LocAgeOf(c.age, c.ok); got != c.want {
			t.Errorf("LocAgeOf(%v,%v)=%d want %d", c.age, c.ok, got, c.want)
		}
	}
}
