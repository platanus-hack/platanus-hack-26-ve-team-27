// Package contract holds the frozen JSON shapes shared by the phone, the
// server, the call webhook and the notifier, plus the branch-by-speed rule.
// docs/contract.md documents every shape; testdata/*.json holds one canonical
// sample per message, read by both the Go and the Kotlin round-trip tests.
// The Kotlin mirror lives in app/src/main/java/com/team27/mama/contract/.
// Changing a shape after the contract freeze needs the Tech lead's OK.
package contract

import (
	"encoding/json"
	"time"
)

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
	// TriggerOffline is server-only: the incident was opened from an offline
	// emergency (HTTP, SMS or the BLE relay). A phone never sends it in an AlertEvent.
	TriggerOffline Trigger = "offline"
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
	// UserName is how "Mamá" greets the person, 1 to 30 characters.
	// Optional: omitted keeps the stored name (#104).
	UserName string `json:"user_name,omitempty"`
	// SafePhrase is the "todo bien" phrase: what the person says when they
	// are safe. It must differ from DuressPhrase and not share most of its
	// key words. Optional: omitted keeps the stored phrase (#104).
	SafePhrase string `json:"safe_phrase,omitempty"`
	// Ed25519PublicKey is the device's Ed25519 identity public key (bitchat's,
	// #56), std Base64 of 32 bytes in JSON; the server derives key_id from it
	// to verify offline payloads (#57). Optional: omitted keeps the stored key.
	Ed25519PublicKey []byte `json:"ed25519_public_key,omitempty"`
	// Ed25519Signature proves the phone holds the key's private half: the
	// 64-byte Ed25519 signature of RegistrationSignedMessage(DeviceID), std
	// Base64 in JSON. Required with Ed25519PublicKey and only with it, since
	// bitchat broadcasts public keys on the mesh and anyone could otherwise
	// register a victim's key first.
	Ed25519Signature []byte `json:"ed25519_signature,omitempty"`
}

// RegistrationSignatureContext prefixes the bytes a DeviceRegistration's
// Ed25519Signature covers, so the signature means nothing anywhere else.
const RegistrationSignatureContext = "MAMA-device-registration-v1"

// RegistrationSignedMessage is what DeviceRegistration.Ed25519Signature
// signs: the ASCII context, then the UTF-8 bytes of deviceID, no separator.
func RegistrationSignedMessage(deviceID string) []byte {
	return append([]byte(RegistrationSignatureContext), deviceID...)
}

// RemoteConfig is server -> phone, the reply to GET /config; the phone caches
// it and works offline from the cache. Keys are std Base64 of 32 bytes in
// JSON. SMSNumber is E.164 and omitted when the server has none.
type RemoteConfig struct {
	SMSNumber              string `json:"sms_number,omitempty"`
	ServerEd25519PublicKey []byte `json:"server_ed25519_public_key"`
	ServerX25519PublicKey  []byte `json:"server_x25519_public_key"`
}

// AlertEvent is phone -> server when the person triggers an alert. The phone
// generates IncidentID (a UUID), so a retried event is the same incident.
// Points holds at least one fix, oldest first; the last one's speed picks
// the branch. DeviceState is the phone's permissions at the gesture (#125),
// nil from an app that sends none.
type AlertEvent struct {
	IncidentID  string       `json:"incident_id"`
	DeviceID    string       `json:"device_id"`
	Trigger     Trigger      `json:"trigger"`
	TriggeredAt time.Time    `json:"triggered_at"`
	Points      []Point      `json:"points"`
	HasAudio    bool         `json:"has_audio"`
	DeviceState *DeviceState `json:"device_state,omitempty"`
}

// Permission names, the keys of DeviceState.Permissions and the values of
// PermissionEvent.Permission (#125). Kotlin mirrors them in
// com.team27.mama.contract.PermissionKeys.
const (
	PermissionLocationFine         = "location_fine"         // precise location
	PermissionLocationBackground   = "location_background"   // location "all the time"
	PermissionMicrophone           = "microphone"            // record audio
	PermissionContacts             = "contacts"              // read and write, to save "Mamá"
	PermissionSMS                  = "sms"                   // send, for the offline emergency
	PermissionNotifications        = "notifications"         // the app's notifications are on
	PermissionAccessibilityService = "accessibility_service" // the volume gesture's service is on
	PermissionBatteryUnrestricted  = "battery_unrestricted"  // battery optimization is off for the app
	PermissionFullScreenIntent     = "full_screen_intent"    // Android 14+ only
	PermissionHibernationExempt    = "hibernation_exempt"    // Android 11+ only: unused-app restrictions are off
)

// DeviceState is what the phone's permissions were when the person
// triggered the alert (#125). Permissions maps a Permission* name to whether
// it was granted; a permission that does not apply to the phone (its Android
// version has no such thing) is left out. The server stores it with the
// incident and never rejects an alert because of it: see UnmarshalJSON.
type DeviceState struct {
	CapturedAt  time.Time       `json:"captured_at,omitzero"` // phone clock; zero only when the phone sent a malformed one
	Permissions map[string]bool `json:"permissions"`
}

