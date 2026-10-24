package payload

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"
)

func TestAckRoundTripAndLayout(t *testing.T) {
	a := Ack{EmergencyID: 0x0123456789abcdef, Seq: 7}
	b := SignAck(a, serverKey())
	if len(b) != AckSize {
		t.Fatalf("len = %d", len(b))
	}
	if got, want := hex.EncodeToString(b[:AckBodySize]), "01040123456789abcdef0007"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	got, err := VerifyAck(b, pub(serverKey()))
	if err != nil || got != a {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAckRejects(t *testing.T) {
	a := Ack{EmergencyID: 9, Seq: 2}
	b := SignAck(a, serverKey())
	flipped := append([]byte{}, b...)
	flipped[AckSize-1] ^= 1
	byDevice := SignAck(a, deviceKey())

	sigCases := map[string]struct {
		b   []byte
		pub ed25519.PublicKey
	}{
		"flipped":   {flipped, pub(serverKey())},
		"device pk": {b, pub(deviceKey())},
		"device sg": {byDevice, pub(serverKey())},
	}
	for name, c := range sigCases {
		if _, err := VerifyAck(c.b, c.pub); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	resign := func(i int, v byte) []byte {
		body := append([]byte{}, b[:AckBodySize]...)
		body[i] = v
		return append(body, ed25519.Sign(serverKey(), append([]byte(SignatureContext), body...))...)
	}
	fmtCases := map[string][]byte{
		"len 75":    b[:75],
		"version 2": resign(0, 2),
		"kind 1":    resign(1, 1),
	}
	for name, c := range fmtCases {
		if _, err := VerifyAck(c, pub(serverKey())); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
