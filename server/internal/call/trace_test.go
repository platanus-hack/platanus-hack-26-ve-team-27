package call

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// unknownStore knows no call or incident.
type unknownStore struct{ Store }

func (unknownStore) SaveTranscript(context.Context, contract.CallWebhook) error {
	return ErrUnknownIncident
}

// A webhook the server refuses, or one for a call it does not know, is the
// failure of a real test call: the trace says so loudly, with the incident
// it claims.
func TestWebhookTraceLines(t *testing.T) {
	body := sampleWebhook(t)
	wh := &Webhook{Secret: "secret", Now: func() time.Time { return time.Unix(1761297100, 0) }}

	buf := captureLog(t)
	postWebhook(wh, signed("other-secret", time.Unix(1761297100, 0), body), body)
	if line := buf.String(); !strings.Contains(line, "step=webhook_rejected level=ERROR reason=\"signature mismatch\"") ||
		!strings.Contains(line, "trace incident=9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f") || !strings.Contains(line, "ELEVENLABS_WEBHOOK_SECRET") {
		t.Errorf("bad signature: %s", line)
	}

	buf = captureLog(t)
	postWebhook(&Webhook{}, "", body)
	if line := buf.String(); !strings.Contains(line, "step=webhook_rejected level=ERROR reason=no_secret") {
		t.Errorf("no secret: %s", line)
	}

	buf = captureLog(t)
	_, post := signedWebhook((&Engine{Store: unknownStore{}}).Received)
	post(body)
	if line := buf.String(); !strings.Contains(line, "step=webhook_received level=ERROR found=false") || !strings.Contains(line, "DROPPED") {
		t.Errorf("unknown call: %s", line)
	}
	if strings.Contains(buf.String(), "+58") {
		t.Errorf("a phone number in the trace: %s", buf.String())
	}
}
