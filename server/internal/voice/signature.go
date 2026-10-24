package voice

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

// The signature headers of POST and DELETE /devices/{id}/voice.
const (
	HeaderSignedAt  = "X-Mama-Signed-At" // unix seconds, decimal
	HeaderSignature = "X-Mama-Signature" // std Base64 of the 64-byte Ed25519 signature
)

// signatureContext keeps a voice signature from being valid as any other
// MAMÁ signature (the registration one is MAMA-device-registration-v1).
const signatureContext = "MAMA-device-voice-v1"

// MaxClockSkew is how far X-Mama-Signed-At may be from the server clock, so
// a captured request stops working soon.
const MaxClockSkew = 5 * time.Minute

// SignedMessage is what the device signs with its bound Ed25519 key:
//
//	MAMA-device-voice-v1 <method> <device_id> <signed_at>[ <hex sha256 of the audio>]
//
// with single spaces, the hash only for POST (lowercase hex). Binding the
// audio's hash means a captured upload cannot carry another file, and
// signed_at bounds a replay to MaxClockSkew.
func SignedMessage(method, deviceID string, signedAt int64, audioSHA256 []byte) []byte {
	m := signatureContext + " " + method + " " + deviceID + " " + strconv.FormatInt(signedAt, 10)
	if audioSHA256 != nil {
		m += " " + hex.EncodeToString(audioSHA256)
	}
	return []byte(m)
}

var (
	// errNoKey means the device has no bound key, so nothing it sends can
	// be verified: 409, like a registration that conflicts with the binding.
	errNoKey = errors.New("device has no registered ed25519 key; register one with POST /devices first")
	// errBadSignature is every other signature failure: 401.
	errBadSignature = errors.New("missing, expired or invalid " + HeaderSignature)
)

// verify checks the request's signature headers against the device's key.
func verify(key []byte, signedAtHeader, sigHeader string, msg func(signedAt int64) []byte, now time.Time) error {
	if len(key) != ed25519.PublicKeySize {
		return errNoKey
	}
	signedAt, err := strconv.ParseInt(signedAtHeader, 10, 64)
	if err != nil {
		return errBadSignature
	}
	if d := now.Sub(time.Unix(signedAt, 0)); d > MaxClockSkew || d < -MaxClockSkew {
		return errBadSignature
	}
	sig, err := base64.StdEncoding.DecodeString(sigHeader)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(key, msg(signedAt), sig) {
		return errBadSignature
	}
	return nil
}
