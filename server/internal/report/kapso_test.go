package report

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const kapsoSecret = "kapso-test-secret"

func kapsoSign(body string) string {
	mac := hmac.New(sha256.New, []byte(kapsoSecret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// kapsoBody is a message status event as Kapso sends it (docs.kapso.ai,
// "Message events"): the wamid in message.id, the status in message.kapso.
func kapsoBody(id, status string) string {
	errs := ""
	if status == "failed" {
		errs = `,"errors":[{"code":131047,"title":"Re-engagement message","message":"More than 24 hours have passed"}]`
	}
	return `{"message":{"id":"` + id + `","timestamp":"1730092888","to":"584241112233","kapso":{"status":"` + status + `",` +
		`"statuses":[{"id":"` + id + `","status":"` + status + `","timestamp":"1730092888","recipient_id":"584241112233"` + errs + `}]}},` +
		`"conversation":{"phone_number":"584241112233"},"phone_number_id":"123456789012345"}`
}

func postKapso(t *testing.T, h http.Handler, event, body, sig string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/kapso", strings.NewReader(body))
	req.Header.Set(KapsoEventHeader, event)
	if sig != "" {
		req.Header.Set(KapsoSignatureHeader, sig)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// fakeDeliveryStore records updates.
type fakeDeliveryStore struct {
	updates []string
	err     error
}

func (f *fakeDeliveryStore) UpdateDelivery(_ context.Context, id, status string, _ time.Time, errText string) (bool, error) {
	f.updates = append(f.updates, id+"="+status+"|"+errText)
	return f.err == nil, f.err
}

func TestKapsoWebhookChecksTheSignature(t *testing.T) {
	store := &fakeDeliveryStore{}
	h := &KapsoWebhook{Secret: kapsoSecret, Store: store}
	body := kapsoBody("wamid.1", "delivered")
	if code := postKapso(t, h, "whatsapp.message.delivered", body, kapsoSign(body)); code != http.StatusOK || len(store.updates) != 1 || store.updates[0] != "wamid.1=delivered|" {
		t.Fatalf("signed: %d %v", code, store.updates)
	}
	for name, sig := range map[string]string{"missing": "", "wrong": kapsoSign(body + " "), "not hex": "zz", "prefixed": "sha256=" + kapsoSign(body)} {
		if code := postKapso(t, h, "whatsapp.message.delivered", body, sig); code != http.StatusUnauthorized {
			t.Errorf("%s signature: %d, want 401", name, code)
		}
	}
	if code := postKapso(t, &KapsoWebhook{Store: store}, "whatsapp.message.delivered", body, kapsoSign(body)); code != http.StatusServiceUnavailable {
		t.Errorf("no secret: %d, want 503", code)
	}
	if len(store.updates) != 1 {
		t.Errorf("rejected events changed the store: %v", store.updates)
	}
}

func TestKapsoWebhookEvents(t *testing.T) {
	store := &fakeDeliveryStore{}
	h := &KapsoWebhook{Secret: kapsoSecret, Store: store}
	failed := kapsoBody("wamid.2", "failed")
	if code := postKapso(t, h, "whatsapp.message.failed", failed, kapsoSign(failed)); code != http.StatusOK ||
		store.updates[0] != "wamid.2=failed|Re-engagement message: More than 24 hours have passed" {
		t.Errorf("failed: %d %v", code, store.updates)
	}
	other := `{"message":{"id":"wamid.3","type":"text"}}`
	if code := postKapso(t, h, "whatsapp.message.received", other, kapsoSign(other)); code != http.StatusOK || len(store.updates) != 1 {
		t.Errorf("other events must be ignored: %d %v", code, store.updates)
	}
	noID := `{"message":{}}`
	if code := postKapso(t, h, "whatsapp.message.read", noID, kapsoSign(noID)); code != http.StatusBadRequest {
		t.Errorf("no message id: %d", code)
	}
	store.err = errors.New("db down")
	read := kapsoBody("wamid.2", "read")
	if code := postKapso(t, h, "whatsapp.message.read", read, kapsoSign(read)); code != http.StatusInternalServerError {
		t.Errorf("store failure: %d, want 500 so Kapso retries", code)
	}
}

// On Postgres: the notifier's send is recorded masked; Kapso's statuses only
// move forward, and the page shows them.
func TestDeliveriesThroughKapsoOnPostgres(t *testing.T) {
	pool := newDB(t)
	store := NewDBStore(pool)
	ctx := context.Background()
	log := DeliveryLog{Store: store, Channel: "whatsapp"}
	log.Sent(ctx, testIncidentID, "+584241112233", "whatsapp", "wamid.A", nil)
	log.Sent(ctx, testIncidentID, "+584125556677", "", "", errors.New("kapso: 422 Unprocessable Entity"))

	h := &KapsoWebhook{Secret: kapsoSecret, Store: store, Now: func() time.Time { return at("2026-10-24T13:11:10Z") }}
	for _, st := range []string{"sent", "delivered", "read", "delivered", "sent"} { // late copies must not move it back
		body := kapsoBody("wamid.A", st)
		if code := postKapso(t, h, "whatsapp.message."+st, body, kapsoSign(body)); code != http.StatusOK {
			t.Fatalf("%s: %d", st, code)
		}
	}
	unknown := kapsoBody("wamid.other", "delivered")
	if code := postKapso(t, h, "whatsapp.message.delivered", unknown, kapsoSign(unknown)); code != http.StatusOK {
		t.Errorf("unknown message: %d, want 200", code)
	}

	rec, err := store.Load(ctx, testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Deliveries) != 2 {
		t.Fatalf("deliveries = %+v", rec.Deliveries)
	}
	a, b := rec.Deliveries[0], rec.Deliveries[1]
	if a.Recipient != "+58 424 ···33" || a.Status != "read" || a.DeliveredAt == nil || a.ReadAt == nil || a.Channel != "whatsapp" || a.MessageID != "wamid.A" {
		t.Errorf("first = %+v", a)
	}
	if b.Recipient != "+58 412 ···77" || b.Status != "failed" || !strings.Contains(b.Error, "422") || b.MessageID != "" {
		t.Errorf("second = %+v", b)
	}
	failed := kapsoBody("wamid.A", "failed")
	postKapso(t, h, "whatsapp.message.failed", failed, kapsoSign(failed))
	if rec, _ := store.Load(ctx, testIncidentID); rec.Deliveries[0].Status != "read" {
		t.Error("a failure after read moved the message back")
	}
	p := Build(rec, Options{Channel: "WhatsApp"})
	if len(p.Family) != 2 || p.Family[0].Label != "leído" || p.Family[1].Label != "no se envió" {
		t.Errorf("family chips = %+v", p.Family)
	}
	// Full numbers never reach the table.
	var full int
	if err := pool.QueryRow(`SELECT count(*) FROM notify_deliveries WHERE recipient LIKE '%4241112233%'`).Scan(&full); err != nil || full != 0 {
		t.Errorf("full number stored: %d %v", full, err)
	}
}

func TestMaskRecipient(t *testing.T) {
	for in, want := range map[string]string{"+584241112233": "+58 424 ···33", "+15555550100": "···00", "+58": "···"} {
		if got := MaskRecipient(in); got != want {
			t.Errorf("MaskRecipient(%q) = %q, want %q", in, got, want)
		}
	}
}
