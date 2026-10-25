package report

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestNewTokenIs128BitsOfBase64URL(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok, err := NewToken(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != 22 || !ValidToken(tok) {
			t.Fatalf("token %q: want 22 base64url chars", tok)
		}
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil || len(raw) != 16 {
			t.Fatalf("token %q decodes to %d bytes (%v), want 16", tok, len(raw), err)
		}
		if seen[tok] {
			t.Fatalf("token %q repeated", tok)
		}
		seen[tok] = true
	}
}

func TestNewTokenReadsItsBitsFromTheReader(t *testing.T) {
	tok, err := NewToken(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	if err != nil || tok != "_____________________w" {
		t.Fatalf("NewToken(0xff...) = %q, %v", tok, err)
	}
	if _, err := NewToken(bytes.NewReader(make([]byte, 15))); err == nil {
		t.Fatal("NewToken with 15 bytes of randomness: want an error")
	}
}

func TestValidToken(t *testing.T) {
	for tok, want := range map[string]bool{
		"AAAAAAAAAAAAAAAAAAAAAA":               true,
		"abc-_DEF0123456789xyzQ":               true,
		"AAAAAAAAAAAAAAAAAAAAA":                false, // 21
		"AAAAAAAAAAAAAAAAAAAAAAA":              false, // 23
		"AAAAAAAAAAAAAAAAAAAAA=":               false, // padding
		"AAAAAAAAAAAAAAAAAAAA+/":               false, // std alphabet
		"9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f": false, // an incident_id is not a token
		"AAAAAAAAAAAAAAAAAAAAAA/audio.mp3":     false,
		"":                                     false,
	} {
		if got := ValidToken(tok); got != want {
			t.Errorf("ValidToken(%q) = %t, want %t", tok, got, want)
		}
	}
}
