package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// KapsoBaseURL is Kapso's Meta WhatsApp Cloud API proxy.
const KapsoBaseURL = "https://api.kapso.ai/meta/whatsapp/v24.0"

// Kapso sends a WhatsApp text message through Kapso (#14 spike).
type Kapso struct {
	BaseURL       string // KapsoBaseURL; tests point it at an httptest.Server
	APIKey        string // sent as X-API-Key
	PhoneNumberID string // the sending WhatsApp phone_number_id
	Client        *http.Client
}

type kapsoText struct {
	Body string `json:"body"`
}

type kapsoMessage struct {
	MessagingProduct string    `json:"messaging_product"`
	To               string    `json:"to"`
	Type             string    `json:"type"`
	Text             kapsoText `json:"text"`
}

// Send posts one text message. Kapso wants to as digits only, so the E.164
// "+" is stripped. Outside the 24-hour window Kapso answers 422; the error
// says so.
func (k *Kapso) Send(ctx context.Context, to, body string) error {
	if k.APIKey == "" || k.PhoneNumberID == "" {
		return fmt.Errorf("%w: KAPSO_API_KEY and KAPSO_PHONE_NUMBER_ID are required", errMissingConfig)
	}
	payload, err := json.Marshal(kapsoMessage{
		MessagingProduct: "whatsapp",
		To:               strings.TrimPrefix(to, "+"),
		Type:             "text",
		Text:             kapsoText{Body: body},
	})
	if err != nil {
		return err
	}
	endpoint := strings.TrimSuffix(k.BaseURL, "/") + "/" + url.PathEscape(k.PhoneNumberID) + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", k.APIKey)
	return do(k.Client, req, "kapso")
}

// do sends req and turns a non-2xx answer into an error carrying the status
// and the start of the body (provider errors never echo our keys).
func do(client *http.Client, req *http.Request, provider string) error {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", provider, err)
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	msg := fmt.Sprintf("%s: %s: %s", provider, resp.Status, strings.TrimSpace(string(snippet)))
	if provider == "kapso" && resp.StatusCode == http.StatusUnprocessableEntity {
		msg += " (outside the 24-hour window: the family number must message the WhatsApp number first)"
	}
	return fmt.Errorf("%s", msg)
}
