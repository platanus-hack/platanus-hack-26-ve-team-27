package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// Tests for the demo channels of #102: NOTIFY_SELF (WithSelf) and NOTIFY_SMS
// (WithSMS).

const (
	family1  = "+584241112233"
	family2  = "+584125556677"
	reporter = "+584149990000"
)

// channelSender records the number and body of every send and fails for the
// numbers in fail.
type channelSender struct {
	mu     sync.Mutex
	to     []string
	bodies []string
	fail   map[string]bool
}

func (c *channelSender) Send(_ context.Context, to, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail[to] {
		return errors.New("provider down")
	}
	c.to = append(c.to, to)
	c.bodies = append(c.bodies, body)
	return nil
}

func demoMessage() contract.NotifierMessage {
	msg := sampleMessage()
	msg.To = []string{family1, family2}
	msg.Reporter = reporter
	return msg
}

func notifyTwice(n *Notifier) {
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
}

// Criterion 1: with NOTIFY_SELF the reporting device gets the same alert as
// the family, once per incident.
func TestWithSelfAlsoSendsToTheReporter(t *testing.T) {
	sender := &channelSender{}
	notifyTwice(New(&fakeStore{msg: demoMessage()}, sender).WithSelf())
	if want := []string{family1, family2, reporter}; !slices.Equal(sender.to, want) {
		t.Fatalf("sent to %v, want %v", sender.to, want)
	}
	if sender.bodies[2] != sender.bodies[0] {
		t.Errorf("reporter body = %q, want the family's %q", sender.bodies[2], sender.bodies[0])
	}
}

// Criterion 1: a reporter who is also a family number gets one message.
func TestWithSelfDedupesAReporterInTheFamily(t *testing.T) {
	msg := demoMessage()
	msg.Reporter = family2
	sender := &channelSender{}
	New(&fakeStore{msg: msg}, sender).WithSelf().Notify(context.Background(), intake.Incident{ID: incidentID})
	if want := []string{family1, family2}; !slices.Equal(sender.to, want) {
		t.Fatalf("sent to %v, want %v", sender.to, want)
	}
}

// With no family numbers, NOTIFY_SELF still alerts the reporter.
func TestWithSelfAndNoFamilySendsToTheReporter(t *testing.T) {
	msg := demoMessage()
	msg.To = []string{}
	sender := &channelSender{}
	New(&fakeStore{msg: msg}, sender).WithSelf().Notify(context.Background(), intake.Incident{ID: incidentID})
	if want := []string{reporter}; !slices.Equal(sender.to, want) {
		t.Fatalf("sent to %v, want %v", sender.to, want)
	}
}

// Criterion 3: without the flags, the reporter's number in the message
// changes nothing: the family gets the same message, on one channel.
func TestWithoutDemoFlagsNothingChanges(t *testing.T) {
	msg := demoMessage()
	sender := &channelSender{}
	notifyTwice(New(&fakeStore{msg: msg}, sender))
	if want := []string{family1, family2}; !slices.Equal(sender.to, want) {
		t.Fatalf("sent to %v, want %v", sender.to, want)
	}
	for _, b := range sender.bodies {
		if b != Format(msg) {
			t.Errorf("body = %q, want Format's %q", b, Format(msg))
		}
	}
}

// Criterion 2: with NOTIFY_SMS every recipient, the reporter included, gets
// the main channel's message and the short SMS, once per incident.
func TestWithSMSSendsEveryRecipientBothChannels(t *testing.T) {
	msg := demoMessage()
	main, sms := &channelSender{}, &channelSender{}
	notifyTwice(New(&fakeStore{msg: msg}, main).WithSelf().WithSMS(sms))
	want := []string{family1, family2, reporter}
	if !slices.Equal(main.to, want) || !slices.Equal(sms.to, want) {
		t.Fatalf("main sent to %v, SMS to %v; want both %v", main.to, sms.to, want)
	}
	for _, b := range main.bodies {
		if b != Format(msg) {
			t.Errorf("main body = %q, want Format's", b)
		}
	}
	for _, b := range sms.bodies {
		if b != FormatSMS(msg) {
			t.Errorf("SMS body = %q, want FormatSMS's %q", b, FormatSMS(msg))
		}
	}
}

// Criterion 2: a channel that fails never stops the other, and a recipient
// reached on either channel keeps the claim (criterion 5).
func TestWithSMSOneChannelFailingDoesNotStopTheOther(t *testing.T) {
	down := map[string]bool{family1: true, family2: true}
	for name, c := range map[string]struct{ main, sms *channelSender }{
		"WhatsApp down": {&channelSender{fail: down}, &channelSender{}},
		"SMS down":      {&channelSender{}, &channelSender{fail: down}},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{msg: demoMessage()}
			notifyTwice(New(store, c.main).WithSMS(c.sms))
			if got := len(c.main.to) + len(c.sms.to); got != 2 {
				t.Errorf("main sent to %v, SMS to %v; want both numbers on the working channel", c.main.to, c.sms.to)
			}
			if store.releases != 0 || !store.claimed {
				t.Errorf("releases=%d claimed=%v, want the claim kept", store.releases, store.claimed)
			}
		})
	}
}

