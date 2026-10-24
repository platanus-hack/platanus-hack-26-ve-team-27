package sms

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Twilio is the InboundSMSProvider for Twilio's Messaging webhook: a form
// POST signed in the X-Twilio-Signature header, answered with TwiML. The reply
// goes back to the sender through Twilio, so no outbound API credentials are
// needed.
type Twilio struct {
	authToken string
	baseURL   string // scheme://host[:port], no trailing slash
}

var _ InboundSMSProvider = (*Twilio)(nil)

// NewTwilio returns the Twilio adapter. authToken is TWILIO_AUTH_TOKEN, the
// HMAC key. baseURL is TWILIO_WEBHOOK_BASE_URL, the public origin Twilio posts
// to (for example https://mama.example.com): the signed URL is built from it,
// never from the request's Host header. Errors never include either value.
func NewTwilio(authToken, baseURL string) (*Twilio, error) {
	if authToken == "" {
		return nil, errors.New("TWILIO_AUTH_TOKEN is not set")
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("TWILIO_WEBHOOK_BASE_URL must be an http(s) origin such as https://mama.example.com, with no path")
	}
	return &Twilio{authToken: authToken, baseURL: baseURL}, nil
}

// Parse verifies X-Twilio-Signature over the configured URL and the POST
// params, then returns the From and Body params.
func (t *Twilio) Parse(r *http.Request) (InboundSMS, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return InboundSMS{}, err
	}
	params, err := url.ParseQuery(string(raw))
	if err != nil {
		// A body that does not parse cannot be verified, so it is unsigned.
		return InboundSMS{}, fmt.Errorf("%w: body is not a form", ErrSignature)
	}
	got, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Twilio-Signature"))
	if err != nil || len(got) == 0 {
		return InboundSMS{}, fmt.Errorf("%w: missing or malformed X-Twilio-Signature", ErrSignature)
	}
	if !hmac.Equal(got, twilioSignature(t.authToken, t.baseURL+r.URL.RequestURI(), params)) {
		return InboundSMS{}, ErrSignature
	}
	in := InboundSMS{From: params.Get("From"), Body: params.Get("Body")}
	if in.From == "" || in.Body == "" {
		return InboundSMS{}, fmt.Errorf("%w: From and Body are required", ErrFormat)
	}
	return in, nil
}

// twilioSignature is HMAC-SHA1 over the full URL followed by every POST param
// as name+value, names sorted; a repeated name contributes its distinct values
// sorted, as Twilio's own validators (twilio-python, twilio-node) compute it.
func twilioSignature(authToken, fullURL string, params url.Values) []byte {
	var b strings.Builder
	b.WriteString(fullURL)
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		values := slices.Clone(params[name])
		slices.Sort(values)
		for _, v := range slices.Compact(values) {
			b.WriteString(name)
			b.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(b.String()))
	return mac.Sum(nil)
}

// Reply answers with TwiML: one <Message> to the sender, or an empty
// <Response> when text is empty.
func (t *Twilio) Reply(w http.ResponseWriter, text string) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response>`)
	if text != "" {
		b.WriteString("<Message>")
		_ = xml.EscapeText(&b, []byte(text))
		b.WriteString("</Message>")
	}
	b.WriteString("</Response>")
	w.Header().Set("Content-Type", "text/xml")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String())
}
