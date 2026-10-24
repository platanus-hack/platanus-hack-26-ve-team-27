package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// outbound stands in for every provider the server calls (ElevenLabs,
// Twilio, Kapso) through http.DefaultTransport: it records the calls placed
// and the messages sent and answers like the real APIs, so nothing leaves
// the machine. A placed call reads as completed on its first status poll.
type outbound struct {
	mu       sync.Mutex
	calls    []string // to_number of each outbound-call request
	messages []string // recipient of each WhatsApp message
	other    []string // anything else: a request the test did not expect
}

func (o *outbound) RoundTrip(r *http.Request) (*http.Response, error) {
	var raw []byte
	if r.Body != nil { // a GET has none
		raw, _ = io.ReadAll(r.Body)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case r.URL.Host == "api.elevenlabs.io" && r.URL.Path == "/v1/convai/twilio/outbound-call":
		var in struct {
			To string `json:"to_number"`
		}
		_ = json.Unmarshal(raw, &in)
		o.calls = append(o.calls, in.To)
		n := len(o.calls)
		return reply(r, fmt.Sprintf(`{"success":true,"message":"ok","conversation_id":"conv_%d","callSid":"CA%d"}`, n, n)), nil
	case r.URL.Host == "api.twilio.com" && strings.Contains(r.URL.Path, "/Calls/"):
		return reply(r, `{"status":"completed"}`), nil
	case r.URL.Host == "api.kapso.ai" && strings.HasSuffix(r.URL.Path, "/messages"):
		var in struct {
			To string `json:"to"`
		}
		_ = json.Unmarshal(raw, &in)
		o.messages = append(o.messages, "+"+in.To)
		return reply(r, fmt.Sprintf(`{"messages":[{"id":"wamid.%d"}]}`, len(o.messages))), nil
	}
	o.other = append(o.other, r.Method+" "+r.URL.String())
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
}

func reply(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func (o *outbound) counts() (calls, messages int, perNumber map[string]int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	perNumber = map[string]int{}
	for _, to := range o.calls {
		perNumber[to]++
	}
	for _, to := range o.messages {
		perNumber[to]++
	}
	return len(o.calls), len(o.messages), perNumber
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *lockedBuffer) count(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.b.String(), s)
}