// Criterion 5: the claim is released only when no recipient got the alert on
// any channel; then a retry sends on both.
func TestWithSMSReleasesOnlyWhenEveryChannelFailed(t *testing.T) {
	down := map[string]bool{family1: true, family2: true, reporter: true}
	store := &fakeStore{msg: demoMessage()}
	main, sms := &channelSender{fail: down}, &channelSender{fail: down}
	n := New(store, main).WithSelf().WithSMS(sms)
	n.Notify(context.Background(), intake.Incident{ID: incidentID})
	if store.releases != 1 || store.claimed {
		t.Fatalf("releases=%d claimed=%v, want 1 and false", store.releases, store.claimed)
	}

	main.fail, sms.fail = nil, nil
	notifyTwice(n)
	if len(main.to) != 3 || len(sms.to) != 3 {
		t.Fatalf("after the retry main sent to %v, SMS to %v; want 3 each", main.to, sms.to)
	}
}

// Each channel's send is recorded with its own channel name.
func TestWithSMSRecordsEachChannel(t *testing.T) {
	k := newKapsoServer(t, 200)
	log := &channelDeliveries{}
	New(&fakeStore{msg: sampleMessage()}, k.sender()).WithSMS(newTwilioServer(t)).WithDeliveries(log).
		Notify(context.Background(), intake.Incident{ID: incidentID})
	if want := []string{"whatsapp", "sms"}; !slices.Equal(log.channels, want) {
		t.Errorf("delivery channels = %v, want %v", log.channels, want)
	}
}

type channelDeliveries struct {
	mu       sync.Mutex
	channels []string
}

func (c *channelDeliveries) Sent(_ context.Context, _, _, channel, _ string, _ error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.channels = append(c.channels, channel)
}

// newTwilioServer stands in for the Twilio Messages API and returns an SMS
// sender pointed at it.
func newTwilioServer(t *testing.T) *SMS {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	return &SMS{BaseURL: srv.URL, AccountSID: "AC1", AuthToken: "t", From: "+15550001111", Client: srv.Client()}
}

// The SMS fits one GSM-7 segment for every verdict, branch and coordinate,
// and points at the location on Google Maps.
func TestFormatSMSFitsOneSegment(t *testing.T) {
	msg := sampleMessage()
	if got, want := FormatSMS(msg), "Alerta de MAMA: coaccion. 09:10 VET. https://maps.google.com/?q=10.4961,-66.8531"; got != want {
		t.Errorf("FormatSMS = %q, want %q", got, want)
	}
	for _, v := range []contract.Verdict{contract.VerdictDuress, contract.VerdictNoDuress, contract.VerdictUnclear, ""} {
		for _, b := range []contract.Branch{contract.BranchCall, contract.BranchImmediateAlert, contract.BranchSilentTracker} {
			msg.Verdict, msg.Branch = v, b
			msg.Location.Lat, msg.Location.Lng = -89.9999, -179.9999
			s := FormatSMS(msg)
			if len(s) > smsLimit {
				t.Errorf("%q is %d characters, over one segment (%d)", s, len(s), smsLimit)
			}
			// Printable ASCII without GSM-7's two-character escapes.
			for _, r := range s {
				if r < ' ' || r > '~' || strings.ContainsRune("[]{}^~\\|", r) {
					t.Errorf("%q has %q, outside GSM-7's basic set", s, r)
					break
				}
			}
		}
	}
}

// Criterion 3: NOTIFY_SMS adds an SMS sender only next to WhatsApp; with
// NOTIFIER=sms the family already gets an SMS, so it adds none.
func TestNewExtraSMS(t *testing.T) {
	env := map[string]string{"TWILIO_ACCOUNT_SID": "AC1", "TWILIO_AUTH_TOKEN": "t", "TWILIO_PHONE_NUMBER": "+15550001111"}
	load := func(notifier, notifySMS string) config.Config {
		env["NOTIFIER"], env["NOTIFY_SMS"] = notifier, notifySMS
		return config.LoadFrom(func(k string) string { return env[k] })
	}
	if s := NewExtraSMS(load("kapso", "")); s != nil {
		t.Errorf("NOTIFY_SMS unset: got %+v, want nil", s)
	}
	if s := NewExtraSMS(load("sms", "true")); s != nil {
		t.Errorf("NOTIFIER=sms NOTIFY_SMS=true: got %+v, want nil (no SMS twice)", s)
	}
	s := NewExtraSMS(load("", "true"))
	if s == nil || s.BaseURL != TwilioBaseURL || s.AccountSID != "AC1" || s.AuthToken != "t" || s.From != "+15550001111" {
		t.Errorf("NOTIFIER=kapso NOTIFY_SMS=true: got %+v, want the Twilio sender", s)
	}
}
