package call

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

const (
	samplePhrase = "dile a la tía Carmen que llego tarde"
	safePhrase   = "ya llegué a la casa, todo tranquilo"
	deviceVoice  = "voice_device_1"
	globalVoice  = "voice_global_1"
)

// fullCallee is a device with every setup field set.
var fullCallee = Callee{
	Phone: victimPhone, UserName: "Mariana", DuressPhrase: samplePhrase, SafePhrase: safePhrase, VoiceID: deviceVoice,
}

func TestCallVariablesAlwaysHoldEveryVariable(t *testing.T) {
	cases := []struct {
		name   string
		callee Callee
		want   map[string]string
	}{
		{"full setup", fullCallee, map[string]string{
			"incident_id": sampleIncident, "nombre": "Mariana", "frase_alerta": samplePhrase, "frase_segura": safePhrase,
		}},
		{"older app: no name, no safe phrase", Callee{Phone: victimPhone, DuressPhrase: samplePhrase}, map[string]string{
			"incident_id": sampleIncident, "nombre": "mi amor", "frase_alerta": samplePhrase, "frase_segura": "",
		}},
	}
	for _, tc := range cases {
		if got := CallVariables(sampleIncident, tc.callee); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: CallVariables = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The call the engine places, through the real Client against a fake
// ElevenLabs, carries the device's variables and its voice, or the global
// voice when the device has none.
func TestStartCallSendsTheDeviceSetupAndVoice(t *testing.T) {
	cases := []struct {
		name      string
		callee    Callee
		wantVars  map[string]string
		wantVoice string
	}{
		{"device voice", fullCallee, CallVariables(sampleIncident, fullCallee), deviceVoice},
		{"no device voice: the global one", Callee{DuressPhrase: samplePhrase}, map[string]string{
			"incident_id": sampleIncident, "nombre": "mi amor", "frase_alerta": samplePhrase, "frase_segura": "",
		}, globalVoice},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureLog(t)
			var body []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				_, _ = io.WriteString(w, `{"success":true,"message":"ok","conversation_id":"conv_1","callSid":"CA1"}`)
			}))
			t.Cleanup(srv.Close)
			client := &Client{BaseURL: srv.URL, APIKey: "key", AgentID: "agent_1", PhoneNumberID: "phnum_1", VoiceID: globalVoice}
			e := testEngine(&memStore{phone: victimPhone, callee: tc.callee}, client, nil, nil)
			e.OutcomeTimeout = 10 * time.Millisecond

			e.StartCall(context.Background(), sampleIncidentInput())

			var sent outboundCall
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Fatalf("body %s: %v", body, err)
			}
			if sent.ToNumber != victimPhone || sent.ClientData == nil || !reflect.DeepEqual(sent.ClientData.DynamicVariables, tc.wantVars) {
				t.Errorf("outbound call = %s, want variables %v", body, tc.wantVars)
			}
			if sent.ClientData == nil || sent.ClientData.ConfigOverride == nil || sent.ClientData.ConfigOverride.TTS.VoiceID != tc.wantVoice {
				t.Errorf("outbound call = %s, want voice %s", body, tc.wantVoice)
			}
			assertNoPhrases(t, out.String())
		})
	}
}

// When both calls fail, the WebRTC fallback hands the widget the same
// variables the phone call carried, and the log never shows a phrase.
func TestFallbackSendsTheSameVariables(t *testing.T) {
	out := captureLog(t)
	placer := &scriptPlacer{errs: []error{errors.New("boom"), errors.New("boom")}}
	e := testEngine(&memStore{phone: victimPhone, callee: fullCallee}, placer, nil, nil)

	e.StartCall(context.Background(), sampleIncidentInput())

	if len(placer.sent) != 2 {
		t.Fatalf("Place calls = %d, want 2", len(placer.sent))
	}
	want := CallVariables(sampleIncident, fullCallee)
	for i, c := range placer.sent {
		if !reflect.DeepEqual(c.Variables, want) || c.VoiceID != deviceVoice {
			t.Errorf("call %d = %+v, want variables %v and voice %s", i+1, c, want, deviceVoice)
		}
	}
	rec := httptest.NewRecorder()
	e.Fallback.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, FallbackPath, nil))
	if got := widgetVars(t, rec.Body.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("fallback variables = %v, want %v", got, want)
	}
	assertNoPhrases(t, out.String())
}

