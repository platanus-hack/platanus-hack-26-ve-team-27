package report

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// KapsoSignatureHeader carries Kapso's webhook signature: the hex
// HMAC-SHA256 of the raw body with the webhook's secret, no prefix
// (docs.kapso.ai, "Webhook security").
const KapsoSignatureHeader = "X-Webhook-Signature"

// KapsoEventHeader names the event, e.g. "whatsapp.message.delivered".
const KapsoEventHeader = "X-Webhook-Event"

// DeliveryStore is what KapsoWebhook writes. DBStore is the real one.
type DeliveryStore interface {
	UpdateDelivery(ctx context.Context, messageID, status string, at time.Time, errText string) (bool, error)
}

// KapsoWebhook handles POST /webhooks/kapso: the phone number's message
// status events (whatsapp.message.sent, .delivered, .read, .failed), so the
// report shows each relative's message as sent, delivered or read. Other
// events are answered 200 and ignored. Create it with a struct literal.
type KapsoWebhook struct {
	Secret string // KAPSO_WEBHOOK_SECRET; blank answers 503
	Store  DeliveryStore
	Now    func() time.Time // time.Now when nil
}

// kapsoEvent is the part of a Kapso message event the report reads.
type kapsoEvent struct {
	Message struct {
		ID    string `json:"id"`
		Kapso struct {
			Status   string `json:"status"`
			Statuses []struct {
				Status string `json:"status"`
				Errors []struct {
					Code    int    `json:"code"`
					Title   string `json:"title"`
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"statuses"`
		} `json:"kapso"`
	} `json:"message"`
}

// kapsoStatuses maps the event names the report uses to a delivery status.
var kapsoStatuses = map[string]string{
	"whatsapp.message.sent":      "sent",
	"whatsapp.message.delivered": "delivered",
	"whatsapp.message.read":      "read",
	"whatsapp.message.failed":    "failed",
}

func (k *KapsoWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	if k.Secret == "" {
		http.Error(w, "webhook secret not configured", http.StatusServiceUnavailable)
		return
	}
	if !validKapsoSignature(r.Header.Get(KapsoSignatureHeader), raw, k.Secret) {
		log.Print("kapso webhook: rejected: signature mismatch")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	status, ok := kapsoStatuses[r.Header.Get(KapsoEventHeader)]
	if !ok {
		w.WriteHeader(http.StatusOK) // not a status event: nothing to record
		return
	}
	var ev kapsoEvent
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Message.ID == "" {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	errText := ""
	for _, st := range ev.Message.Kapso.Statuses {
		for _, e := range st.Errors {
			errText = strings.TrimSpace(e.Title + ": " + e.Message)
		}
	}
	now := time.Now
	if k.Now != nil {
		now = k.Now
	}
	changed, err := k.Store.UpdateDelivery(r.Context(), ev.Message.ID, status, now(), errText)
	if err != nil {
		log.Printf("kapso webhook: message %s %s: %v", ev.Message.ID, status, err)
		http.Error(w, "could not store the status", http.StatusInternalServerError)
		return
	}
	if changed {
		log.Printf("kapso webhook: message %s is %s", ev.Message.ID, status)
	}
	w.WriteHeader(http.StatusOK)
}

// validKapsoSignature compares the header with the hex HMAC-SHA256 of body,
// in constant time.
func validKapsoSignature(header string, body []byte, secret string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(header))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
