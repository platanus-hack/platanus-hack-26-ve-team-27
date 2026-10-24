package notify

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// Tests for the demo flag of #102: NOTIFY_SELF (WithSelf).

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

// With NOTIFY_SELF the reporting device gets the same alert as
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

// A reporter who is also a family number gets one message.
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

// Without NOTIFY_SELF, the reporter's number in the message changes nothing:
// the family gets the same message as before.
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
