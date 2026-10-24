// Package config reads the server's settings and service keys from the
// environment. Keys live in the team vault and the Render env, never in the
// repo (see README "Secrets"). Load never fails: Missing reports what is unset
// and each feature decides whether it can run without it.
package config

import (
	"fmt"
	"os"
	"strings"
)

// DefaultPort is used when PORT is unset.
const DefaultPort = "8080"

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

	ElevenLabs ElevenLabs
	Twilio     Twilio
	Kapso      Kapso
	Anthropic  Anthropic
}

// ElevenLabs is the voice agent that places the "Mamá" call through its
// native Twilio integration (POST /v1/convai/twilio/outbound-call).
type ElevenLabs struct {
	APIKey        string // ELEVENLABS_API_KEY, sent as the xi-api-key header
	AgentID       string // ELEVENLABS_AGENT_ID, the agent_id of the "Mamá" agent
	PhoneNumberID string // ELEVENLABS_PHONE_NUMBER_ID, agent_phone_number_id of the imported Twilio number
	WebhookSecret string // ELEVENLABS_WEBHOOK_SECRET, HMAC secret of the post-call webhook
}

// Twilio owns the caller number and sends the SMS fallback.
type Twilio struct {
	AccountSID  string // TWILIO_ACCOUNT_SID, AC...
	AuthToken   string // TWILIO_AUTH_TOKEN
	PhoneNumber string // TWILIO_PHONE_NUMBER, E.164, e.g. +1...
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

// Load reads the config from the process environment.
func Load() Config { return LoadFrom(os.Getenv) }

// LoadFrom reads the config through getenv, so tests need no real env.
// Values are trimmed; PORT falls back to DefaultPort and NOTIFIER to
// NotifierKapso. NOTIFIER is not validated here: notify.NewSender rejects an
// unknown value.
func LoadFrom(getenv func(string) string) Config {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	c := Config{
		Port:        get("PORT"),
		DatabaseURL: get("DATABASE_URL"),
		Notifier:    strings.ToLower(get("NOTIFIER")),
		ElevenLabs: ElevenLabs{
			APIKey:        get("ELEVENLABS_API_KEY"),
			AgentID:       get("ELEVENLABS_AGENT_ID"),
			PhoneNumberID: get("ELEVENLABS_PHONE_NUMBER_ID"),
			WebhookSecret: get("ELEVENLABS_WEBHOOK_SECRET"),
		},
		Twilio: Twilio{
			AccountSID:  get("TWILIO_ACCOUNT_SID"),
			AuthToken:   get("TWILIO_AUTH_TOKEN"),
			PhoneNumber: get("TWILIO_PHONE_NUMBER"),
		},
		Kapso: Kapso{
			APIKey:        get("KAPSO_API_KEY"),
			PhoneNumberID: get("KAPSO_PHONE_NUMBER_ID"),
		},
		Anthropic: Anthropic{
			APIKey:      get("ANTHROPIC_API_KEY"),
			DuressModel: get("DURESS_LLM_MODEL"),
		},
	}
	if c.Port == "" {
		c.Port = DefaultPort
	}
	if c.Notifier == "" {
		c.Notifier = NotifierKapso
	}
	return c
}

// vars lists every env var name with its value, in a fixed order. PORT and
// NOTIFIER are left out because they always have a value, DURESS_LLM_MODEL
// because it is optional.
func (c Config) vars() []struct{ name, value string } {
	return []struct{ name, value string }{
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
	parts := []string{"PORT=" + c.Port, "NOTIFIER=" + c.Notifier}
	for _, v := range c.vars() {
		state := "set"
		if v.value == "" {
			state = "unset"
		}
		parts = append(parts, v.name+"="+state)
	}
	return fmt.Sprintf("config{%s}", strings.Join(parts, " "))
}

// GoString keeps %#v as safe as %v.
func (c Config) GoString() string { return c.String() }
