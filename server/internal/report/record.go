// Package report is the incident report ("ficha del incidente", #76): a
// private page at GET /i/{token} with the incident's timeline, locations,
// call transcript and recording, and the server's Ed25519 seal.
//
// The token is a link secret, not the incident_id (which comes from the
// phone): see NewToken and DBStore.Ensure. Build turns a stored Record into
// the page, as a pure function; Handler serves it with privacy headers and
// embedded fonts and images, so opening a report makes no third-party
// request. The whole feature sits behind INCIDENT_REPORT_ENABLED.
package report

import (
	"crypto/ed25519"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Record is everything stored about one incident that the report shows.
type Record struct {
	Incident Incident
	Device   Device
	Points   []Point // oldest first
	Calls    []Call  // in the order they were requested
	Seal     *Seal   // nil until sealed
	// Deliveries are the family messages the notifier sent, oldest first,
	// with what the provider reported since (notify_deliveries).
	Deliveries []Delivery
	// Emergencies are offline emergencies (#57) of the same device whose
	// time span overlaps the incident's: linked by device and time only,
	// since the schema has no link. Shown, never sealed.
	Emergencies []Emergency
}

// Incident is the incidents row.
type Incident struct {
	ID          string
	DeviceID    string
	Trigger     contract.Trigger
	Branch      contract.Branch
	SpeedMPS    *float64   // the speed the branch was picked from; nil = unknown
	TriggeredAt time.Time  // phone clock
	CreatedAt   time.Time  // server clock: the alert reached the server
	NotifiedAt  *time.Time // server clock: the family message was claimed, before sending
}

// Device is what the report needs from the devices row.
type Device struct {
	PhoneNumber   string // E.164; the page shows it masked only
	FamilyNumbers int    // how many family numbers the device has
	// DuressPhrase only finds the transcript turn that says it, to highlight
	// it. It is never shown and never sealed.
	DuressPhrase string
	// UserName is devices.user_name when that column exists (#104 adds it);
	// "" otherwise. Shown, never sealed.
	UserName string
}

// Point is one gps_points row.
type Point struct {
	Lat        float64
	Lng        float64
	AccuracyM  *float64
	SpeedMPS   *float64
	RecordedAt time.Time // phone clock
	ReceivedAt time.Time // server clock
}

// Call is one calls row, with its recording when one is stored.
type Call struct {
	ID             int64
	ConversationID string // "" until ElevenLabs answered the outbound request
	Status         string // a calls.status value
	Transcript     []contract.TranscriptTurn
	DurationSecs   *int      // from the post-call webhook
	RequestedAt    time.Time // server clock
	// EndedAt is start + duration from the webhook (ElevenLabs' clock), or
	// the server clock when the server marked the call failed (then
	// DurationSecs is nil).
	EndedAt       *time.Time
	Verdict       contract.Verdict // "" until the detector judged the call
	VerdictReason string
	VerdictAt     *time.Time // server clock
	Recording     *Recording // nil until the audio is stored
	// What the engine saw polling Twilio (call_progress), server clock:
	// TwilioStatus is the last status, "" when never polled; RingingAt the
	// first ringing (or later) status, PickedUpAt the first in-progress.
	TwilioStatus string
	RingingAt    *time.Time
	PickedUpAt   *time.Time
	// Context is the call's ficha (#106, calls.ficha) once stored; nil
	// without one, or before that column exists.
	Context *CallContext
}

// Emergency is an offline emergency (emergencies row) linked to the
// incident by device and time.
type Emergency struct {
	ID              string
	Status          string // open, acked or cancelled
	Seq             int
	Channels        []string // http, sms, mesh, in first-seen order
	MinHops         *int
	Lat, Lng        *float64  // nil without a fix
	SentAt          time.Time // phone clock
	FirstReceivedAt time.Time // server clock
	UpdatedAt       time.Time // server clock
	CancelledAt     *time.Time
}

// Recording describes a stored call recording; the audio bytes are read
// separately (DBStore.Audio).
type Recording struct {
	ContentType string
	SHA256      [32]byte
	Size        int64
	StoredAt    time.Time // server clock
}

// Seal is the stored seal of an incident (see Document and Sign).
type Seal struct {
	SealedAt  time.Time
	Document  []byte // the exact bytes of ficha.json
	Signature []byte // Ed25519 over SignatureContext || Document
	PublicKey ed25519.PublicKey
}

// AnsweredAt is when the call connected: EndedAt minus the duration, both
// from ElevenLabs. ok is false when either is unknown or the call failed.
func (c Call) AnsweredAt() (t time.Time, ok bool) {
	if c.Status != "done" || c.EndedAt == nil || c.DurationSecs == nil {
		return time.Time{}, false
	}
	return c.EndedAt.Add(-time.Duration(*c.DurationSecs) * time.Second), true
}

// Delivery is one family message: to whom (masked), whether the provider
// took it, and what its status webhooks said since.
type Delivery struct {
	Recipient   string // masked, "+58 424 ···33"
	Channel     string // whatsapp or sms
	MessageID   string
	Status      string // accepted, failed, sent, delivered or read
	Error       string
	SentAt      time.Time  // server clock
	DeliveredAt *time.Time // server clock, when the webhook said so
	ReadAt      *time.Time
}
