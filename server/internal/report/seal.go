package report

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// SignatureContext prefixes the bytes a seal signs, and is the document's
// "format". It keeps a report signature from ever being valid as an offline
// ack or a device registration, and the reverse, though the server signs all
// with one key (docs/adr/0005-incident-report-seal.md).
const SignatureContext = "MAMA-incident-report-v1"

// Errors of Verify. Each message says, in Spanish, why the seal fails: the
// verify command prints it for whoever checks a report.
var (
	ErrBadKey       = errors.New("la llave pública debe ser Base64 estándar de 32 bytes, como la de GET /config")
	ErrBadSignature = errors.New("la firma debe tener 64 bytes (o su Base64)")
	ErrSealInvalid  = errors.New("la firma no corresponde a esta ficha con esta llave: la ficha cambió después de sellarla, o la selló otra llave")
	ErrNotAReport   = errors.New("la firma es válida, pero el archivo no es una ficha de MAMÁ v1")
)

// document is ficha.json, version 1. Field order is the byte order: the
// stored bytes, not this struct, are what the signature covers.
type document struct {
	Format    string        `json:"format"`
	Incident  docIncident   `json:"incident"`
	Points    []docPoint    `json:"points"`
	Calls     []docCall     `json:"calls"`
	SealedAt  string        `json:"sealed_at"`                 // server clock
	PublicKey string        `json:"server_ed25519_public_key"` // std Base64, as GET /config
	Clocks    docClockNotes `json:"clocks"`
}

// docIncident has no device_id on purpose: it is the device's only
// credential on POST /devices and POST /incidents/{incident_id}/points, and
// anyone holding the link downloads this file. With it they could
// re-register the device to their own numbers or add points to its
// incidents.
type docIncident struct {
	IncidentID    string   `json:"incident_id"`
	Trigger       string   `json:"trigger"`
	Branch        string   `json:"branch"`
	SpeedMPS      *float64 `json:"speed_mps,omitempty"`
	TriggeredAt   string   `json:"triggered_at"`
	ReceivedAt    string   `json:"received_at"`
	NotifiedAt    string   `json:"notified_at,omitempty"`
	FamilyNumbers int      `json:"family_numbers"`
}

type docPoint struct {
	Lat        float64  `json:"lat"`
	Lng        float64  `json:"lng"`
	AccuracyM  *float64 `json:"accuracy_m,omitempty"`
	SpeedMPS   *float64 `json:"speed_mps,omitempty"`
	RecordedAt string   `json:"recorded_at"`
	ReceivedAt string   `json:"received_at"`
}

type docCall struct {
	ConversationID string                    `json:"conversation_id,omitempty"`
	Status         string                    `json:"status"`
	RequestedAt    string                    `json:"requested_at"`
	EndedAt        string                    `json:"ended_at,omitempty"`
	DurationSecs   *int                      `json:"duration_secs,omitempty"`
	Transcript     []contract.TranscriptTurn `json:"transcript"`
	Verdict        string                    `json:"verdict,omitempty"`
	VerdictReason  string                    `json:"verdict_reason,omitempty"`
	VerdictAt      string                    `json:"verdict_at,omitempty"`
	Recording      *docRecording             `json:"recording,omitempty"`
}

type docRecording struct {
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
	StoredAt    string `json:"stored_at"`
}

// docClockNotes says which clock each time comes from, inside the signed
// bytes, so the file explains itself without this code.
type docClockNotes struct {
	Phone      string `json:"phone"`
	Server     string `json:"server"`
	ElevenLabs string `json:"elevenlabs"`
}

var clockNotes = docClockNotes{
	Phone:      "incident.triggered_at, points[].recorded_at",
	Server:     "incident.received_at, incident.notified_at (claimed before sending, not a delivery receipt), points[].received_at, calls[].requested_at, calls[].verdict_at, calls[].recording.stored_at, sealed_at; calls[].ended_at when the server marked the call failed",
	ElevenLabs: "calls[].ended_at and calls[].duration_secs from the post-call webhook; calls[].transcript[].time_in_call_secs counts from the call's start",
}

