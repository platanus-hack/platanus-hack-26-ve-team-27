// Package contract holds the frozen JSON shapes shared by the phone, the
// server, the call webhook and the notifier, plus the branch-by-speed rule.
// docs/contract.md documents every shape; testdata/*.json holds one canonical
// sample per message, read by both the Go and the Kotlin round-trip tests.
// The Kotlin mirror lives in app/src/main/java/com/team27/mama/contract/.
// Changing a shape after the contract freeze needs the Tech lead's OK.
package contract

import "time"

// Branch is the server's decision for an incident, picked from the speed.
type Branch string

const (
	// BranchSilentTracker: moving fast (in a car). The phone keeps sending
	// points; no call.
	BranchSilentTracker Branch = "silent_tracker"
	// BranchImmediateAlert: still. The family is alerted at once; no call.
	BranchImmediateAlert Branch = "immediate_alert"
	// BranchCall: otherwise, or speed unknown. "Mamá" calls.
	BranchCall Branch = "call"
)

// Speed thresholds in meters per second (Android Location.getSpeed units).
// These are the only copies on the server; Kotlin mirrors them in
// com.team27.mama.contract.SpeedThresholds.
const (
	// StillBelowMPS: a speed strictly below this is still (< 3.6 km/h).
	StillBelowMPS = 1.0
	// FastFromMPS: a speed at or above this is fast (>= 25.2 km/h).
	FastFromMPS = 7.0
)

// ChooseBranch picks the branch for a speed in m/s. A nil speed (the fix had
// no speed) or a negative or NaN one is unknown and gets BranchCall, the
// branch that asks the person.
func ChooseBranch(speedMPS *float64) Branch {
	switch {
	case speedMPS == nil || !(*speedMPS >= 0):
		return BranchCall
	case *speedMPS < StillBelowMPS:
		return BranchImmediateAlert
	case *speedMPS >= FastFromMPS:
		return BranchSilentTracker
	default:
		return BranchCall
	}
}

// Trigger is how the person started the alert on the phone.
type Trigger string

const (
	TriggerVolumePattern Trigger = "volume_pattern" // up-down-up, accessibility service
	TriggerWidget        Trigger = "widget"
	TriggerTile          Trigger = "tile" // quick settings tile
	TriggerInApp         Trigger = "in_app"
)

// Verdict is the duress detector's result for a call.
type Verdict string

const (
	VerdictDuress   Verdict = "duress"
	VerdictNoDuress Verdict = "no_duress"
	VerdictUnclear  Verdict = "unclear"
)

// Point is one GPS fix. SpeedMPS and AccuracyM are nil when the fix has none.
type Point struct {
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	AccuracyM  *float64  `json:"accuracy_m,omitempty"`
	SpeedMPS   *float64  `json:"speed_mps,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// DeviceRegistration is phone -> server, sent at the end of first-run setup
// and again whenever the setup changes. There is no auth: DeviceID is a UUID
// the app generates at install.
type DeviceRegistration struct {
	DeviceID      string   `json:"device_id"`
	PhoneNumber   string   `json:"phone_number"`   // E.164; "Mamá" calls this number
	FamilyNumbers []string `json:"family_numbers"` // E.164; the notifier writes to these
	DuressPhrase  string   `json:"duress_phrase"`
	AppVersion    string   `json:"app_version,omitempty"`
}

// AlertEvent is phone -> server when the person triggers an alert. The phone
// generates IncidentID (a UUID), so a retried event is the same incident.
// Points holds at least one fix, oldest first; the last one's speed picks
// the branch.
type AlertEvent struct {
	IncidentID  string    `json:"incident_id"`
	DeviceID    string    `json:"device_id"`
	Trigger     Trigger   `json:"trigger"`
	TriggeredAt time.Time `json:"triggered_at"`
	Points      []Point   `json:"points"`
	HasAudio    bool      `json:"has_audio"`
}

// LastPoint returns the newest fix, or nil when there is none.
func (e AlertEvent) LastPoint() *Point {
	if len(e.Points) == 0 {
		return nil
	}
	return &e.Points[len(e.Points)-1]
}

// Branch picks the branch from the newest fix's speed.
func (e AlertEvent) Branch() Branch {
	if p := e.LastPoint(); p != nil {
		return ChooseBranch(p.SpeedMPS)
	}
	return ChooseBranch(nil)
}

// AlertResponse is server -> phone, the reply to an AlertEvent. SpeedMPS is
// the speed the branch was picked from, nil when unknown.
type AlertResponse struct {
	IncidentID string   `json:"incident_id"`
	Branch     Branch   `json:"branch"`
	SpeedMPS   *float64 `json:"speed_mps,omitempty"`
}

// LocationUpdate is phone -> server: more fixes for an open incident, sent
// while in silent tracker mode (and by any branch that keeps tracking).
type LocationUpdate struct {
	IncidentID string  `json:"incident_id"`
	DeviceID   string  `json:"device_id"`
	Points     []Point `json:"points"`
}

// CallWebhook is ElevenLabs -> server: the post-call webhook
// (type "post_call_transcription"). Only the fields the server reads are
// modeled; the real payload has more and they are ignored.
type CallWebhook struct {
	Type           string          `json:"type"`
	EventTimestamp int64           `json:"event_timestamp"` // unix seconds
	Data           CallWebhookData `json:"data"`
}

// CallWebhookData is the "data" object of the post-call webhook.
type CallWebhookData struct {
	AgentID        string           `json:"agent_id"`
	ConversationID string           `json:"conversation_id"` // unique per call; dedupes repeated webhooks
	Status         string           `json:"status"`
	Transcript     []TranscriptTurn `json:"transcript"`
	Metadata       CallMetadata     `json:"metadata"`
	// ClientData carries the incident_id the server passed as a dynamic
	// variable when it placed the call.
	ClientData ConversationInitiationClientData `json:"conversation_initiation_client_data"`
}

// TranscriptTurn is one turn of the call. Role is "agent" ("Mamá") or "user".
type TranscriptTurn struct {
	Role           string  `json:"role"`
	Message        string  `json:"message"`
	TimeInCallSecs float64 `json:"time_in_call_secs"`
}

// CallMetadata is the part of the webhook metadata the server keeps.
type CallMetadata struct {
	StartTimeUnixSecs int64 `json:"start_time_unix_secs"`
	CallDurationSecs  int   `json:"call_duration_secs"`
}

// ConversationInitiationClientData holds the dynamic variables of the call.
type ConversationInitiationClientData struct {
	DynamicVariables map[string]string `json:"dynamic_variables"`
}

// DynamicVarIncidentID is the dynamic variable that ties a call to its
// incident. The call engine sets it; the webhook handler reads it back.
const DynamicVarIncidentID = "incident_id"

// IncidentID returns the incident the call belongs to, or "" when missing.
func (w CallWebhook) IncidentID() string {
	return w.Data.ClientData.DynamicVariables[DynamicVarIncidentID]
}

// NotifierMessage is server -> notifier: the one message the family gets for
// an incident. It carries the location and the verdict, and never a link.
// Verdict and Reason are empty when no call happened (immediate_alert,
// silent_tracker). IncidentID is the dedupe key: one message per incident.
type NotifierMessage struct {
	IncidentID  string    `json:"incident_id"`
	To          []string  `json:"to"` // E.164 family numbers
	Branch      Branch    `json:"branch"`
	Location    Point     `json:"location"`
	Verdict     Verdict   `json:"verdict,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	TriggeredAt time.Time `json:"triggered_at"`
}
