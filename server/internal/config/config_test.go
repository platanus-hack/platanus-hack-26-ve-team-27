package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var allVars = []string{
	"DATABASE_URL",
	"ELEVENLABS_API_KEY", "ELEVENLABS_AGENT_ID", "ELEVENLABS_PHONE_NUMBER_ID", "ELEVENLABS_WEBHOOK_SECRET",
	"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER",
	"KAPSO_API_KEY", "KAPSO_PHONE_NUMBER_ID",
	"ANTHROPIC_API_KEY",
	"EMERGENCY_SMS_NUMBER", "EMERGENCY_ED25519_SEED", "EMERGENCY_X25519_PRIVATE_KEY",
}

func TestEmptyEnvDefaultsPortAndReportsEverythingMissing(t *testing.T) {
	c := LoadFrom(envFrom(nil))
	if c.Port != DefaultPort {
		t.Errorf("Port = %q, want %q", c.Port, DefaultPort)
	}
	if got := c.Missing(); !reflect.DeepEqual(got, allVars) {
		t.Errorf("Missing() = %v, want %v", got, allVars)
	}
}

func TestLoadReadsEveryVar(t *testing.T) {
	env := map[string]string{"PORT": "9000"}
	for _, name := range allVars {
		env[name] = "value-of-" + name
	}
	c := LoadFrom(envFrom(env))

	if len(c.Missing()) != 0 {
		t.Fatalf("Missing() = %v, want none", c.Missing())
	}
	checks := map[string]string{
		"PORT":                         c.Port,
		"DATABASE_URL":                 c.DatabaseURL,
		"ELEVENLABS_API_KEY":           c.ElevenLabs.APIKey,
		"ELEVENLABS_AGENT_ID":          c.ElevenLabs.AgentID,
		"ELEVENLABS_PHONE_NUMBER_ID":   c.ElevenLabs.PhoneNumberID,
		"ELEVENLABS_WEBHOOK_SECRET":    c.ElevenLabs.WebhookSecret,
		"TWILIO_ACCOUNT_SID":           c.Twilio.AccountSID,
		"TWILIO_AUTH_TOKEN":            c.Twilio.AuthToken,
		"TWILIO_PHONE_NUMBER":          c.Twilio.PhoneNumber,
		"KAPSO_API_KEY":                c.Kapso.APIKey,
		"KAPSO_PHONE_NUMBER_ID":        c.Kapso.PhoneNumberID,
		"ANTHROPIC_API_KEY":            c.Anthropic.APIKey,
		"EMERGENCY_SMS_NUMBER":         c.Emergency.SMSNumber,
		"EMERGENCY_ED25519_SEED":       c.Emergency.Ed25519Seed,
		"EMERGENCY_X25519_PRIVATE_KEY": c.Emergency.X25519PrivateKey,
	}
	for name, got := range checks {
		if got != env[name] {
			t.Errorf("%s = %q, want %q", name, got, env[name])
		}
	}
}

func TestBlankValuesCountAsMissing(t *testing.T) {
	c := LoadFrom(envFrom(map[string]string{"KAPSO_API_KEY": "   ", "PORT": " "}))
	if c.Port != DefaultPort {
		t.Errorf("Port = %q, want %q for a blank PORT", c.Port, DefaultPort)
	}
	found := false
	for _, name := range c.Missing() {
		found = found || name == "KAPSO_API_KEY"
	}
	if !found {
		t.Errorf("Missing() = %v, want it to include KAPSO_API_KEY", c.Missing())
	}
}

func TestLoadUsesProcessEnv(t *testing.T) {
	t.Setenv("TWILIO_PHONE_NUMBER", "+15550001111")
	if got := Load().Twilio.PhoneNumber; got != "+15550001111" {
		t.Errorf("Load().Twilio.PhoneNumber = %q, want +15550001111", got)
	}
}

func TestFormattingNeverPrintsValues(t *testing.T) {
	const secret = "planted-secret-value-123" // secret-scan:allow (test fixture)
	env := map[string]string{}
	for _, name := range allVars {
		env[name] = secret
	}
	c := LoadFrom(envFrom(env))
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(verb, c)
		if strings.Contains(out, secret) {
			t.Errorf("Sprintf(%q, cfg) leaks a value: %s", verb, out)
		}
		if !strings.Contains(out, "TWILIO_AUTH_TOKEN=set") {
			t.Errorf("Sprintf(%q, cfg) = %s, want it to show TWILIO_AUTH_TOKEN=set", verb, out)
		}
	}
}

func TestEmergencyFormattingNeverPrintsValues(t *testing.T) {
	const secret = "planted-emergency-secret-456" // secret-scan:allow (test fixture)
	e := Emergency{SMSNumber: secret, Ed25519Seed: secret}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(verb, e)
		if strings.Contains(out, secret) {
			t.Errorf("Sprintf(%q, emergency) leaks a value: %s", verb, out)
		}
		for _, want := range []string{"EMERGENCY_SMS_NUMBER=set", "EMERGENCY_ED25519_SEED=set", "EMERGENCY_X25519_PRIVATE_KEY=unset"} {
			if !strings.Contains(out, want) {
				t.Errorf("Sprintf(%q, emergency) = %s, want it to show %s", verb, out, want)
			}
		}
	}
}

func TestNotifierDefaultsToKapsoAndIsLowercased(t *testing.T) {
	if got := LoadFrom(envFrom(nil)).Notifier; got != NotifierKapso {
		t.Errorf("unset NOTIFIER = %q, want %q", got, NotifierKapso)
	}
	if got := LoadFrom(envFrom(map[string]string{"NOTIFIER": " SMS "})).Notifier; got != NotifierSMS {
		t.Errorf("NOTIFIER=\" SMS \" = %q, want %q", got, NotifierSMS)
	}
	// NOTIFIER always has a value, so it is never reported missing.
	for _, name := range LoadFrom(envFrom(nil)).Missing() {
		if name == "NOTIFIER" {
			t.Error("Missing() lists NOTIFIER")
		}
	}
}

