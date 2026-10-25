package contract

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Each canonical sample in testdata/ must parse into its Go type with no
// unknown field and re-emit as the same JSON. The Kotlin ContractRoundTripTest
// reads the same files.
func TestRoundTripSamples(t *testing.T) {
	cases := []struct {
		file string
		into func() any
	}{
		{"device_registration.json", func() any { return new(DeviceRegistration) }},
		{"remote_config.json", func() any { return new(RemoteConfig) }},
		{"alert_event.json", func() any { return new(AlertEvent) }},
		{"alert_response.json", func() any { return new(AlertResponse) }},
		{"location_update.json", func() any { return new(LocationUpdate) }},
		{"call_webhook.json", func() any { return new(CallWebhook) }},
		{"notifier_message.json", func() any { return new(NotifierMessage) }},
		{"permission_events.json", func() any { return new(PermissionEvents) }},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", c.file))
			if err != nil {
				t.Fatal(err)
			}
			v := c.into()
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(v); err != nil {
				t.Fatalf("decode: %v", err)
			}
			out, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var want, got any
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("re-emitted JSON differs\nwant %s\n got %s", raw, out)
			}
		})
	}
}

func TestEverySampleHasACase(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 8 {
		t.Fatalf("testdata has %d samples, TestRoundTripSamples covers 8: add the new one there", len(files))
	}
}

// The sample's ed25519_signature is a real signature by its key over its
// device_id, so the intake tests and the app can register it as is.
func TestDeviceRegistrationSampleSignatureVerifies(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "device_registration.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reg DeviceRegistration
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.Ed25519PublicKey) != ed25519.PublicKeySize || len(reg.Ed25519Signature) != ed25519.SignatureSize {
		t.Fatalf("key %d bytes, signature %d bytes", len(reg.Ed25519PublicKey), len(reg.Ed25519Signature))
	}
	if !ed25519.Verify(reg.Ed25519PublicKey, RegistrationSignedMessage(reg.DeviceID), reg.Ed25519Signature) {
		t.Fatal("sample signature does not verify over RegistrationSignedMessage(device_id)")
	}
	if ed25519.Verify(reg.Ed25519PublicKey, RegistrationSignedMessage(reg.DeviceID+"x"), reg.Ed25519Signature) {
		t.Fatal("sample signature verifies over another device_id")
	}
}

func TestRegistrationSignedMessage(t *testing.T) {
	if got, want := string(RegistrationSignedMessage("abc")), "MAMA-device-registration-v1abc"; got != want {
		t.Fatalf("RegistrationSignedMessage = %q, want %q", got, want)
	}
}

func ptr(f float64) *float64 { return &f }

func TestChooseBranch(t *testing.T) {
	cases := []struct {
		name  string
		speed *float64
		want  Branch
	}{
		{"unknown speed", nil, BranchCall},
		{"negative speed", ptr(-1), BranchCall},
		{"NaN speed", ptr(math.NaN()), BranchCall},
		{"standing", ptr(0), BranchImmediateAlert},
		{"just under still", ptr(0.99), BranchImmediateAlert},
		{"still threshold is a walk", ptr(StillBelowMPS), BranchCall},
		{"walking", ptr(1.4), BranchCall},
		{"just under fast", ptr(6.99), BranchCall},
		{"fast threshold", ptr(FastFromMPS), BranchSilentTracker},
		{"car", ptr(15), BranchSilentTracker},
	}
	for _, c := range cases {
		if got := ChooseBranch(c.speed); got != c.want {
			t.Errorf("%s: ChooseBranch = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAlertEventBranchUsesNewestPoint(t *testing.T) {
	e := AlertEvent{Points: []Point{{SpeedMPS: ptr(15)}, {SpeedMPS: ptr(0.2)}}}
	if got := e.Branch(); got != BranchImmediateAlert {
		t.Fatalf("Branch = %q, want %q", got, BranchImmediateAlert)
	}
	if got := (AlertEvent{}).Branch(); got != BranchCall {
		t.Fatalf("Branch with no points = %q, want %q", got, BranchCall)
	}
}

func TestCallWebhookIncidentID(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "call_webhook.json"))
	if err != nil {
		t.Fatal(err)
	}
	var w CallWebhook
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if got, want := w.IncidentID(), "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"; got != want {
		t.Fatalf("IncidentID = %q, want %q", got, want)
	}
}

// An alert is never rejected over its device_state (#125): whatever the
// phone sends there decodes, even under the intake's strict decoder, and
// keeps only the well-formed permissions.
func TestDeviceStateDecodesLeniently(t *testing.T) {
	cases := []struct {
		name  string
		state string
		want  DeviceState
	}{
		{"null permission and a non-boolean are dropped", `{"captured_at":"2026-10-24T09:10:00.251Z","permissions":{"sms":true,"full_screen_intent":null,"microphone":"yes"}}`,
			DeviceState{CapturedAt: time.Date(2026, 10, 24, 9, 10, 0, 251_000_000, time.UTC), Permissions: map[string]bool{"sms": true}}},
		{"unknown fields are ignored", `{"captured_at":"2026-10-24T09:10:00Z","permissions":{"location_fine":false},"android":36}`,
			DeviceState{CapturedAt: time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC), Permissions: map[string]bool{"location_fine": false}}},
		{"malformed captured_at stays zero", `{"captured_at":"ayer","permissions":{"sms":false}}`,
			DeviceState{Permissions: map[string]bool{"sms": false}}},
		{"not an object", `"todo bien"`, DeviceState{}},
		{"permissions not an object", `{"permissions":[true]}`, DeviceState{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := `{"incident_id":"9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f","device_id":"d","trigger":"in_app",` +
				`"triggered_at":"2026-10-24T09:10:00Z","points":[],"has_audio":false,"device_state":` + c.state + `}`
			dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
			dec.DisallowUnknownFields()
			var ev AlertEvent
			if err := dec.Decode(&ev); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if ev.DeviceState == nil || !reflect.DeepEqual(*ev.DeviceState, c.want) {
				t.Fatalf("device_state = %+v, want %+v", ev.DeviceState, c.want)
			}
		})
	}
}

// An alert from an app without #125 has no device_state, and none is sent back.
func TestAlertEventWithoutDeviceState(t *testing.T) {
	var ev AlertEvent
	if err := json.Unmarshal([]byte(`{"incident_id":"i","device_id":"d","trigger":"in_app","triggered_at":"2026-10-24T09:10:00Z","points":[],"has_audio":false}`), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.DeviceState != nil {
		t.Fatalf("device_state = %+v, want nil", ev.DeviceState)
	}
	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("device_state")) {
		t.Fatalf("re-emitted %s carries device_state", out)
	}
}

// A malformed captured_at is left out when the snapshot is stored again.
func TestDeviceStateWithoutCaptureTimeLeavesItOut(t *testing.T) {
	out, err := json.Marshal(DeviceState{Permissions: map[string]bool{"sms": true}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != `{"permissions":{"sms":true}}` {
		t.Fatalf("encoded %s", got)
	}
}
