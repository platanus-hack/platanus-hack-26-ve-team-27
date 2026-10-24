package payload

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
)

// Ack tells the phone which seq the server has accepted for an emergency.
type Ack struct {
	EmergencyID uint64
	Seq         uint16
}

// SignAck returns the 76-byte ack signed by the server key.
func SignAck(a Ack, key ed25519.PrivateKey) []byte {
	body := make([]byte, AckBodySize, AckSize)
	body[0] = Version
	body[1] = byte(KindAck)
	binary.BigEndian.PutUint64(body[2:], a.EmergencyID)
	binary.BigEndian.PutUint16(body[10:], a.Seq)
	return append(body, ed25519.Sign(key, signedMessage(body))...)
}

// VerifyAck checks length, version, kind and the server's signature.
func VerifyAck(b []byte, pub ed25519.PublicKey) (Ack, error) {
	if len(b) != AckSize {
		return Ack{}, fmt.Errorf("%w: ack is %d bytes, want %d", ErrFormat, len(b), AckSize)
	}
	if b[0] != Version {
		return Ack{}, fmt.Errorf("%w: ack version %d", ErrFormat, b[0])
	}
	if Kind(b[1]) != KindAck {
		return Ack{}, fmt.Errorf("%w: ack kind %d", ErrFormat, b[1])
	}
	if len(pub) != ed25519.PublicKeySize {
		return Ack{}, fmt.Errorf("%w: public key is %d bytes", ErrSignature, len(pub))
	}
	if !ed25519.Verify(pub, signedMessage(b[:AckBodySize]), b[AckBodySize:]) {
		return Ack{}, fmt.Errorf("%w: Ed25519 verification failed", ErrSignature)
	}
	return Ack{
		EmergencyID: binary.BigEndian.Uint64(b[2:]),
		Seq:         binary.BigEndian.Uint16(b[10:]),
	}, nil
}
