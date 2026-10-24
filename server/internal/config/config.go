// Package config reads the server's settings and service keys from the
// environment. Keys live in the team vault and the Render env, never in the
// repo (see README "Secrets"). Load never fails: Missing reports what is unset
// and each feature decides whether it can run without it.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultPort is used when PORT is unset.
const DefaultPort = "8080"

// DefaultReportTTLDays is how long an incident report link lives when
// INCIDENT_REPORT_TTL_DAYS is unset or not a positive number.
const DefaultReportTTLDays = 30

// NOTIFIER values: the channel the family alert goes out on.
const (
	NotifierKapso = "kapso" // WhatsApp through Kapso, the default
	NotifierSMS   = "sms"   // SMS through Twilio, the fallback
)

// Config holds every env var the server reads.
type Config struct {
	Port        string // PORT
	DatabaseURL string // DATABASE_URL
	Notifier    string // NOTIFIER, lowercased; NotifierKapso when unset
	// PublicURL is PUBLIC_URL, else Render's RENDER_EXTERNAL_URL: the
	// server's public base URL, optional. Log lines that point the operator
	// at a page (the call fallback) use it; blank logs the bare path.
	PublicURL string
	// CallWhenStill is CALL_WHEN_STILL, "true" or "1"; anything else or
	// unset is off. On, a still phone gets the call branch instead of the
	// immediate alert, so a person standing still still gets "Mamá".
	CallWhenStill bool

	ElevenLabs ElevenLabs
	Twilio     Twilio
	Kapso      Kapso
	Anthropic  Anthropic
	Emergency  Emergency
	Report     Report
}

// ElevenLabs is the voice agent that places the "Mamá" call through its
// native Twilio integration (POST /v1/convai/twilio/outbound-call).
type ElevenLabs struct {
	APIKey        string // ELEVENLABS_API_KEY, sent as the xi-api-key header
	AgentID       string // ELEVENLABS_AGENT_ID, the agent_id of the "Mamá" agent
	PhoneNumberID string // ELEVENLABS_PHONE_NUMBER_ID, agent_phone_number_id of the imported Twilio number
	WebhookSecret string // ELEVENLABS_WEBHOOK_SECRET, HMAC secret of the post-call webhook
	// VoiceID is ELEVENLABS_VOICE_ID, optional: when set, each call
	// overrides the agent's voice with it (Mariana's cloned voice, #35).
	// Blank keeps the voice set on the agent.
	VoiceID string
	// CallSpikeToken is CALL_SPIKE_TOKEN, optional: the bearer token of the
	// #11 spike route POST /spike/call. Blank leaves the route off, so a
	// deploy never exposes an open "call any number" endpoint.
	CallSpikeToken string
	// ForceCallFailure is CALL_FORCE_FAILURE, "true" or "1": every call
	// fails without dialing, so the retry and the WebRTC fallback can be
	// rehearsed (#23). Off by default.
	ForceCallFailure bool
}

// Twilio owns the caller number, sends the SMS fallback and receives the
// offline emergency texts (#58).
type Twilio struct {
	AccountSID  string // TWILIO_ACCOUNT_SID, AC...
	AuthToken   string // TWILIO_AUTH_TOKEN, also the key of X-Twilio-Signature
	PhoneNumber string // TWILIO_PHONE_NUMBER, E.164, e.g. +1...
	// WebhookBaseURL is TWILIO_WEBHOOK_BASE_URL, optional: the public origin
	// Twilio posts the inbound SMS webhook to (e.g. https://mama.example.com),
	// which the signature covers. Write it exactly as in the Twilio console,
	// without a default port (:443): the signed URL must match byte for byte.
	// Blank leaves POST /sms/inbound/twilio off.
	WebhookBaseURL string
}

// Kapso sends the WhatsApp alert to the family.
type Kapso struct {
	APIKey        string // KAPSO_API_KEY, sent as the X-API-Key header
	PhoneNumberID string // KAPSO_PHONE_NUMBER_ID, the WhatsApp phone_number_id
}

