package sms

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/twiliosig"
)

// Test-only credentials and numbers; +1500555xxxx are Twilio's magic test
// numbers, never real phones.
const (
	testAuthToken = "test-auth-token" // secret-scan:allow (test fixture)
	testBaseURL   = "https://mama.example.com"
	testPath      = "/sms/inbound/twilio"
	testFrom      = "+15005550001"
	// testBody is the alert_caracas SMS vector of payload/testdata.
	testBody = "MAMA1 AQFuZkwnxDVtcwEjRWeJq83vAAAAEADh_5nzUQAqatyBoBuDbuWUTkz8G59I2UZro-vI0F9Tp84oMamtaOI9iyVnc0GXy7Sml4xgsTUa2cZYFkL_av9w-7CpAdjNcBQGLwY"
	// testSignature is X-Twilio-Signature of sampleWebhook() posted to
	// testBaseURL+testPath with testAuthToken, computed independently with
	// Python's hmac module: Base64(HMAC-SHA1(token, URL + name+value for each
	// param sorted by name)).
	testSignature = "3zJOjO8i3lhQICOoa9exkn/5V74="
)

// sampleWebhook is the form Twilio posts for an inbound SMS.
func sampleWebhook() url.Values {
	return url.Values{
		"ToCountry":     {"US"},
		"ToState":       {"CA"},
		"SmsMessageSid": {"SMtestmessage0001"},
		"NumMedia":      {"0"},
		"ToCity":        {""},
		"FromZip":       {""},
		"SmsSid":        {"SMtestmessage0001"},
		"FromState":     {""},
		"SmsStatus":     {"received"},
		"FromCity":      {""},
		"Body":          {testBody},
		"FromCountry":   {"US"},
		"To":            {"+15005550006"},
		"ToZip":         {""},
		"NumSegments":   {"1"},
		"MessageSid":    {"SMtestmessage0001"},
		"AccountSid":    {"ACtestaccount"},
		"From":          {testFrom},
		"ApiVersion":    {"2010-04-01"},
	}
}

func mustTwilio(t *testing.T) *Twilio {
	t.Helper()
	tw, err := NewTwilio(testAuthToken, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	return tw
}

func twilioRequest(path string, form url.Values, sig string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sig != "" {
		r.Header.Set("X-Twilio-Signature", sig)
	}
	return r
}

func sign(token, fullURL string, form url.Values) string {
	return base64.StdEncoding.EncodeToString(twiliosig.Sign(token, fullURL, form))
}

func TestTwilioParsesSignedSampleWebhook(t *testing.T) {
	in, err := mustTwilio(t).Parse(twilioRequest(testPath, sampleWebhook(), testSignature))
	if err != nil {
		t.Fatal(err)
	}
	if in != (InboundSMS{From: testFrom, Body: testBody}) {
		t.Fatalf("Parse = %+v", in)
	}
}

func TestTwilioRejects(t *testing.T) {
	tampered := sampleWebhook()
	tampered.Set("Body", "MAMA1 AAAA")
	extra := sampleWebhook()
	extra.Set("Injected", "1")
	noBody := sampleWebhook()
	noBody.Del("Body")
	noFrom := sampleWebhook()
	noFrom.Del("From")

	cases := []struct {
		name string
		path string
		form url.Values
		sig  string
		want error
	}{
		{"missing signature", testPath, sampleWebhook(), "", ErrSignature},
		{"signature not Base64", testPath, sampleWebhook(), "not base64!", ErrSignature},
		{"other token", testPath, sampleWebhook(), sign("other-token", testBaseURL+testPath, sampleWebhook()), ErrSignature},
		{"tampered param", testPath, tampered, testSignature, ErrSignature},
		{"added param", testPath, extra, testSignature, ErrSignature},
		{"wrong path", "/sms/inbound/other", sampleWebhook(), testSignature, ErrSignature},
		{"added query", testPath + "?x=1", sampleWebhook(), testSignature, ErrSignature},
		{"signed for another host", testPath, sampleWebhook(), sign(testAuthToken, "https://evil.example.com"+testPath, sampleWebhook()), ErrSignature},
		{"signed without Body", testPath, noBody, sign(testAuthToken, testBaseURL+testPath, noBody), ErrFormat},
		{"signed without From", testPath, noFrom, sign(testAuthToken, testBaseURL+testPath, noFrom), ErrFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := mustTwilio(t).Parse(twilioRequest(c.path, c.form, c.sig))
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// The Host header is attacker-controlled; the signed URL comes from config.
func TestTwilioIgnoresHostHeader(t *testing.T) {
	r := twilioRequest(testPath, sampleWebhook(), sign(testAuthToken, "https://evil.example.com"+testPath, sampleWebhook()))
	r.Host = "evil.example.com"
	if _, err := mustTwilio(t).Parse(r); !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
	r = twilioRequest(testPath, sampleWebhook(), testSignature)
	r.Host = "internal:8080"
	if _, err := mustTwilio(t).Parse(r); err != nil {
		t.Fatalf("err = %v, want nil whatever the Host", err)
	}
}

func TestTwilioRepeatedParamsSortValues(t *testing.T) {
	a := url.Values{"X": {"b", "a"}}
	b := url.Values{"X": {"a", "b"}}
	if sign(testAuthToken, testBaseURL, a) != sign(testAuthToken, testBaseURL, b) {
		t.Fatal("value order changed the signature")
	}
	if sign(testAuthToken, testBaseURL, a) == sign(testAuthToken, testBaseURL, url.Values{"X": {"a"}}) {
		t.Fatal("a dropped value did not change the signature")
	}
}

func TestNewTwilioValidatesConfig(t *testing.T) {
	cases := []struct {
		name, token, base string
		ok                bool
	}{
		{"https origin", testAuthToken, "https://mama.example.com", true},
		{"trailing slash", testAuthToken, "https://mama.example.com/", true},
		{"http with port", testAuthToken, "http://localhost:8080", true},
		{"no token", "", "https://mama.example.com", false},
		{"no base", testAuthToken, "", false},
		{"no scheme", testAuthToken, "mama.example.com", false},
		{"ftp", testAuthToken, "ftp://mama.example.com", false},
		{"with path", testAuthToken, "https://mama.example.com/sms", false},
		{"with query", testAuthToken, "https://mama.example.com?x=1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewTwilio(c.token, c.base)
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok %v", err, c.ok)
			}
			if err != nil && c.token != "" && strings.Contains(err.Error(), c.token) {
				t.Fatalf("error %q leaks the token", err)
			}
		})
	}
}

func TestTwilioReplyIsTwiML(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"message", "MAMA1 abc", `<?xml version="1.0" encoding="UTF-8"?><Response><Message>MAMA1 abc</Message></Response>`},
		{"escaped", `a<b>&"c"`, `<?xml version="1.0" encoding="UTF-8"?><Response><Message>a&lt;b&gt;&amp;&#34;c&#34;</Message></Response>`},
		{"no reply", "", `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mustTwilio(t).Reply(rec, c.text)
			if rec.Code != 200 || rec.Body.String() != c.want {
				t.Fatalf("Reply = %d %q, want 200 %q", rec.Code, rec.Body.String(), c.want)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/xml" {
				t.Fatalf("Content-Type = %q", ct)
			}
		})
	}
}
