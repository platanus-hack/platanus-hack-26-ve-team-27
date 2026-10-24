package notify

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/ratelimit"
)

// captureLog sends the standard logger to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// A relative over the per-number cap is skipped, logged masked and not
// recorded as a delivery; once nobody got the message the claim is released.
func TestNotifySkipsANumberOverItsLimit(t *testing.T) {
	out := captureLog(t)
	msg := sampleMessage()
	msg.To = []string{"+584241112233", "+584125556677"}
	sender := &fakeSender{}
	deliveries := &fakeDeliveries{}
	limit := &ratelimit.Limiter{PerNumber: 1, Window: 10 * time.Minute}
	n := New(&fakeStore{msg: msg}, sender).WithDeliveries(deliveries).WithLimit(limit)
	n.Notify(context.Background(), intake.Incident{ID: incidentID})

	// A second incident to the same family: both are over the cap.
	second := &fakeStore{msg: msg}
	New(second, sender).WithDeliveries(deliveries).WithLimit(limit).
		Notify(context.Background(), intake.Incident{ID: "5c0a9d2e-1b3f-4e6a-8c7d-9e0f1a2b3c4d"})

	if strings.Join(sender.sent, ",") != "+584241112233,+584125556677" {
		t.Errorf("sent = %v, want each number once, for the first incident only", sender.sent)
	}
	if len(deliveries.entries) != 2 {
		t.Errorf("deliveries = %+v, want only the 2 real sends", deliveries.entries)
	}
	if second.releases != 1 {
		t.Errorf("releases = %d, want the claim released when nobody got it", second.releases)
	}
	if !strings.Contains(out.String(), "message to ****2233 over the limit of 1 per number per 10m0s; not sent") {
		t.Errorf("log does not report the skipped number:\n%s", out)
	}
	if strings.Contains(out.String(), "+584241112233") {
		t.Errorf("log names a family number in full:\n%s", out)
	}
}

func TestNotifyStopsAtTheTotalLimit(t *testing.T) {
	out := captureLog(t)
	msg := sampleMessage()
	msg.To = []string{"+584241110001", "+584241110002", "+584241110003", "+584241110004", "+584241110005"}
	sender := &fakeSender{}
	New(&fakeStore{msg: msg}, sender).WithLimit(&ratelimit.Limiter{PerNumber: 10, Total: 3, Window: 10 * time.Minute}).
		Notify(context.Background(), intake.Incident{ID: incidentID})

	if len(sender.sent) != 3 {
		t.Errorf("sent = %v, want the first 3 (the total cap)", sender.sent)
	}
	if !strings.Contains(out.String(), "over the limit of 3 in total per 10m0s") {
		t.Errorf("log does not report the total cap:\n%s", out)
	}
}
