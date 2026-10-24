package report

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"regexp"
)

// tokenBytes is the link secret's size: 128 random bits.
const tokenBytes = 16

// tokenRE matches a token: 16 bytes in base64url without padding.
var tokenRE = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

// NewToken returns a fresh link token: 128 bits from r (crypto/rand when nil)
// in base64url without padding, 22 characters. It is unrelated to the
// incident_id, which the phone made and which travels in other messages.
func NewToken(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, tokenBytes)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidToken reports whether s has the shape of a token, so a malformed one
// is refused before any database lookup.
func ValidToken(s string) bool { return tokenRE.MatchString(s) }