// A call that cannot be placed (not a +58 number) still opens the fallback
// with the device's variables.
func TestHopelessCallFallbackSendsTheDeviceVariables(t *testing.T) {
	captureLog(t)
	placer := &scriptPlacer{}
	e := testEngine(&memStore{phone: "+19005551234", callee: fullCallee}, placer, nil, nil)

	e.StartCall(context.Background(), sampleIncidentInput())

	rec := httptest.NewRecorder()
	e.Fallback.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, FallbackPath, nil))
	if got, want := widgetVars(t, rec.Body.String()), CallVariables(sampleIncident, fullCallee); !reflect.DeepEqual(got, want) {
		t.Errorf("fallback variables = %v, want %v", got, want)
	}
}

// A provider error that echoes the request does not leak the phrases into
// the error, which the engine logs.
func TestPlaceErrorsNeverQuoteThePhrases(t *testing.T) {
	vars := CallVariables(sampleIncident, fullCallee)
	echoed, _ := json.Marshal(map[string]any{"detail": "invalid", "input": vars})
	cases := []struct {
		name, body string
		status     int
	}{
		{"422 echoing the request", string(echoed), http.StatusUnprocessableEntity},
		{"success false naming them", `{"success":false,"message":"bad ` + samplePhrase + ` / ` + safePhrase + `"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := fakeElevenLabs(t, tc.status, tc.body)
			c := &Client{BaseURL: srv.URL, APIKey: "key", AgentID: "a", PhoneNumberID: "p"}
			_, err := c.Place(context.Background(), Call{To: victimPhone, Variables: vars})
			if err == nil {
				t.Fatal("want an error")
			}
			assertNoPhrases(t, err.Error())
		})
	}
}

// The spike route has no device, but its call still carries every variable.
func TestSpikeCallSendsEveryVariable(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"success":true,"message":"ok","conversation_id":"c","callSid":"s"}`)
	}))
	t.Cleanup(srv.Close)
	rt := NewRoutes(config.Config{ElevenLabs: config.ElevenLabs{
		APIKey: "key", AgentID: "agent_1", PhoneNumberID: "phnum_1", CallSpikeToken: "tok",
	}}, nil)
	rt.client.BaseURL = srv.URL
	mux := http.NewServeMux()
	rt.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/spike/call", strings.NewReader(`{"to":"`+victimPhone+`"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("spike call = %d %s", rec.Code, rec.Body)
	}
	var sent outboundCall
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if want := CallVariables("", Callee{}); sent.ClientData == nil || !reflect.DeepEqual(sent.ClientData.DynamicVariables, want) {
		t.Errorf("outbound call = %s, want variables %v", body, want)
	}
}

func assertNoPhrases(t *testing.T, s string) {
	t.Helper()
	for _, p := range []string{samplePhrase, safePhrase, "llegu\\u00e9", "t\\u00eda"} {
		if strings.Contains(s, p) {
			t.Errorf("%q leaks a phrase:\n%s", p, s)
		}
	}
}

// widgetVars returns the dynamic variables the fallback page hands the
// widget, or nil when it hands none.
func widgetVars(t *testing.T, body string) map[string]string {
	t.Helper()
	_, attr, ok := strings.Cut(body, `dynamic-variables="`)
	if !ok {
		return nil
	}
	attr, _, _ = strings.Cut(attr, `"`)
	var vars map[string]string
	if err := json.Unmarshal([]byte(html.UnescapeString(attr)), &vars); err != nil {
		t.Fatalf("dynamic-variables %q: %v", attr, err)
	}
	return vars
}

// Begin reads the device's setup with the call: blank for what is unset,
// the stored values once set.
func TestBeginReadsTheDeviceSetup(t *testing.T) {
	pool := callDB(t, victimPhone)
	store := NewDBStore(pool)
	ctx := context.Background()

	_, got, err := store.Begin(ctx, sampleIncident)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Callee{Phone: victimPhone, DuressPhrase: samplePhrase}); got != want {
		t.Errorf("unset: Begin = %+v, want %+v", got, want)
	}

	if _, err := pool.Exec(`UPDATE devices SET user_name = $1, safe_phrase = $2, voice_id = $3 WHERE id = 'dev-1'`,
		fullCallee.UserName, fullCallee.SafePhrase, fullCallee.VoiceID); err != nil {
		t.Fatal(err)
	}
	if _, got, err = store.Begin(ctx, sampleIncident); err != nil || got != fullCallee {
		t.Errorf("set: Begin = %+v (err %v), want %+v", got, err, fullCallee)
	}
}