// The abuse the caps are for, against the mux as main builds it: a stranger
// registers devices and loops POST /incidents with fresh incident_ids. With
// the default caps, 200 alerts place 6 calls to one number, 40 over many,
// and the family messages stop at 10 per relative and 60 in total.
func TestIncidentFloodIsCapped(t *testing.T) {
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logs := &lockedBuffer{}
	prevLog := log.Writer()
	log.SetOutput(logs)
	rec := &outbound{}
	prevTransport := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() {
		http.DefaultTransport = prevTransport
		log.SetOutput(prevLog)
	})
	cfg := config.LoadFrom(func(k string) string {
		return map[string]string{
			"ELEVENLABS_API_KEY": "probe-key", "ELEVENLABS_AGENT_ID": "agent_probe", "ELEVENLABS_PHONE_NUMBER_ID": "probe-number",
			"TWILIO_ACCOUNT_SID": "ACprobe", "TWILIO_AUTH_TOKEN": "probe-token",
			"KAPSO_API_KEY": "probe-key", "KAPSO_PHONE_NUMBER_ID": "probe-number",
		}[k]
	})

	incidents := 0
	newIncident := func() string {
		incidents++
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", incidents)
	}
	// flood builds a fresh server (fresh caps) and sends one alert per
	// entry of phones, each from a device registered with that phone and
	// those family numbers. speed picks the branch: walking is a call,
	// still is the immediate alert that messages the family.
	flood := func(t *testing.T, phones []string, family func(i int) []string, speed float64) time.Duration {
		mux := newMux(pool, newNotifier(cfg, pool, nil, nil), newCallRoutes(cfg, pool), nil, nil)
		start := time.Now()
		for i, phone := range phones {
			device := fmt.Sprintf("probe-device-%d", incidents)
			reg, _ := json.Marshal(contract.DeviceRegistration{DeviceID: device, PhoneNumber: phone, FamilyNumbers: family(i), DuressPhrase: "dile a la tía Carmen"})
			if w := serve(mux, "/devices", reg); w.Code != http.StatusNoContent {
				t.Fatalf("POST /devices = %d %s", w.Code, w.Body)
			}
			ev, _ := json.Marshal(contract.AlertEvent{
				IncidentID: newIncident(), DeviceID: device, Trigger: contract.TriggerWidget, TriggeredAt: time.Now(),
				Points: []contract.Point{{Lat: 10.4961, Lng: -66.8531, SpeedMPS: &speed, RecordedAt: time.Now()}},
			})
			if w := serve(mux, "/incidents", ev); w.Code != http.StatusCreated {
				t.Fatalf("POST /incidents = %d %s", w.Code, w.Body)
			}
		}
		return time.Since(start)
	}
	waitFor := func(t *testing.T, done func() bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); !done(); {
			if time.Now().After(deadline) {
				t.Fatalf("handoffs did not finish; log:\n%s", logs)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	const walking, still = 2.0, 0.0
	none := func(int) []string { return nil }

	same := make([]string, 200)
	for i := range same {
		same[i] = "+584141234567"
	}
	took := flood(t, same, none, walking)
	waitFor(t, func() bool { return logs.count("call timing: ") == 200 })
	calls, _, _ := rec.counts()
	t.Logf("200 alerts to one number in %s: %d calls placed, %d rate_limited", took, calls, logs.count("outcome=rate_limited"))
	if calls != config.DefaultCallsPerNumber || logs.count("outcome=rate_limited") != 200-config.DefaultCallsPerNumber {
		t.Errorf("one number: %d calls placed, want the per-number cap %d", calls, config.DefaultCallsPerNumber)
	}

	many := make([]string, 200)
	for i := range many {
		many[i] = fmt.Sprintf("+58414%07d", i)
	}
	took = flood(t, many, none, walking)
	waitFor(t, func() bool { return logs.count("call timing: ") == 400 })
	total, _, perNumber := rec.counts()
	t.Logf("200 alerts to 200 numbers in %s: %d calls placed", took, total-calls)
	if total-calls != config.DefaultCallsTotal {
		t.Errorf("many numbers: %d calls placed, want the total cap %d", total-calls, config.DefaultCallsTotal)
	}
	for number, n := range perNumber {
		if n > config.DefaultCallsPerNumber {
			t.Errorf("%s called %d times", number, n)
		}
	}

	fiveRelatives := func(int) []string {
		return []string{"+584241110001", "+584241110002", "+584241110003", "+584241110004", "+584241110005"}
	}
	took = flood(t, same, fiveRelatives, still)
	notified := func(want int) func() bool {
		return func() bool {
			_, messages, _ := rec.counts()
			return messages+logs.count("; not sent") == want
		}
	}
	waitFor(t, notified(200*5))
	_, messages, _ := rec.counts()
	t.Logf("200 alerts to the same 5 relatives in %s: %d messages sent", took, messages)
	if messages != 5*config.DefaultMessagesPerNumber {
		t.Errorf("same family: %d messages, want %d (the per-number cap for each of 5)", messages, 5*config.DefaultMessagesPerNumber)
	}

	twoEach := func(i int) []string {
		return []string{fmt.Sprintf("+58424%07d", 2*i), fmt.Sprintf("+58424%07d", 2*i+1)}
	}
	took = flood(t, same, twoEach, still)
	waitFor(t, notified(200*5+200*2))
	_, all, perNumber := rec.counts()
	t.Logf("200 alerts to 400 different relatives in %s: %d messages sent", took, all-messages)
	if all-messages != config.DefaultMessagesTotal {
		t.Errorf("many families: %d messages, want the total cap %d", all-messages, config.DefaultMessagesTotal)
	}
	for number, n := range perNumber {
		if strings.HasPrefix(number, "+58424") && n > config.DefaultMessagesPerNumber {
			t.Errorf("%s messaged %d times", number, n)
		}
	}

	if len(rec.other) != 0 {
		t.Errorf("unexpected outbound requests: %v", rec.other)
	}
	for _, line := range []string{
		"call: placing at most 6 per number and 40 in total per 10m0s",
		"notifier: sending at most 10 per number and 60 in total per 10m0s",
	} {
		if logs.count(line) == 0 {
			t.Errorf("no boot log %q", line)
		}
	}
	if logs.count("+584141234567") != 0 || logs.count("+584241110001") != 0 {
		t.Error("a phone number is logged in full")
	}
}

func serve(mux http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	return w
}
