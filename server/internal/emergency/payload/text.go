package payload

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Transport text prefixes. Every MAMA mesh text starts with MeshPrefix.
const (
	SMSPrefix        = "MAMA1 "
	MeshPrefix       = "MAMA1:"
	MeshSealedPrefix = "MAMA1:SEAL:"
	MeshAckPrefix    = "MAMA1:ACK:"
)

// MeshFrameType says which kind of mesh text was parsed.
type MeshFrameType uint8

const (
	MeshSealed MeshFrameType = 1
	MeshAck    MeshFrameType = 2
)

// MeshFrame is a parsed mesh text: a sealed box or an ack.
type MeshFrame struct {
	Type  MeshFrameType
	Bytes []byte
}

const whitespace = " \t\r\n"

// SMSText is "MAMA1 " plus unpadded Base64url of a signed payload or ack.
func SMSText(frame []byte) string {
	return SMSPrefix + base64.RawURLEncoding.EncodeToString(frame)
}

// MeshSealedText is "MAMA1:SEAL:" plus padded standard Base64 of a sealed box.
func MeshSealedText(box []byte) string {
	return MeshSealedPrefix + base64.StdEncoding.EncodeToString(box)
}

// MeshAckText is "MAMA1:ACK:" plus padded standard Base64 of an ack.
func MeshAckText(ack []byte) string {
	return MeshAckPrefix + base64.StdEncoding.EncodeToString(ack)
}

// decodeCanonical decodes s and requires that re-encoding gives s back. That
// rejects wrong padding, embedded newlines (Go skips them) and stray bits.
func decodeCanonical(enc *base64.Encoding, s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("%w: empty Base64", ErrFormat)
	}
	b, err := enc.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: Base64: %v", ErrFormat, err)
	}
	if enc.EncodeToString(b) != s {
		return nil, fmt.Errorf("%w: Base64 is not canonical", ErrFormat)
	}
	return b, nil
}

// ParseSMSText returns the signed payload or ack bytes of an SMS body. Outer
// whitespace is ignored; everything else must match exactly.
func ParseSMSText(s string) ([]byte, error) {
	s = strings.Trim(s, whitespace)
	rest, ok := strings.CutPrefix(s, SMSPrefix)
	if !ok {
		return nil, fmt.Errorf("%w: missing %q prefix", ErrFormat, SMSPrefix)
	}
	return decodeCanonical(base64.RawURLEncoding, rest)
}

// IsMeshText reports whether s looks like a MAMA mesh text.
func IsMeshText(s string) bool {
	return strings.HasPrefix(strings.Trim(s, whitespace), MeshPrefix)
}

// ParseMeshText parses a SEAL or ACK mesh text.
func ParseMeshText(s string) (MeshFrame, error) {
	s = strings.Trim(s, whitespace)
	rest, ok := strings.CutPrefix(s, MeshPrefix)
	if !ok {
		return MeshFrame{}, fmt.Errorf("%w: missing %q prefix", ErrFormat, MeshPrefix)
	}
	tag, data, ok := strings.Cut(rest, ":")
	if !ok {
		return MeshFrame{}, fmt.Errorf("%w: missing mesh tag", ErrFormat)
	}
	var t MeshFrameType
	switch tag {
	case "SEAL":
		t = MeshSealed
	case "ACK":
		t = MeshAck
	default:
		return MeshFrame{}, fmt.Errorf("%w: unknown mesh tag %q", ErrFormat, tag)
	}
	b, err := decodeCanonical(base64.StdEncoding, data)
	if err != nil {
		return MeshFrame{}, err
	}
	return MeshFrame{Type: t, Bytes: b}, nil
}
