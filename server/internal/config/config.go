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
	"time"
)

// DefaultPort is used when PORT is unset.
const DefaultPort = "8080"

// DefaultReportTTLDays is how long an incident report link lives when
// INCIDENT_REPORT_TTL_DAYS is unset or not a positive number.
const DefaultReportTTLDays = 30

// LimitWindow is the sliding window every Limits cap counts over.
const LimitWindow = 10 * time.Minute

// Defaults of the Limits caps, per LimitWindow, when their env var is unset
// or not a positive number. They leave room for a rehearsal loop: several
// alerts to the same phone within minutes.
const (
	// DefaultCallsPerNumber is 3 incidents whose first call fails and is
	// retried, or 6 that connect at once.
	DefaultCallsPerNumber = 6
	DefaultCallsTotal     = 40
	// An incident sends one message to each family number (the notifier
	// claims it once), so DefaultMessagesPerNumber is 10 alerts reaching the
	// same relative, from one phone or from several that list them.
	DefaultMessagesPerNumber = 10
	// DefaultMessagesTotal is 12 alerts to 5 family numbers each.
	DefaultMessagesTotal = 60
)

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
	// NotifySelf is NOTIFY_SELF, "true" or "1"; off by default. On, the
	// reporting device's own phone_number also gets the alert (#102), so the
	// demo shows it arriving on the same phone. Demo only: in production the
	// aggressor may be looking at that phone.
	NotifySelf bool

	ElevenLabs ElevenLabs
	Twilio     Twilio
	Kapso      Kapso
	Anthropic  Anthropic
	Emergency  Emergency
	Report     Report
	Limits     Limits
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
	// FallbackToken is FALLBACK_TOKEN, optional: the secret the WebRTC
	// fallback page wants as ?token= before it hands the browser call the
	// duress and safe phrases (#104). Blank keeps the page open but sends the
	// phrases empty.
	FallbackToken string
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
	// WebhookSecret is KAPSO_WEBHOOK_SECRET, optional: the secret of the
	// phone number's webhook that reports message statuses (sent, delivered,
	// read, failed) to POST /webhooks/kapso for the incident report (#76).
	// Blank leaves the route off.
	WebhookSecret string
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
	// CentralKey is CENTRAL_KEY, optional: the secret of the central
	// dashboard GET /central/ (#99), which sees every incident. Given once
	// as ?key=, it becomes a cookie. Blank leaves the dashboard off (404).
	CentralKey string
	// MapKey is CARTO_BASEMAPS_KEY, optional: the free key of CARTO's
	// basemaps (carto.com/basemaps/apikey), which the route map's tiles
	// need. Blank leaves the route as the drawing, with no map. It is not a
	// secret: the reader's browser sends it with every tile.
	MapKey string
}

// Limits caps the calls the server places and the family messages it sends,
// per number and in total, over LimitWindow (package ratelimit). There is no
// auth on /devices or /incidents, so without them a loop of fresh incidents
// places a paid call, and messages every family number, each time. The
// counts live in memory: a restart, such as a deploy or a change of these
// vars on Render, resets them.
type Limits struct {
	CallsPerNumber    int // CALL_LIMIT_PER_NUMBER, calls placed to one number, retries included; DefaultCallsPerNumber when unset or invalid
	CallsTotal        int // CALL_LIMIT_TOTAL, calls placed to every number together; DefaultCallsTotal when unset or invalid
	MessagesPerNumber int // NOTIFY_LIMIT_PER_NUMBER, family messages sent to one number; DefaultMessagesPerNumber when unset or invalid
	MessagesTotal     int // NOTIFY_LIMIT_TOTAL, family messages sent to every number together; DefaultMessagesTotal when unset or invalid
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
		NotifySelf:    isOn(get("NOTIFY_SELF")),
		ElevenLabs: ElevenLabs{
			APIKey:           get("ELEVENLABS_API_KEY"),
			AgentID:          get("ELEVENLABS_AGENT_ID"),
			PhoneNumberID:    get("ELEVENLABS_PHONE_NUMBER_ID"),
			WebhookSecret:    get("ELEVENLABS_WEBHOOK_SECRET"),
			VoiceID:          get("ELEVENLABS_VOICE_ID"),
			CallSpikeToken:   get("CALL_SPIKE_TOKEN"),
			FallbackToken:    get("FALLBACK_TOKEN"),
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
			WebhookSecret: get("KAPSO_WEBHOOK_SECRET"),
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
			Enabled:    isOn(get("INCIDENT_REPORT_ENABLED")),
			TTLDays:    positiveInt(get("INCIDENT_REPORT_TTL_DAYS"), DefaultReportTTLDays),
			DemoKey:    get("DEMO_KEY"),
			CentralKey: get("CENTRAL_KEY"),
			MapKey:     get("CARTO_BASEMAPS_KEY"),
		},
		Limits: Limits{
			CallsPerNumber:    positiveInt(get("CALL_LIMIT_PER_NUMBER"), DefaultCallsPerNumber),
			CallsTotal:        positiveInt(get("CALL_LIMIT_TOTAL"), DefaultCallsTotal),
			MessagesPerNumber: positiveInt(get("NOTIFY_LIMIT_PER_NUMBER"), DefaultMessagesPerNumber),
			MessagesTotal:     positiveInt(get("NOTIFY_LIMIT_TOTAL"), DefaultMessagesTotal),
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
		fmt.Sprintf("NOTIFY_SELF=%t", c.NotifySelf),
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
