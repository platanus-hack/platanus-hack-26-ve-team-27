package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

const incidentID = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"

func ptr(f float64) *float64 { return &f }

func sampleMessage() contract.NotifierMessage {
	return contract.NotifierMessage{
		IncidentID:  incidentID,
		To:          []string{"+584241112233"},
		Branch:      contract.BranchCall,
		Location:    contract.Point{Lat: 10.4961, Lng: -66.8531, AccuracyM: ptr(12.5), RecordedAt: time.Date(2026, 10, 24, 13, 10, 0, 0, time.UTC)},
		Verdict:     contract.VerdictDuress,
		Reason:      "the user said the duress phrase",
		TriggeredAt: time.Date(2026, 10, 24, 13, 10, 0, 253e6, time.UTC),
	}
}

// fakeStore claims like the SQL UPDATE: the first Claim wins until Release.
type fakeStore struct {
	mu       sync.Mutex
	msg      contract.NotifierMessage
	claimed  bool
	claims   int
	releases int
}

func (f *fakeStore) Message(context.Context, intake.Incident) (contract.NotifierMessage, error) {
	return f.msg, nil
}

func (f *fakeStore) Claim(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	if f.claimed {
		return false, nil
	}
	f.claimed = true
	return true, nil
}

func (f *fakeStore) Release(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	f.claimed = false
	return nil
}

// fakeSender records every Send and fails for the numbers in fail.
type fakeSender struct {
	mu   sync.Mutex
	sent []string
	fail map[string]bool
}

func (f *fakeSender) Send(_ context.Context, to, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[to] {
		return errors.New("provider down")
	}
	f.sent = append(f.sent, to)
	return nil
}

// kapsoServer stands in for the Kapso API and counts the messages it gets.
type kapsoServer struct {
	*httptest.Server
	mu       sync.Mutex
	messages []kapsoMessage
}

func newKapsoServer(t *testing.T, status int) *kapsoServer {
	t.Helper()
	k := &kapsoServer{}
	k.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m kapsoMessage
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			t.Errorf("decode Kapso body: %v", err)
		}
		k.mu.Lock()
		k.messages = append(k.messages, m)
		k.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.x"}]}`))
	}))
	t.Cleanup(k.Close)
	return k
}

func (k *kapsoServer) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.messages)
}

func (k *kapsoServer) sender() *Kapso {
	return &Kapso{BaseURL: k.URL, APIKey: "test-key", PhoneNumberID: "597907523413541", Client: k.Client()}
}

// Acceptance criterion 1: concurrent and retried Notify calls for one
// incident make exactly one Kapso request.
func TestConcurrentNotifySendsOnce(t *testing.T) {
	k := newKapsoServer(t, http.StatusOK)
	n := New(&fakeStore{msg: sampleMessage()}, k.sender())
	inc := intake.Incident{ID: incidentID, Branch: contract.BranchImmediateAlert}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Notify(context.Background(), inc)
		}()
	}
	wg.Wait()
	n.Notify(context.Background(), inc) // a later retry

	if got := k.count(); got != 1 {
		t.Fatalf("Kapso requests = %d, want 1", got)
	}
}

func TestNotifySendsToEveryFamilyNumberOnce(t *testing.T) {
	msg := sampleMessage()
	msg.To = []string{"+584241112233", "+584125556677"}
	sender := &fakeSender{}
	n := New(&fakeStore{msg: msg}, sender)
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	if strings.Join(sender.sent, ",") != "+584241112233,+584125556677" {
		t.Fatalf("sent = %v, want each family number once", sender.sent)
	}
}

// A send that reached nobody releases the claim, so a retry sends.
func TestFailedSendReleasesClaim(t *testing.T) {
	store := &fakeStore{msg: sampleMessage()}
	sender := &fakeSender{fail: map[string]bool{"+584241112233": true}}
	n := New(store, sender)

	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	if store.releases != 1 || store.claimed {
		t.Fatalf("after a failed send: releases=%d claimed=%v, want 1 and false", store.releases, store.claimed)
	}

	sender.fail = nil
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	if len(sender.sent) != 1 {
		t.Fatalf("sent after the retry = %v, want one message", sender.sent)
	}
}

// Once any family number got the message the claim stays: a retry must not
// message the others twice.
func TestPartialSendKeepsClaim(t *testing.T) {
	msg := sampleMessage()
	msg.To = []string{"+584241112233", "+584125556677"}
	store := &fakeStore{msg: msg}
	sender := &fakeSender{fail: map[string]bool{"+584125556677": true}}
	n := New(store, sender)

	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	if store.releases != 0 || !store.claimed || len(sender.sent) != 1 {
		t.Fatalf("releases=%d claimed=%v sent=%v, want 0, true, one message", store.releases, store.claimed, sender.sent)
	}
}

func TestNoFamilyNumbersSendsNothingAndKeepsIncidentUnclaimed(t *testing.T) {
	msg := sampleMessage()
	msg.To = []string{}
	store := &fakeStore{msg: msg}
	sender := &fakeSender{}
	New(store, sender).Notify(context.Background(), intake.Incident{ID: incidentID})
	if store.claims != 0 || len(sender.sent) != 0 {
		t.Fatalf("claims=%d sent=%v, want none", store.claims, sender.sent)
	}
}

func assertNoLink(t *testing.T, body string) {
	t.Helper()
	for _, bad := range []string{"http", "www", "://", ".com", "maps."} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Errorf("message contains %q, want no link:\n%s", bad, body)
		}
	}
}

