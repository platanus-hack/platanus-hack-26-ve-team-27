package inboundsms

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A sample Twilio SMS webhook and its signature. The signature was produced
// outside this code, from the algorithm in Twilio's webhook security docs:
//
//	printf '%s' "<url><Body..><From..><MessageSid..><To..>" |
//	  openssl dgst -sha1 -hmac test-auth-token -binary | base64
//
// with the parameters concatenated as name+value in alphabetical order. It
// is not a vector published by Twilio.
const (
	sampleToken     = "test-auth-token"
	sampleBase      = "https://mama-test.example.com"
	samplePath      = "/sms/inbound/twilio"
	sampleSignature = "USaagTmU/ymTGDEdknEL9DyeO1Y="
	// Parameters deliberately out of alphabetical order.
	sampleForm = "To=Whatever%2B15557654321&Body=MAMA1+AAAA_test&MessageSid=SM0123456789abcdef0123456789abcdef&From=Roundtrip%2B584141234567"
)

func sampleRequest(form, signature string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, samplePath, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if signature != "" {
		req.Header.Set("X-Twilio-Signature", signature)
	}
	return req
}

func TestTwilioParseAcceptsTheSampleWebhook(t *testing.T) {
	msg, err := NewTwilio(sampleToken, sampleBase+"/").Parse(sampleRequest(sampleForm, sampleSignature))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Message{From: "Roundtrip+584141234567", Body: "MAMA1 AAAA_test"}); msg != want {
		t.Errorf("Message = %+v, want %+v", msg, want)
	}
}

func TestTwilioParseRejectsUnauthenticatedRequests(t *testing.T) {
	tw := NewTwilio(sampleToken, sampleBase)
	for name, c := range map[string]struct {
		tw  Twilio
		req *http.Request
	}{
		"wrong signature":      {tw, sampleRequest(sampleForm, "AAAAAAAAAAAAAAAAAAAAAAAAAAA=")},
		"missing header":       {tw, sampleRequest(sampleForm, "")},
		"not base64":           {tw, sampleRequest(sampleForm, "%%%")},
		"tampered body":        {tw, sampleRequest(strings.Replace(sampleForm, "AAAA_test", "BBBB_test", 1), sampleSignature)},
		"extra parameter":      {tw, sampleRequest(sampleForm+"&X=1", sampleSignature)},
		"other auth token":     {NewTwilio("other", sampleBase), sampleRequest(sampleForm, sampleSignature)},
		"other public URL":     {NewTwilio(sampleToken, "https://evil.example.com"), sampleRequest(sampleForm, sampleSignature)},
		"query not in the URL": {tw, httptest.NewRequest(http.MethodPost, samplePath+"?a=1", strings.NewReader(sampleForm))},
	} {
		c.req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, err := c.tw.Parse(c.req); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: err = %v, want ErrUnauthenticated", name, err)
		}
	}
}

func TestTwilioSignatureCoversTheQueryAndRepeatedParameters(t *testing.T) {
	tw := NewTwilio(sampleToken, sampleBase)
	form := "k=b&k=a&Body=x"
	req := httptest.NewRequest(http.MethodPost, samplePath+"?q=1", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Values of a repeated name are signed in sorted order: Body, k=a, k=b.
	want := tw.sign(sampleBase+samplePath+"?q=1", map[string][]string{"Body": {"x"}, "k": {"a", "b"}})
	req.Header.Set("X-Twilio-Signature", base64.StdEncoding.EncodeToString(want))
	if _, err := tw.Parse(req); err != nil {
		t.Errorf("Parse: %v", err)
	}
}

func TestTwilioConfigured(t *testing.T) {
	for _, c := range []struct {
		tw   Twilio
		want bool
	}{
		{NewTwilio("t", "https://x"), true},
		{NewTwilio("", "https://x"), false},
		{NewTwilio("t", ""), false},
		{NewTwilio("t", "/"), false},
	} {
		if got := c.tw.Configured(); got != c.want {
			t.Errorf("%+v Configured() = %v, want %v", c.tw, got, c.want)
		}
	}
}

func TestTwilioReplyIsEscapedTwiML(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"", `<?xml version="1.0" encoding="UTF-8"?><Response/>`},
		{"MAMA1:ACK:ab+/=", `<?xml version="1.0" encoding="UTF-8"?><Response><Message>MAMA1:ACK:ab+/=</Message></Response>`},
		{`a<b>&"c'`, `<?xml version="1.0" encoding="UTF-8"?><Response><Message>a&lt;b&gt;&amp;&#34;c&#39;</Message></Response>`},
	} {
		rec := httptest.NewRecorder()
		Twilio{}.Reply(rec, c.text)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/xml") {
			t.Errorf("Reply(%q) = %d %q", c.text, rec.Code, rec.Header().Get("Content-Type"))
		}
		if got := rec.Body.String(); got != c.want {
			t.Errorf("Reply(%q) body = %s, want %s", c.text, got, c.want)
		}
	}
}
