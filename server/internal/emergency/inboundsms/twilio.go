package inboundsms

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1" // Twilio's webhook signature is HMAC-SHA1; not our choice.
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"sort"
	"strings"
)

// Twilio is the Provider for Twilio programmable SMS. In the Twilio console
// the number's "A message comes in" webhook is
// POST <PUBLIC_BASE_URL>/sms/inbound/twilio.
type Twilio struct {
	// AuthToken is TWILIO_AUTH_TOKEN, the HMAC key of X-Twilio-Signature.
	AuthToken string
	// BaseURL is the public origin Twilio calls, PUBLIC_BASE_URL, without a
	// trailing slash. Twilio signs the full URL it requested, which behind a
	// proxy the server cannot rebuild from the request, so it is configured.
	BaseURL string
}

var _ Provider = Twilio{}

// NewTwilio returns the Twilio provider; trailing slashes on baseURL are dropped.
func NewTwilio(authToken, baseURL string) Twilio {
	return Twilio{AuthToken: authToken, BaseURL: strings.TrimRight(baseURL, "/")}
}

// Name returns "twilio".
func (Twilio) Name() string { return "twilio" }

// Configured reports whether both the auth token and the public URL are set.
func (t Twilio) Configured() bool { return t.AuthToken != "" && t.BaseURL != "" }

// Parse verifies X-Twilio-Signature and returns the From and Body parameters.
func (t Twilio) Parse(r *http.Request) (Message, error) {
	if err := r.ParseForm(); err != nil {
		return Message{}, err
	}
	// PostForm, not Form: the query string is already part of the signed URL.
	got, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Twilio-Signature"))
	if err != nil || !hmac.Equal(got, t.sign(t.BaseURL+r.URL.RequestURI(), r.PostForm)) {
		return Message{}, ErrUnauthenticated
	}
	return Message{From: r.PostForm.Get("From"), Body: r.PostForm.Get("Body")}, nil
}

// sign is HMAC-SHA1(AuthToken, url + each POST parameter name and value,
// sorted by name; the values of a repeated name sorted too).
func (t Twilio) sign(url string, params map[string][]string) []byte {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	mac := hmac.New(sha1.New, []byte(t.AuthToken))
	mac.Write([]byte(url))
	for _, name := range names {
		values := append([]string(nil), params[name]...)
		sort.Strings(values)
		for _, v := range values {
			mac.Write([]byte(name))
			mac.Write([]byte(v))
		}
	}
	return mac.Sum(nil)
}

// Reply answers with TwiML: a <Message> Twilio texts back to the sender, or an
// empty <Response/> when text is empty.
func (Twilio) Reply(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(twiml(text))
}

func twiml(text string) []byte {
	const head = `<?xml version="1.0" encoding="UTF-8"?>`
	if text == "" {
		return []byte(head + "<Response/>")
	}
	var b bytes.Buffer
	b.WriteString(head + "<Response><Message>")
	_ = xml.EscapeText(&b, []byte(text)) // a bytes.Buffer never fails
	b.WriteString("</Message></Response>")
	return b.Bytes()
}