// UnmarshalJSON never fails, because an alert must never be rejected over a
// diagnostic: unknown fields are ignored, a permission that is null or not a
// boolean is dropped, and a missing or malformed captured_at stays zero (and
// is left out when encoded). A device_state that is not an object at all
// decodes to an empty one.
func (s *DeviceState) UnmarshalJSON(raw []byte) error {
	*s = DeviceState{}
	var loose struct {
		CapturedAt  json.RawMessage            `json:"captured_at"`
		Permissions map[string]json.RawMessage `json:"permissions"`
	}
	if json.Unmarshal(raw, &loose) != nil {
		return nil
	}
	_ = json.Unmarshal(loose.CapturedAt, &s.CapturedAt)
	for name, value := range loose.Permissions {
		var granted *bool
		if json.Unmarshal(value, &granted) == nil && granted != nil {
			if s.Permissions == nil {
				s.Permissions = make(map[string]bool, len(loose.Permissions))
			}
			s.Permissions[name] = *granted
		}
	}
	return nil
}

// PermissionEvents is phone -> server, POST /devices/{device_id}/permissions
// (#125): when each permission was granted or lost, as the phone saw it. The
// phone sends the whole list at the end of first-run setup, then only what
// changed, when the app opens or an alert is triggered.
type PermissionEvents struct {
	Events []PermissionEvent `json:"events"`
}

// PermissionEvent is one change. At is the phone clock; a resent event
// (same device, Permission and At) is a no-op on the server.
type PermissionEvent struct {
	Permission string    `json:"permission"` // a Permission* name
	Granted    bool      `json:"granted"`
	At         time.Time `json:"at"`
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

// IncidentCancel is phone -> server: the person called off an open incident
// ("Fue sin querer"), sent to POST /incidents/{incident_id}/cancel. The same
// two ids as LocationUpdate prove which device owns the incident.
type IncidentCancel struct {
	IncidentID string `json:"incident_id"`
	DeviceID   string `json:"device_id"`
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
// Values are any JSON: ElevenLabs adds system__* variables, some numeric.
type ConversationInitiationClientData struct {
	DynamicVariables map[string]any `json:"dynamic_variables"`
}

// Dynamic variables the call engine sends with every "Mamá" call, the
// phone call and the WebRTC fallback alike. Every one always has a value,
// since ElevenLabs refuses a conversation whose prompt names a missing one.
const (
	// DynamicVarIncidentID ties a call to its incident; the webhook handler
	// reads it back.
	DynamicVarIncidentID = "incident_id"
	// DynamicVarName is the person's user_name, or "mi amor" when unset.
	DynamicVarName = "nombre"
	// DynamicVarDuressPhrase is the device's duress_phrase.
	DynamicVarDuressPhrase = "frase_alerta"
	// DynamicVarSafePhrase is the device's safe_phrase, or "" when unset.
	DynamicVarSafePhrase = "frase_segura"
)

// IncidentID returns the incident the call belongs to, or "" when missing or
// not a string.
func (w CallWebhook) IncidentID() string {
	id, _ := w.Data.ClientData.DynamicVariables[DynamicVarIncidentID].(string)
	return id
}

// NotifierMessage is server -> notifier: the one message the family gets for
// an incident. It carries the location and the verdict. The incident report
// link (#76) is not part of it: the notifier adds it to the text.
// Verdict and Reason are empty when no call happened (immediate_alert,
// silent_tracker). IncidentID is the dedupe key: one message per incident.
// Reporter is the reporting device's own phone_number; the notifier writes to
// it only with NOTIFY_SELF (#102, demo only). Ficha is left out when the call
// has none (no_duress, no call, or the extraction failed).
type NotifierMessage struct {
	IncidentID  string    `json:"incident_id"`
	To          []string  `json:"to"`                 // E.164 family numbers
	Reporter    string    `json:"reporter,omitempty"` // E.164, the device's own number
	Branch      Branch    `json:"branch"`
	Location    Point     `json:"location"`
	Verdict     Verdict   `json:"verdict,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	TriggeredAt time.Time `json:"triggered_at"`
	// Ficha is the context of the call behind the verdict (calls.ficha,
	// #106), when one was extracted. Only the WhatsApp text shows it.
	Ficha *CallContext `json:"ficha,omitempty"`
}

// CallContext is what an LLM took from a finished call's transcript (#106),
// stored in calls.ficha. It is not the incident report's ficha.json (#76).
// Every field is CallContextUnknown when the call did not say it; the
// extraction never invents one.
type CallContext struct {
	Lugar     string `json:"lugar"`     // where the person said they are
	Personas  string `json:"personas"`  // who is with them
	Vehiculo  string `json:"vehiculo"`  // the vehicle they are in
	Rumbo     string `json:"rumbo"`     // where they are heading
	Situacion string `json:"situacion"` // one sentence: what seems to be happening
	Confianza string `json:"confianza"` // ConfianzaBaja, ConfianzaMedia or ConfianzaAlta
}

// CallContextUnknown is a CallContext field the call did not say.
const CallContextUnknown = "no se sabe"

// CallContext.Confianza values.
const (
	ConfianzaBaja  = "baja"
	ConfianzaMedia = "media"
	ConfianzaAlta  = "alta"
)