// Anthropic is the LLM step of the duress detector (#22). Without a key the
// detector runs rules only.
type Anthropic struct {
	APIKey      string // ANTHROPIC_API_KEY, sent as the x-api-key header
	DuressModel string // DURESS_LLM_MODEL, optional; blank = duress.DefaultModel
}

// Emergency is the offline emergency channel (#55): the central SMS number the
// phones text and the server key pair they pin. Both keys are standard Base64
// of 32 raw bytes; serverkeys validates them. The whole channel is an extra
// behind Enabled, off by default.
type Emergency struct {
	Enabled          bool   // EMERGENCY_OFFLINE_ENABLED, "true" or "1"; anything else or unset = off
	SMSNumber        string // EMERGENCY_SMS_NUMBER, E.164; blank = not served in GET /config
	Ed25519Seed      string // EMERGENCY_ED25519_SEED, signs offline acks
	X25519PrivateKey string // EMERGENCY_X25519_PRIVATE_KEY, opens mesh sealed boxes
}

// Report is the incident report page (#76): the private link GET /i/{token},
// the call recording and the seal. An extra behind Enabled, off by default.
// The seal uses EMERGENCY_ED25519_SEED; without it the page says "Sin sellar".
type Report struct {
	Enabled bool // INCIDENT_REPORT_ENABLED, "true" or "1"; anything else or unset = off
	TTLDays int  // INCIDENT_REPORT_TTL_DAYS, how long a link lives; DefaultReportTTLDays when unset or invalid
	// DemoKey is DEMO_KEY, optional: the key of the presenter's shortcuts
	// GET /demo/latest and /demo/next (the newest incident's report). Blank
	// leaves them off (404).
	DemoKey string
}

// Load reads the config from the process environment.
func Load() Config { return LoadFrom(os.Getenv) }

// LoadFrom reads the config through getenv, so tests need no real env.
// Values are trimmed; PORT falls back to DefaultPort and NOTIFIER to
// NotifierKapso. NOTIFIER is not validated here: notify.NewSender rejects an
// unknown value.
func LoadFrom(getenv func(string) string) Config {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	c := Config{
		Port:          get("PORT"),
		DatabaseURL:   get("DATABASE_URL"),
		Notifier:      strings.ToLower(get("NOTIFIER")),
		PublicURL:     get("PUBLIC_URL"),
		CallWhenStill: isOn(get("CALL_WHEN_STILL")),
		ElevenLabs: ElevenLabs{
			APIKey:           get("ELEVENLABS_API_KEY"),
			AgentID:          get("ELEVENLABS_AGENT_ID"),
			PhoneNumberID:    get("ELEVENLABS_PHONE_NUMBER_ID"),
			WebhookSecret:    get("ELEVENLABS_WEBHOOK_SECRET"),
			VoiceID:          get("ELEVENLABS_VOICE_ID"),
			CallSpikeToken:   get("CALL_SPIKE_TOKEN"),
			ForceCallFailure: isOn(get("CALL_FORCE_FAILURE")),
		},
		Twilio: Twilio{
			AccountSID:     get("TWILIO_ACCOUNT_SID"),
			AuthToken:      get("TWILIO_AUTH_TOKEN"),
			PhoneNumber:    get("TWILIO_PHONE_NUMBER"),
			WebhookBaseURL: get("TWILIO_WEBHOOK_BASE_URL"),
		},
		Kapso: Kapso{
			APIKey:        get("KAPSO_API_KEY"),
			PhoneNumberID: get("KAPSO_PHONE_NUMBER_ID"),
		},
		Anthropic: Anthropic{
			APIKey:      get("ANTHROPIC_API_KEY"),
			DuressModel: get("DURESS_LLM_MODEL"),
		},
		Emergency: Emergency{
			Enabled:          isOn(get("EMERGENCY_OFFLINE_ENABLED")),
			SMSNumber:        get("EMERGENCY_SMS_NUMBER"),
			Ed25519Seed:      get("EMERGENCY_ED25519_SEED"),
			X25519PrivateKey: get("EMERGENCY_X25519_PRIVATE_KEY"),
		},
		Report: Report{
			Enabled: isOn(get("INCIDENT_REPORT_ENABLED")),
			TTLDays: positiveInt(get("INCIDENT_REPORT_TTL_DAYS"), DefaultReportTTLDays),
			DemoKey: get("DEMO_KEY"),
		},
	}
	if c.Port == "" {
		c.Port = DefaultPort
	}
	if c.Notifier == "" {
		c.Notifier = NotifierKapso
	}
	if c.PublicURL == "" {
		c.PublicURL = get("RENDER_EXTERNAL_URL")
	}
	return c
}

