package payload

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// signedMessage is what Ed25519 signs: the context, then the bytes.
func signedMessage(body []byte) []byte {
	return append([]byte(SignatureContext), body...)
}

// DeviceID is the first 8 bytes of SHA-256 of the raw public key.
func DeviceID(pub ed25519.PublicKey) uint64 {
	sum := sha256.Sum256(pub)
	return binary.BigEndian.Uint64(sum[:8])
}

// Sign returns the 98-byte signed payload. The payload must carry the device
// id of the signing key.
func Sign(p Payload, key ed25519.PrivateKey) ([]byte, error) {
	body, err := p.MarshalBody()
	if err != nil {
		return nil, err
	}
	if p.DeviceID != DeviceID(key.Public().(ed25519.PublicKey)) {
		return nil, fmt.Errorf("payload: device_id does not match the signing key")
	}
	return append(body, ed25519.Sign(key, signedMessage(body))...), nil
}

// ParseSigned checks the length and the body rules but not the signature. The
// server uses it to read device_id before it looks up the public key.
func ParseSigned(b []byte) (Payload, error) {
	if len(b) != SignedSize {
		return Payload{}, fmt.Errorf("%w: signed payload is %d bytes, want %d", ErrFormat, len(b), SignedSize)
	}
	return ParseBody(b[:BodySize])
}

// Verify runs the full check in order: length, version, kind, ranges,
// device_id against the key, signature.
func Verify(b []byte, pub ed25519.PublicKey) (Payload, error) {
	p, err := ParseSigned(b)
	if err != nil {
		return Payload{}, err
	}
	if len(pub) != ed25519.PublicKeySize {
		return Payload{}, fmt.Errorf("%w: public key is %d bytes", ErrSignature, len(pub))
	}
	if p.DeviceID != DeviceID(pub) {
		return Payload{}, fmt.Errorf("%w: device_id does not match the public key", ErrSignature)
	}
	if !ed25519.Verify(pub, signedMessage(b[:BodySize]), b[BodySize:]) {
		return Payload{}, fmt.Errorf("%w: Ed25519 verification failed", ErrSignature)
	}
	return p, nil
}