// Acceptance criterion 2: location and verdict, and no link.
func TestFormatHasLocationAndVerdictAndNoLink(t *testing.T) {
	got := Format(sampleMessage())
	want := "🚨 Alerta de MAMÁ\n" +
		"Ubicación: 10.4961, -66.8531 (±12 m)\n" +
		"Veredicto: coacción\n" +
		"Hora: 09:10 VET"
	if got != want {
		t.Fatalf("Format =\n%s\nwant\n%s", got, want)
	}
	assertNoLink(t, got)
}

func TestFormatVerdictLabels(t *testing.T) {
	cases := []struct {
		branch  contract.Branch
		verdict contract.Verdict
		want    string
	}{
		{contract.BranchCall, contract.VerdictDuress, "Veredicto: coacción\n"},
		{contract.BranchCall, contract.VerdictNoDuress, "Veredicto: sin coacción\n"},
		{contract.BranchCall, contract.VerdictUnclear, "Veredicto: no está claro\n"},
		{contract.BranchCall, "", "Veredicto: sin veredicto\n"},
		// No call happened: verdict and reason are left out.
		{contract.BranchImmediateAlert, "", "Veredicto: sin llamada, alerta inmediata\n"},
		{contract.BranchSilentTracker, "", "Veredicto: sin llamada, en movimiento rápido (rastreo silencioso)\n"},
	}
	for _, c := range cases {
		msg := sampleMessage()
		msg.Branch, msg.Verdict = c.branch, c.verdict
		if c.verdict == "" {
			msg.Reason = ""
		}
		got := Format(msg)
		if !strings.Contains(got, c.want) || !strings.Contains(got, "Ubicación: 10.4961, -66.8531") {
			t.Errorf("%s/%q: Format =\n%s\nwant it to contain %q and the location", c.branch, c.verdict, got, c.want)
		}
		assertNoLink(t, got)
	}
}

func TestFormatWithoutAccuracy(t *testing.T) {
	msg := sampleMessage()
	msg.Location.AccuracyM = nil
	if got := Format(msg); !strings.Contains(got, "Ubicación: 10.4961, -66.8531\n") {
		t.Fatalf("Format =\n%s\nwant the location line without accuracy", got)
	}
}

// bodySender records every message body.
type bodySender struct {
	mu     sync.Mutex
	bodies []string
}

func (b *bodySender) Send(_ context.Context, _, body string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bodies = append(b.bodies, body)
	return nil
}

type fakeLinks struct {
	url string
	err error
	ids []string
}

func (f *fakeLinks) ReportURL(_ context.Context, id string) (string, error) {
	f.ids = append(f.ids, id)
	return f.url, f.err
}

// #76: with the report on, the message ends with the report link.
func TestFormatWithReportAddsTheLinkLast(t *testing.T) {
	got := FormatWithReport(sampleMessage(), "https://mama.example.com/i/Mq-7vD8oe12cyws_nqXsWg")
	want := Format(sampleMessage()) + "\nFicha: https://mama.example.com/i/Mq-7vD8oe12cyws_nqXsWg"
	if got != want {
		t.Fatalf("FormatWithReport =\n%s\nwant\n%s", got, want)
	}
	if FormatWithReport(sampleMessage(), "") != Format(sampleMessage()) {
		t.Error("a blank report URL changes the message")
	}
}

func TestNotifySendsTheReportLink(t *testing.T) {
	msg := sampleMessage()
	msg.To = []string{"+584241112233", "+584125556677"}
	sender := &bodySender{}
	links := &fakeLinks{url: "https://mama.example.com/i/Mq-7vD8oe12cyws_nqXsWg"}
	New(&fakeStore{msg: msg}, sender).WithReportLinks(links).Notify(context.Background(), intake.Incident{ID: incidentID})
	if len(sender.bodies) != 2 || len(links.ids) != 1 || links.ids[0] != incidentID {
		t.Fatalf("bodies %d, link asked for %v", len(sender.bodies), links.ids)
	}
	for _, b := range sender.bodies {
		if !strings.HasSuffix(b, "\nFicha: https://mama.example.com/i/Mq-7vD8oe12cyws_nqXsWg") {
			t.Errorf("body =\n%s\nwant the report line last", b)
		}
	}
}

// A link that cannot be made never stops the alert.
func TestNotifyWithoutAReportLinkStillSends(t *testing.T) {
	for _, links := range []*fakeLinks{{err: errors.New("db down")}, {url: ""}} {
		sender := &bodySender{}
		New(&fakeStore{msg: sampleMessage()}, sender).WithReportLinks(links).Notify(context.Background(), intake.Incident{ID: incidentID})
		if len(sender.bodies) != 1 {
			t.Fatalf("sent %d messages, want 1", len(sender.bodies))
		}
		assertNoLink(t, sender.bodies[0])
	}
	// Report off: no links at all.
	sender := &bodySender{}
	New(&fakeStore{msg: sampleMessage()}, sender).Notify(context.Background(), intake.Incident{ID: incidentID})
	assertNoLink(t, sender.bodies[0])
}

// The report link goes through Kapso with previews off.
func TestKapsoSendsTheReportLinkWithoutPreview(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	k := &Kapso{BaseURL: srv.URL, APIKey: "k", PhoneNumberID: "1", Client: srv.Client()}
	if err := k.Send(context.Background(), "+584241112233", FormatWithReport(sampleMessage(), "https://mama.example.com/i/Mq-7vD8oe12cyws_nqXsWg")); err != nil {
		t.Fatal(err)
	}
	text, _ := got["text"].(map[string]any)
	if text["preview_url"] != false || !strings.Contains(text["body"].(string), "Ficha: https://") {
		t.Errorf("text = %v, want the link with preview_url false", text)
	}
}
