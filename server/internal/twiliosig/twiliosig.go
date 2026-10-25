// Package twiliosig checks the X-Twilio-Signature of Twilio's webhooks
// (https://www.twilio.com/docs/usage/webhooks/webhooks-security). The
// inbound SMS of the offline channel (#57) and the live transcription of the
// call (#143) share it.
package twiliosig

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"slices"
	"strings"
)

// Header carries the signature of a webhook Twilio posts.
const Header = "X-Twilio-Signature"

// Valid reports whether header, Base64 as Twilio sends it, signs a form
// POST of params to fullURL with authToken. fullURL is the URL Twilio was
// told to post to, never one built from the request's Host header.
func Valid(authToken, fullURL string, params url.Values, header string) bool {
	got, err := base64.StdEncoding.DecodeString(header)
	if err != nil || len(got) == 0 {
		return false
	}
	return hmac.Equal(got, Sign(authToken, fullURL, params))
}

// Sign is HMAC-SHA1 over the full URL followed by every POST param as
// name+value, names sorted; a repeated name contributes its distinct values
// sorted, as Twilio's own validators (twilio-python, twilio-node) compute it.
func Sign(authToken, fullURL string, params url.Values) []byte {
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
