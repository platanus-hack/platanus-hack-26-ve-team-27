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

// kapsoText is a WhatsApp text. PreviewURL is always sent, as false: the
// report link (#76) must not make WhatsApp fetch the page for a preview.
type kapsoText struct {
	Body       string `json:"body"`
	PreviewURL bool   `json:"preview_url"`
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
	_, err := k.SendWithID(ctx, to, body)
	return err
}

// kapsoSent is the part of Kapso's answer (Meta's Cloud API shape) that
// names the message: messages[0].id is the WhatsApp message id (wamid) its
// status webhooks carry.
type kapsoSent struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

// SendWithID is Send that also returns the WhatsApp message id ("" when
// Kapso's answer has none), so delivery and read receipts can be matched.
func (k *Kapso) SendWithID(ctx context.Context, to, body string) (string, error) {
	if k.APIKey == "" || k.PhoneNumberID == "" {
		return "", fmt.Errorf("%w: KAPSO_API_KEY and KAPSO_PHONE_NUMBER_ID are required", errMissingConfig)
	}
	payload, err := json.Marshal(kapsoMessage{
		MessagingProduct: "whatsapp",
		To:               strings.TrimPrefix(to, "+"),
		Type:             "text",
		Text:             kapsoText{Body: body},
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimSuffix(k.BaseURL, "/") + "/" + url.PathEscape(k.PhoneNumberID) + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", k.APIKey)
	raw, err := doRead(k.Client, req, "kapso")
	if err != nil {
		return "", err
	}
	var sent kapsoSent
	if json.Unmarshal(raw, &sent) == nil && len(sent.Messages) > 0 {
		return sent.Messages[0].ID, nil
	}
	return "", nil
}

// do sends req and turns a non-2xx answer into an error carrying the status
// and the start of the body (provider errors never echo our keys).
func do(client *http.Client, req *http.Request, provider string) error {
	_, err := doRead(client, req, provider)
	return err
}

// doRead is do that returns the start of a 2xx body (64 KiB at most).
func doRead(client *http.Client, req *http.Request, provider string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return body, nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := fmt.Sprintf("%s: %s: %s", provider, resp.Status, strings.TrimSpace(string(snippet)))
	if provider == "kapso" && resp.StatusCode == http.StatusUnprocessableEntity {
		msg += " (outside the 24-hour window: the family number must message the WhatsApp number first)"
	}
	return nil, fmt.Errorf("%s", msg)
}
