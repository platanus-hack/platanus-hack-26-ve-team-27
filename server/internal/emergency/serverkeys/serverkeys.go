// Package serverkeys holds the server's two offline-emergency keys (#55): the
// Ed25519 key that signs acks and the X25519 key that opens mesh sealed boxes.
// Phones pin the public halves from GET /config.
package serverkeys

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

// Env var names, used in errors and in the ephemeral list. Never the values.
const (
	seedVar   = "EMERGENCY_ED25519_SEED"
	x25519Var = "EMERGENCY_X25519_PRIVATE_KEY"
)

// Keys is the server key pair.
type Keys struct {
	Signing ed25519.PrivateKey // signs acks (payload.SignAck)
	Sealing *ecdh.PrivateKey   // opens sealed boxes (payload.Open)
}

// SigningPublic is the Ed25519 public key phones verify acks with.
func (k Keys) SigningPublic() ed25519.PublicKey {
	return k.Signing.Public().(ed25519.PublicKey)
}

// SealingPublic is the 32 raw bytes of the X25519 public key phones seal to.
func (k Keys) SealingPublic() []byte { return k.Sealing.PublicKey().Bytes() }

// Generate makes a fresh key pair from r. It reads exactly 32 bytes per key
// with io.ReadFull and never hands r to a stdlib key generator, which newer Go
// versions ignore.
func Generate(r io.Reader) (Keys, error) {
	seed := make([]byte, 32)
	if _, err := io.ReadFull(r, seed); err != nil {
		return Keys{}, fmt.Errorf("serverkeys: reading signing seed: %w", err)
	}
	priv := make([]byte, 32)
	if _, err := io.ReadFull(r, priv); err != nil {
		return Keys{}, fmt.Errorf("serverkeys: reading sealing key: %w", err)
	}
	return build(seed, priv)
}

func build(seed, priv []byte) (Keys, error) {
	sealing, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return Keys{}, fmt.Errorf("serverkeys: sealing key: %w", err)
	}
	return Keys{Signing: ed25519.NewKeyFromSeed(seed), Sealing: sealing}, nil
}

// FromConfig decodes the keys named in c. A key that is unset is generated from
// r and its env var name is returned in ephemeral so the caller can warn. A key
// that is set but not standard Base64 of 32 bytes is an error that names the
// variable and never echoes the value.
func FromConfig(c config.Emergency, r io.Reader) (k Keys, ephemeral []string, err error) {
	seed, err := decode(seedVar, c.Ed25519Seed)
	if err != nil {
		return Keys{}, nil, err
	}
	priv, err := decode(x25519Var, c.X25519PrivateKey)
	if err != nil {
		return Keys{}, nil, err
	}
	if seed == nil {
		ephemeral = append(ephemeral, seedVar)
		if seed, err = readKey(r, "signing seed"); err != nil {
			return Keys{}, nil, err
		}
	}
	if priv == nil {
		ephemeral = append(ephemeral, x25519Var)
		if priv, err = readKey(r, "sealing key"); err != nil {
			return Keys{}, nil, err
		}
	}
	k, err = build(seed, priv)
	if err != nil {
		return Keys{}, nil, err
	}
	return k, ephemeral, nil
}

func readKey(r io.Reader, what string) ([]byte, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("serverkeys: reading %s: %w", what, err)
	}
	return b, nil
}

// decode returns nil for an unset value.
func decode(name, v string) ([]byte, error) {
	if v == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.Strict().DecodeString(v)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("serverkeys: %s must be standard Base64 of 32 bytes", name)
	}
	return b, nil
}
