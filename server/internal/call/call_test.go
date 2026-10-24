package call

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// fakeElevenLabs records the outbound-call request and answers with status
// and body.
func fakeElevenLabs(t *testing.T, status int, body string) (*httptest.Server, *http.Request, *[]byte) {
	t.Helper()
	var got http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &gotBody
}

func TestPlaceSendsTheOutboundCallRequest(t *testing.T) {
	srv, got, gotBody := fakeElevenLabs(t, http.StatusOK,
		`{"success":true,"message":"ok","conversation_id":"conv_1","callSid":"CA1"}`)
	c := &Client{BaseURL: srv.URL, APIKey: "key", AgentID: "agent_1", PhoneNumberID: "phnum_1", VoiceID: "voice_1"}

	placed, err := c.Place(context.Background(), "+584141234567", "inc-1")
	if err != nil {
		t.Fatal(err)
	}
	if placed != (Placed{ConversationID: "conv_1", CallSID: "CA1"}) {
		t.Errorf("Placed = %+v", placed)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/v1/convai/twilio/outbound-call" {
		t.Errorf("request = %s %s", got.Method, got.URL.Path)
	}
	if got.Header.Get("xi-api-key") != "key" || got.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", got.Header)
	}
	want := `{"agent_id":"agent_1","agent_phone_number_id":"phnum_1","to_number":"+584141234567",` +
		`"conversation_initiation_client_data":{"dynamic_variables":{"incident_id":"inc-1"},` +
		`"conversation_config_override":{"tts":{"voice_id":"voice_1"}}}}`
	if string(*gotBody) != want {
		t.Errorf("body =\n%s\nwant\n%s", *gotBody, want)
	}
}

func TestPlaceWithoutVoiceOrIncidentSendsNoClientData(t *testing.T) {
	srv, _, gotBody := fakeElevenLabs(t, http.StatusOK, `{"success":true,"message":"ok","conversation_id":"c","callSid":"s"}`)
	c := &Client{BaseURL: srv.URL, APIKey: "key", AgentID: "a", PhoneNumberID: "p"}

	if _, err := c.Place(context.Background(), "+584141234567", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(*gotBody), "conversation_initiation_client_data") {
		t.Errorf("body has client data: %s", *gotBody)
	}
}