func TestDuressModelIsOptional(t *testing.T) {
	c := LoadFrom(envFrom(map[string]string{"DURESS_LLM_MODEL": " claude-test-model "}))
	if c.Anthropic.DuressModel != "claude-test-model" {
		t.Errorf("DuressModel = %q, want claude-test-model", c.Anthropic.DuressModel)
	}
	for _, name := range LoadFrom(envFrom(nil)).Missing() {
		if name == "DURESS_LLM_MODEL" {
			t.Errorf("Missing() lists the optional DURESS_LLM_MODEL")
		}
	}
}

func TestCallSpikeVarsAreOptional(t *testing.T) {
	c := LoadFrom(envFrom(map[string]string{
		"ELEVENLABS_VOICE_ID": " voice123 ",
		"CALL_SPIKE_TOKEN":    " tok ",
	}))
	if c.ElevenLabs.VoiceID != "voice123" || c.ElevenLabs.CallSpikeToken != "tok" {
		t.Errorf("VoiceID, CallSpikeToken = %q, %q; want voice123, tok", c.ElevenLabs.VoiceID, c.ElevenLabs.CallSpikeToken)
	}
	for _, name := range LoadFrom(envFrom(nil)).Missing() {
		if name == "ELEVENLABS_VOICE_ID" || name == "CALL_SPIKE_TOKEN" {
			t.Errorf("Missing() lists the optional %s", name)
		}
	}
	if s := c.String(); strings.Contains(s, "tok") || strings.Contains(s, "voice123") {
		t.Errorf("String() leaks a value: %s", s)
	}
}

func TestEmergencyOfflineFlagIsOffUnlessTrueOr1(t *testing.T) {
	for v, want := range map[string]bool{"": false, "true": true, "TRUE": true, "1": true, " true ": true, "yes": false, "0": false, "false": false, "tru": false} {
		got := LoadFrom(envFrom(map[string]string{"EMERGENCY_OFFLINE_ENABLED": v})).Emergency.Enabled
		if got != want {
			t.Errorf("EMERGENCY_OFFLINE_ENABLED=%q: Enabled = %v, want %v", v, got, want)
		}
	}
}

func TestTwilioWebhookBaseURLIsOptional(t *testing.T) {
	c := LoadFrom(envFrom(map[string]string{"TWILIO_WEBHOOK_BASE_URL": " https://mama.example.com "}))
	if c.Twilio.WebhookBaseURL != "https://mama.example.com" {
		t.Errorf("WebhookBaseURL = %q, want https://mama.example.com", c.Twilio.WebhookBaseURL)
	}
	for _, name := range LoadFrom(envFrom(nil)).Missing() {
		if name == "TWILIO_WEBHOOK_BASE_URL" {
			t.Error("Missing() lists the optional TWILIO_WEBHOOK_BASE_URL")
		}
	}
}

func TestPublicURLPrefersPublicURLOverRender(t *testing.T) {
	if got := LoadFrom(envFrom(map[string]string{"RENDER_EXTERNAL_URL": "https://r.example"})).PublicURL; got != "https://r.example" {
		t.Errorf("PublicURL = %q, want the Render URL", got)
	}
	if got := LoadFrom(envFrom(map[string]string{"PUBLIC_URL": "https://p.example", "RENDER_EXTERNAL_URL": "https://r.example"})).PublicURL; got != "https://p.example" {
		t.Errorf("PublicURL = %q, want PUBLIC_URL", got)
	}
}

func TestCallForceFailureIsOffUnlessTrueOr1(t *testing.T) {
	for v, want := range map[string]bool{"": false, "yes": false, "true": true, "TRUE": true, "1": true} {
		if got := LoadFrom(envFrom(map[string]string{"CALL_FORCE_FAILURE": v})).ElevenLabs.ForceCallFailure; got != want {
			t.Errorf("CALL_FORCE_FAILURE=%q: ForceCallFailure = %v, want %v", v, got, want)
		}
	}
}

func TestReportIsOffByDefaultWithThirtyDayLinks(t *testing.T) {
	c := LoadFrom(envFrom(nil))
	if c.Report.Enabled || c.Report.TTLDays != DefaultReportTTLDays || DefaultReportTTLDays != 30 {
		t.Errorf("Report = %+v, want off with %d-day links", c.Report, DefaultReportTTLDays)
	}
	for _, name := range c.Missing() {
		if strings.HasPrefix(name, "INCIDENT_REPORT") {
			t.Errorf("Missing() lists the optional %s", name)
		}
	}
}

func TestReportReadsFlagAndTTL(t *testing.T) {
	for _, tc := range []struct {
		enabled, ttl string
		wantOn       bool
		wantTTL      int
	}{
		{"true", "7", true, 7},
		{"1", "", true, DefaultReportTTLDays},
		{"yes", "0", false, DefaultReportTTLDays},
		{"TRUE", "-3", true, DefaultReportTTLDays},
		{"", "abc", false, DefaultReportTTLDays},
	} {
		c := LoadFrom(envFrom(map[string]string{"INCIDENT_REPORT_ENABLED": tc.enabled, "INCIDENT_REPORT_TTL_DAYS": tc.ttl}))
		if c.Report.Enabled != tc.wantOn || c.Report.TTLDays != tc.wantTTL {
			t.Errorf("enabled=%q ttl=%q: Report = %+v, want on=%t ttl=%d", tc.enabled, tc.ttl, c.Report, tc.wantOn, tc.wantTTL)
		}
	}
}
