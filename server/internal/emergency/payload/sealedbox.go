package payload

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

const (
	// SealedOverhead is the ephemeral public key (32) plus the GCM tag (16).
	SealedOverhead = 48
	// SealedBoxInfo is the HKDF info string.
	SealedBoxInfo = "MAMA-sealed-box-v1"
)

// deriveKeyNonce returns the AES-256 key and the 12-byte GCM nonce. The nonce
// is derived, not sent: there is one key per ephemeral, so it is never reused.
func deriveKeyNonce(shared, ephPub, recipientPub []byte) (key, nonce []byte, err error) {
	salt := append(append([]byte{}, ephPub...), recipientPub...)
	okm, err := hkdf.Key(sha256.New, shared, salt, SealedBoxInfo, 44)
	if err != nil {
		return nil, nil, err
	}
	return okm[:32], okm[32:44], nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext to the recipient's X25519 public key. The 32
// ephemeral bytes are read from rand with io.ReadFull so tests can fix them.
func Seal(rand io.Reader, recipient *ecdh.PublicKey, plaintext []byte) ([]byte, error) {
	if recipient == nil || recipient.Curve() != ecdh.X25519() {
		return nil, errors.New("payload: recipient is not an X25519 key")
	}
	seed := make([]byte, 32)
	if _, err := io.ReadFull(rand, seed); err != nil {
		return nil, fmt.Errorf("payload: reading ephemeral key: %w", err)
	}
	eph, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(recipient)
	if err != nil {
		return nil, fmt.Errorf("payload: key agreement: %w", err)
	}
	ephPub := eph.PublicKey().Bytes()
	key, nonce, err := deriveKeyNonce(shared, ephPub, recipient.Bytes())
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(ephPub, nonce, plaintext, nil), nil
}

// Open decrypts a sealed box. A box shorter than SealedOverhead is ErrFormat;
// a key agreement or authentication failure is ErrDecrypt.
func Open(recipient *ecdh.PrivateKey, box []byte) ([]byte, error) {
	if len(box) < SealedOverhead {
		return nil, fmt.Errorf("%w: sealed box is %d bytes, want at least %d", ErrFormat, len(box), SealedOverhead)
	}
	ephPub, err := ecdh.X25519().NewPublicKey(box[:32])
	if err != nil {
		return nil, fmt.Errorf("%w: ephemeral key: %v", ErrDecrypt, err)
	}
	shared, err := recipient.ECDH(ephPub)
	if err != nil {
		return nil, fmt.Errorf("%w: key agreement: %v", ErrDecrypt, err)
	}
	key, nonce, err := deriveKeyNonce(shared, box[:32], recipient.PublicKey().Bytes())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecrypt, err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecrypt, err)
	}
	pt, err := gcm.Open(nil, nonce, box[32:], nil)
	if err != nil {
		return nil, fmt.Errorf("%w: authentication failed", ErrDecrypt)
	}
	return pt, nil
}