// positiveInt parses v as a positive int, else returns def.
func positiveInt(v string, def int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// isOn reports whether a flag value is "true" (any case) or "1". Anything
// else, a typo included, leaves the flag off.
func isOn(v string) bool { return v == "1" || strings.EqualFold(v, "true") }

// vars lists every env var name with its value, in a fixed order. PORT and
// NOTIFIER are left out because they always have a value, DURESS_LLM_MODEL,
// ELEVENLABS_VOICE_ID, CALL_SPIKE_TOKEN and TWILIO_WEBHOOK_BASE_URL because
// they are optional.
func (c Config) vars() []struct{ name, value string } {
	return append([]struct{ name, value string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"ELEVENLABS_API_KEY", c.ElevenLabs.APIKey},
		{"ELEVENLABS_AGENT_ID", c.ElevenLabs.AgentID},
		{"ELEVENLABS_PHONE_NUMBER_ID", c.ElevenLabs.PhoneNumberID},
		{"ELEVENLABS_WEBHOOK_SECRET", c.ElevenLabs.WebhookSecret},
		{"TWILIO_ACCOUNT_SID", c.Twilio.AccountSID},
		{"TWILIO_AUTH_TOKEN", c.Twilio.AuthToken},
		{"TWILIO_PHONE_NUMBER", c.Twilio.PhoneNumber},
		{"KAPSO_API_KEY", c.Kapso.APIKey},
		{"KAPSO_PHONE_NUMBER_ID", c.Kapso.PhoneNumberID},
		{"ANTHROPIC_API_KEY", c.Anthropic.APIKey},
	}, c.Emergency.vars()...)
}

func (e Emergency) vars() []struct{ name, value string } {
	return []struct{ name, value string }{
		{"EMERGENCY_SMS_NUMBER", e.SMSNumber},
		{"EMERGENCY_ED25519_SEED", e.Ed25519Seed},
		{"EMERGENCY_X25519_PRIVATE_KEY", e.X25519PrivateKey},
	}
}

// Missing returns the names of the env vars that are unset or blank. It never
// returns values, so it is safe to log.
func (c Config) Missing() []string {
	var missing []string
	for _, v := range c.vars() {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}
	return missing
}

// String reports which vars are set without printing any value, so logging a
// Config by mistake cannot leak a key.
func (c Config) String() string {
	// NOTIFIER is a channel name, not a secret, so its value is shown.
	parts := append([]string{"PORT=" + c.Port, "NOTIFIER=" + c.Notifier,
		fmt.Sprintf("EMERGENCY_OFFLINE_ENABLED=%t", c.Emergency.Enabled),
		fmt.Sprintf("CALL_WHEN_STILL=%t", c.CallWhenStill),
		fmt.Sprintf("INCIDENT_REPORT_ENABLED=%t", c.Report.Enabled)}, setOrUnset(c.vars())...)
	return fmt.Sprintf("config{%s}", strings.Join(parts, " "))
}

// GoString keeps %#v as safe as %v.
func (c Config) GoString() string { return c.String() }

// String reports which emergency vars are set, never their values: the
// Ed25519 seed and the X25519 private key are the server's secret keys.
func (e Emergency) String() string {
	return fmt.Sprintf("emergency{%s}", strings.Join(setOrUnset(e.vars()), " "))
}

// GoString keeps %#v as safe as %v.
func (e Emergency) GoString() string { return e.String() }

// setOrUnset renders each var as NAME=set or NAME=unset.
func setOrUnset(vars []struct{ name, value string }) []string {
	parts := make([]string, 0, len(vars))
	for _, v := range vars {
		state := "set"
		if v.value == "" {
			state = "unset"
		}
		parts = append(parts, v.name+"="+state)
	}
	return parts
}
