// Package payload is the Go reference for the offline emergency payload: the
// 34-byte body, the Ed25519 signature over it and the server's signed ack.
// The layout, the signature context and the reasons behind them are in
// docs/adr/0001-offline-emergency-payload.md. The Kotlin port lives in
// app/src/main/java/com/team27/mama/emergency/payload/ and must stay
// byte-identical; testdata/vectors.json pins both.
package payload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// Wire sizes and limits. All multi-byte integers are big-endian.
const (
	// Version is the only payload and ack version.
	Version = 1
	// BodySize is the unsigned body: version, kind, device_id, emergency_id,
	// seq, lat, lon, loc_age_s and timestamp.
	BodySize = 34
	// SignatureSize is a raw Ed25519 signature.
	SignatureSize = 64
	// SignedSize is body plus signature.
	SignedSize = BodySize + SignatureSize
	// AckBodySize is version, kind, emergency_id and seq.
	AckBodySize = 12
	// AckSize is the ack body plus the server's signature.
	AckSize = AckBodySize + SignatureSize
	// NoLocation in loc_age_s means "no fix yet"; lat and lon are then 0.
	NoLocation uint16 = 0xFFFF
	// MaxLatE5 and MaxLonE5 are the degree limits times 1e5.
	MaxLatE5 = 9_000_000
	MaxLonE5 = 18_000_000
	// SignatureContext is prepended to every signed message and never
	// transmitted. It keeps a MAMA signature from being valid over a bitchat
	// packet, which is signed with the same Ed25519 identity key.
	SignatureContext = "MAMA-offline-emergency-v1"
)

// LocAgeOf returns the loc_age_s for a fix that is age old: NoLocation when
// there is no fix, otherwise the whole seconds clamped to 0..65534.
func LocAgeOf(age time.Duration, ok bool) uint16 {
	if !ok {
		return NoLocation
	}
	s := age / time.Second
	if s < 0 {
		return 0
	}
	if s > 65534 {
		return 65534
	}
	return uint16(s)
}

// Kind is the message type in byte 1.
type Kind uint8

const (
	KindAlert  Kind = 1
	KindUpdate Kind = 2
	KindCancel Kind = 3
	// KindAck only appears in an ack, never in a payload body.
	KindAck Kind = 4
)

// The three error categories, shared with the Kotlin port. Wrap them with
// fmt.Errorf("%w: ...") and test with errors.Is.
var (
	// ErrFormat: wrong length, version, kind, range, prefix or Base64.
	ErrFormat = errors.New("payload: malformed")
	// ErrSignature: bad signature, wrong device_id or a bad public key.
	ErrSignature = errors.New("payload: bad signature")
	// ErrDecrypt: a sealed box that cannot be opened.
	ErrDecrypt = errors.New("payload: cannot decrypt")
)

// Payload is the decoded body of an alert, update or cancel.
type Payload struct {
	Kind        Kind
	DeviceID    uint64
	EmergencyID uint64
	Seq         uint16
	LatE5       int32
	LonE5       int32
	// LocAgeS is the seconds since the fix, saturated at 65534; NoLocation
	// means there is no fix.
	LocAgeS   uint16
	Timestamp uint32
}

// checkFields applies the body rules that do not depend on the byte layout.
func checkFields(p Payload) error {
	if p.Kind < KindAlert || p.Kind > KindCancel {
		return fmt.Errorf("%w: kind %d", ErrFormat, p.Kind)
	}
	if p.LatE5 < -MaxLatE5 || p.LatE5 > MaxLatE5 {
		return fmt.Errorf("%w: lat_e5 %d out of range", ErrFormat, p.LatE5)
	}
	if p.LonE5 < -MaxLonE5 || p.LonE5 > MaxLonE5 {
		return fmt.Errorf("%w: lon_e5 %d out of range", ErrFormat, p.LonE5)
	}
	if p.LocAgeS == NoLocation && (p.LatE5 != 0 || p.LonE5 != 0) {
		return fmt.Errorf("%w: no fix but non-zero coordinates", ErrFormat)
	}
	return nil
}

// MarshalBody returns the 34-byte body, or ErrFormat if a field is invalid.
func (p Payload) MarshalBody() ([]byte, error) {
	if err := checkFields(p); err != nil {
		return nil, err
	}
	b := make([]byte, BodySize)
	b[0] = Version
	b[1] = byte(p.Kind)
	binary.BigEndian.PutUint64(b[2:], p.DeviceID)
	binary.BigEndian.PutUint64(b[10:], p.EmergencyID)
	binary.BigEndian.PutUint16(b[18:], p.Seq)
	binary.BigEndian.PutUint32(b[20:], uint32(p.LatE5))
	binary.BigEndian.PutUint32(b[24:], uint32(p.LonE5))
	binary.BigEndian.PutUint16(b[28:], p.LocAgeS)
	binary.BigEndian.PutUint32(b[30:], p.Timestamp)
	return b, nil
}

// ParseBody is the inverse of MarshalBody and enforces the same rules.
func ParseBody(b []byte) (Payload, error) {
	if len(b) != BodySize {
		return Payload{}, fmt.Errorf("%w: body is %d bytes, want %d", ErrFormat, len(b), BodySize)
	}
	if b[0] != Version {
		return Payload{}, fmt.Errorf("%w: version %d", ErrFormat, b[0])
	}
	p := Payload{
		Kind:        Kind(b[1]),
		DeviceID:    binary.BigEndian.Uint64(b[2:]),
		EmergencyID: binary.BigEndian.Uint64(b[10:]),
		Seq:         binary.BigEndian.Uint16(b[18:]),
		LatE5:       int32(binary.BigEndian.Uint32(b[20:])),
		LonE5:       int32(binary.BigEndian.Uint32(b[24:])),
		LocAgeS:     binary.BigEndian.Uint16(b[28:]),
		Timestamp:   binary.BigEndian.Uint32(b[30:]),
	}
	if err := checkFields(p); err != nil {
		return Payload{}, err
	}
	return p, nil
}

// ToE5 converts degrees to degrees times 1e5, rounding half away from zero on
// the double product. NaN, infinities and |deg| > 180 are rejected; the lat
// range is enforced later by the body rules.
func ToE5(deg float64) (int32, error) {
	if math.IsNaN(deg) || math.IsInf(deg, 0) || math.Abs(deg) > 180 {
		return 0, fmt.Errorf("%w: degrees %v out of range", ErrFormat, deg)
	}
	return int32(math.Round(deg * 1e5)), nil
}