// Document is ficha.json for rec, sealed at sealedAt by pub: the incident,
// its points, its calls with transcript, verdict and recording hash, and the
// time the family message was claimed. It leaves out the phone numbers, the
// device id (see docIncident) and the duress phrase. Callers store and serve
// the returned bytes as they are.
func Document(rec Record, sealedAt time.Time, pub ed25519.PublicKey) ([]byte, error) {
	inc := rec.Incident
	d := document{
		Format: SignatureContext,
		Incident: docIncident{
			IncidentID:    inc.ID,
			Trigger:       string(inc.Trigger),
			Branch:        string(inc.Branch),
			SpeedMPS:      inc.SpeedMPS,
			TriggeredAt:   stamp(inc.TriggeredAt),
			ReceivedAt:    stamp(inc.CreatedAt),
			NotifiedAt:    stampPtr(inc.NotifiedAt),
			FamilyNumbers: rec.Device.FamilyNumbers,
		},
		Points:    []docPoint{},
		Calls:     []docCall{},
		SealedAt:  stamp(sealedAt),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Clocks:    clockNotes,
	}
	for _, p := range rec.Points {
		d.Points = append(d.Points, docPoint{
			Lat: p.Lat, Lng: p.Lng, AccuracyM: p.AccuracyM, SpeedMPS: p.SpeedMPS,
			RecordedAt: stamp(p.RecordedAt), ReceivedAt: stamp(p.ReceivedAt),
		})
	}
	for _, c := range rec.Calls {
		dc := docCall{
			ConversationID: c.ConversationID,
			Status:         c.Status,
			RequestedAt:    stamp(c.RequestedAt),
			EndedAt:        stampPtr(c.EndedAt),
			DurationSecs:   c.DurationSecs,
			Transcript:     c.Transcript,
			Verdict:        string(c.Verdict),
			VerdictReason:  c.VerdictReason,
			VerdictAt:      stampPtr(c.VerdictAt),
		}
		if dc.Transcript == nil {
			dc.Transcript = []contract.TranscriptTurn{}
		}
		if r := c.Recording; r != nil {
			dc.Recording = &docRecording{
				SHA256: hex.EncodeToString(r.SHA256[:]), SizeBytes: r.Size,
				ContentType: r.ContentType, StoredAt: stamp(r.StoredAt),
			}
		}
		d.Calls = append(d.Calls, dc)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// signedMessage is what a seal signs: the context, then the document bytes.
func signedMessage(doc []byte) []byte {
	return append([]byte(SignatureContext), doc...)
}

// Sign returns the pure Ed25519 signature (64 bytes) of the document.
func Sign(doc []byte, key ed25519.PrivateKey) []byte {
	return ed25519.Sign(key, signedMessage(doc))
}

// EncodeSignature is ficha.sig: the signature in standard Base64 and a
// newline, so it can be pasted, mailed or printed.
func EncodeSignature(sig []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
}

// ParseSignature reads ficha.sig: Base64 text (EncodeSignature's), or the
// 64 raw bytes.
func ParseSignature(b []byte) ([]byte, error) {
	if len(b) == ed25519.SignatureSize {
		return b, nil
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrBadSignature
	}
	return sig, nil
}

// ParsePublicKey reads the server key as GET /config and the page show it:
// standard Base64 of 32 bytes.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrBadKey
	}
	return ed25519.PublicKey(b), nil
}

// Sealed is what Verify read from a document whose seal is valid.
type Sealed struct {
	IncidentID string
	SealedAt   string
	Calls      int
}

// Verify checks a seal: sig must be pub's Ed25519 signature over
// SignatureContext || doc, byte for byte, and doc must be a v1 report that
// names pub as its key. The errors say why not, in Spanish.
func Verify(doc, sig []byte, pub ed25519.PublicKey) (Sealed, error) {
	if len(pub) != ed25519.PublicKeySize {
		return Sealed{}, ErrBadKey
	}
	if len(sig) != ed25519.SignatureSize {
		return Sealed{}, ErrBadSignature
	}
	if !ed25519.Verify(pub, signedMessage(doc), sig) {
		return Sealed{}, ErrSealInvalid
	}
	var d document
	if err := json.Unmarshal(doc, &d); err != nil || d.Format != SignatureContext {
		return Sealed{}, ErrNotAReport
	}
	if d.PublicKey != base64.StdEncoding.EncodeToString(pub) {
		return Sealed{}, fmt.Errorf("%w: el documento nombra otra llave", ErrNotAReport)
	}
	return Sealed{IncidentID: d.Incident.IncidentID, SealedAt: d.SealedAt, Calls: len(d.Calls)}, nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func stampPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return stamp(*t)
}
