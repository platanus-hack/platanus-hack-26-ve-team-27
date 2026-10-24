package payload

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestSMSText(t *testing.T) {
	signed := signedPayloadBytes(t)
	s := SMSText(signed)
	if len(s) != 137 || !strings.HasPrefix(s, "MAMA1 ") || strings.Contains(s, "=") {
		t.Fatalf("bad sms %q", s)
	}
	got, err := ParseSMSText(s)
	if err != nil || !bytes.Equal(got, signed) {
		t.Fatalf("got %x, %v", got, err)
	}
	ack := SignAck(Ack{EmergencyID: 1, Seq: 2}, serverKey())
	if a := SMSText(ack); len(a) != 108 {
		t.Fatalf("ack sms len %d", len(a))
	}
}

func TestParseSMSTextWhitespace(t *testing.T) {
	signed := signedPayloadBytes(t)
	s := SMSText(signed)
	if got, err := ParseSMSText("  " + s + "\r\n"); err != nil || !bytes.Equal(got, signed) {
		t.Fatalf("trimmed: %v", err)
	}
	bad := map[string]string{
		"padded":      "MAMA1 " + base64.URLEncoding.EncodeToString(signed[:97]),
		"newline":     s[:60] + "\n" + s[60:],
		"lowercase":   "mama1 " + s[6:],
		"mesh prefix": "MAMA1:" + s[6:],
		"empty b64":   "MAMA1 ",
		"plus":        "MAMA1 +" + s[7:],
	}
	for name, in := range bad {
		if _, err := ParseSMSText(in); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMeshTexts(t *testing.T) {
	box, _ := Seal(rand.Reader, serverX25519().PublicKey(), signedPayloadBytes(t))
	st := MeshSealedText(box)
	if len(st) != 207 || !strings.HasPrefix(st, "MAMA1:SEAL:") {
		t.Fatalf("sealed %d %q", len(st), st)
	}
	ack := SignAck(Ack{EmergencyID: 1, Seq: 2}, serverKey())
	at := MeshAckText(ack)
	if len(at) != 114 || !strings.HasPrefix(at, "MAMA1:ACK:") {
		t.Fatalf("ack %d %q", len(at), at)
	}
	f, err := ParseMeshText(st)
	if err != nil || f.Type != MeshSealed || !bytes.Equal(f.Bytes, box) {
		t.Fatalf("sealed parse: %+v %v", f, err)
	}
	f, err = ParseMeshText(" " + at + "\n")
	if err != nil || f.Type != MeshAck || !bytes.Equal(f.Bytes, ack) {
		t.Fatalf("ack parse: %+v %v", f, err)
	}
	if !IsMeshText(st) || !IsMeshText(at) || IsMeshText("MAMA1 x") || IsMeshText("hola") {
		t.Fatal("IsMeshText")
	}
}

func TestParseMeshTextRejects(t *testing.T) {
	box := bytes.Repeat([]byte{0xfb}, 146)
	std := base64.StdEncoding.EncodeToString(box)
	url := base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 38))
	if !strings.ContainsAny(url, "-_") {
		t.Fatal("test setup")
	}
	bad := map[string]string{
		"unknown tag": "MAMA1:XYZ:" + std,
		"no tag":      "MAMA1:",
		"unpadded":    "MAMA1:SEAL:" + strings.TrimRight(std, "="),
		"urlsafe":     "MAMA1:ACK:" + url,
		"newline":     "MAMA1:SEAL:" + std[:50] + "\n" + std[50:],
		"other":       "hola",
	}
	for name, in := range bad {
		if _, err := ParseMeshText(in); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
