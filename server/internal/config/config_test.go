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
		"PORT":                       c.Port,
		"DATABASE_URL":               c.DatabaseURL,
		"ELEVENLABS_API_KEY":         c.ElevenLabs.APIKey,
		"ELEVENLABS_AGENT_ID":        c.ElevenLabs.AgentID,
		"ELEVENLABS_PHONE_NUMBER_ID": c.ElevenLabs.PhoneNumberID,
		"ELEVENLABS_WEBHOOK_SECRET":  c.ElevenLabs.WebhookSecret,
		"TWILIO_ACCOUNT_SID":         c.Twilio.AccountSID,
		"TWILIO_AUTH_TOKEN":          c.Twilio.AuthToken,
		"TWILIO_PHONE_NUMBER":        c.Twilio.PhoneNumber,
		"KAPSO_API_KEY":              c.Kapso.APIKey,
		"KAPSO_PHONE_NUMBER_ID":      c.Kapso.PhoneNumberID,
		"ANTHROPIC_API_KEY":          c.Anthropic.APIKey,
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