func TestPlaceErrors(t *testing.T) {
	cases := []struct {
		name, body, wantErr string
		status              int
	}{
		{"http error", `{"detail":"bad agent"}`, "422", http.StatusUnprocessableEntity},
		{"success false", `{"success":false,"message":"Twilio refused the number"}`, "Twilio refused the number", http.StatusOK},
		{"bad json", `not json`, "decode", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := fakeElevenLabs(t, tc.status, tc.body)
			c := &Client{BaseURL: srv.URL, APIKey: "key", AgentID: "a", PhoneNumberID: "p"}
			_, err := c.Place(context.Background(), "+584141234567", "")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestPlaceWithoutConfigFailsBeforeAnyRequest(t *testing.T) {
	_, err := (&Client{BaseURL: "http://127.0.0.1:0"}).Place(context.Background(), "+584141234567", "")
	if !errors.Is(err, ErrMissingConfig) {
		t.Errorf("err = %v, want ErrMissingConfig", err)
	}
}

// sampleWebhook is the contract's canonical CallWebhook sample.
func sampleWebhook(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "contract", "testdata", "call_webhook.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func signed(secret string, at time.Time, body []byte) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	return "t=" + ts + ",v0=" + hex.EncodeToString(Sign(secret, ts, body))
}

func postWebhook(wh *Webhook, header string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", strings.NewReader(string(body)))
	if header != "" {
		req.Header.Set(SignatureHeader, header)
	}
	rec := httptest.NewRecorder()
	wh.ServeHTTP(rec, req)
	return rec
}

func TestWebhookSignature(t *testing.T) {
	now := time.Unix(1761297100, 0)
	body := sampleWebhook(t)
	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"valid", signed("secret", now, body), http.StatusOK},
		{"valid, near the edge", signed("secret", now.Add(-29*time.Minute), body), http.StatusOK},
		{"missing", "", http.StatusUnauthorized},
		{"no v0", "t=" + strconv.FormatInt(now.Unix(), 10), http.StatusUnauthorized},
		{"wrong secret", signed("other", now, body), http.StatusUnauthorized},
		{"stale", signed("secret", now.Add(-31*time.Minute), body), http.StatusUnauthorized},
		{"future", signed("secret", now.Add(31*time.Minute), body), http.StatusUnauthorized},
		{"not hex", "t=" + strconv.FormatInt(now.Unix(), 10) + ",v0=zz", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			wh := &Webhook{Secret: "secret", Now: func() time.Time { return now },
				Received: func(context.Context, contract.CallWebhook) { calls++ }}
			rec := postWebhook(wh, tc.header, body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if wantCalls := map[bool]int{true: 1, false: 0}[tc.want == http.StatusOK]; calls != wantCalls {
				t.Errorf("Received called %d times, want %d", calls, wantCalls)
			}
		})
	}
}

func TestWebhookRejectsATamperedBody(t *testing.T) {
	now := time.Unix(1761297100, 0)
	body := sampleWebhook(t)
	header := signed("secret", now, body)
	tampered := []byte(strings.Replace(string(body), "42", "43", 1))
	wh := &Webhook{Secret: "secret", Now: func() time.Time { return now }}
	if rec := postWebhook(wh, header, tampered); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWebhookParsesTheTranscript(t *testing.T) {
	var got contract.CallWebhook
	wh := &Webhook{Received: func(_ context.Context, h contract.CallWebhook) { got = h }}

	if rec := postWebhook(wh, "", sampleWebhook(t)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.IncidentID() != "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f" {
		t.Errorf("incident = %q", got.IncidentID())
	}
	if len(got.Data.Transcript) != 2 || got.Data.Transcript[0].Role != "agent" || got.Data.Metadata.CallDurationSecs != 42 {
		t.Errorf("parsed = %+v", got.Data)
	}
}

func TestWebhookOtherTypesAnswer200WithoutReceived(t *testing.T) {
	for _, body := range []string{
		`{"type":"call_initiation_failure","data":{"conversation_id":"c","failure_reason":"busy"}}`,
		`{"type":"post_call_audio","data":{}}`,
	} {
		called := false
		wh := &Webhook{Received: func(context.Context, contract.CallWebhook) { called = true }}
		if rec := postWebhook(wh, "", []byte(body)); rec.Code != http.StatusOK || called {
			t.Errorf("%s: status %d, Received called %v", body, rec.Code, called)
		}
	}
}

func TestWebhookBadJSONIs400(t *testing.T) {
	if rec := postWebhook(&Webhook{}, "", []byte("{")); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func spikeMux(t *testing.T, elevenLabsURL, token string) *http.ServeMux {
	t.Helper()
	rt := NewRoutes(config.ElevenLabs{APIKey: "key", AgentID: "a", PhoneNumberID: "p", CallSpikeToken: token})
	rt.client.BaseURL = elevenLabsURL
	mux := http.NewServeMux()
	rt.Register(mux)
	return mux
}

func postSpike(mux *http.ServeMux, auth, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/spike/call", strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSpikeRouteIsOffWithoutAToken(t *testing.T) {
	if rec := postSpike(spikeMux(t, "http://127.0.0.1:0", ""), "Bearer ", `{"to":"+584141234567"}`); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestSpikeRoute(t *testing.T) {
	srv, _, gotBody := fakeElevenLabs(t, http.StatusOK, `{"success":true,"message":"ok","conversation_id":"conv_1","callSid":"CA1"}`)
	mux := spikeMux(t, srv.URL, "tok")

	cases := []struct {
		name, auth, body string
		want             int
	}{
		{"no auth", "", `{"to":"+584141234567"}`, http.StatusUnauthorized},
		{"wrong token", "Bearer nope", `{"to":"+584141234567"}`, http.StatusUnauthorized},
		{"not e164", "Bearer tok", `{"to":"04141234567"}`, http.StatusBadRequest},
		{"bad json", "Bearer tok", `{`, http.StatusBadRequest},
		{"ok", "Bearer tok", `{"to":"+584141234567","incident_id":"inc-1"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*gotBody = nil
			rec := postSpike(mux, tc.auth, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want != http.StatusOK {
				if *gotBody != nil {
					t.Error("ElevenLabs was called for a rejected request")
				}
				return
			}
			var out spikeCallResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.ConversationID != "conv_1" || out.CallSID != "CA1" || out.RequestMS < 0 {
				t.Errorf("response = %+v", out)
			}
		})
	}
}

func TestSpikeRouteReportsAProviderErrorAs502(t *testing.T) {
	srv, _, _ := fakeElevenLabs(t, http.StatusOK, `{"success":false,"message":"Twilio error 13227"}`)
	rec := postSpike(spikeMux(t, srv.URL, "tok"), "Bearer tok", `{"to":"+584141234567"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "13227") {
		t.Errorf("status = %d body %q, want 502 with the Twilio error", rec.Code, rec.Body)
	}
}
