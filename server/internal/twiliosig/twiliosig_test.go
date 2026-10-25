package twiliosig

import (
	"encoding/base64"
	"net/url"
	"testing"
)

// The example from Twilio's "Webhooks security" docs.
func docsExample() (token, fullURL string, params url.Values) {
	return "12345", "https://mycompany.com/myapp.php?foo=1&bar=2", url.Values{
		"CallSid": {"CA1234567890ABCDE"},
		"Caller":  {"+12349013030"},
		"Digits":  {"1234"},
		"From":    {"+12349013030"},
		"To":      {"+18005551212"},
	}
}

func TestSignMatchesDocsExample(t *testing.T) {
	token, fullURL, params := docsExample()
	if got := base64.StdEncoding.EncodeToString(Sign(token, fullURL, params)); got != "0/KCTR6DLpKmkAf8muzZqo1nDgQ=" {
		t.Fatalf("signature = %q, want Twilio's 0/KCTR6DLpKmkAf8muzZqo1nDgQ=", got)
	}
}

func TestValid(t *testing.T) {
	token, fullURL, params := docsExample()
	tampered := url.Values{}
	for k, v := range params {
		tampered[k] = v
	}
	tampered.Set("Digits", "9999")
	for _, tc := range []struct {
		name, token, url, header string
		params                   url.Values
		want                     bool
	}{
		{"docs example", token, fullURL, "0/KCTR6DLpKmkAf8muzZqo1nDgQ=", params, true},
		{"other token", "54321", fullURL, "0/KCTR6DLpKmkAf8muzZqo1nDgQ=", params, false},
		{"other URL", token, "https://mycompany.com/myapp.php", "0/KCTR6DLpKmkAf8muzZqo1nDgQ=", params, false},
		{"tampered param", token, fullURL, "0/KCTR6DLpKmkAf8muzZqo1nDgQ=", tampered, false},
		{"missing", token, fullURL, "", params, false},
		{"not Base64", token, fullURL, "not base64!", params, false},
	} {
		if got := Valid(tc.token, tc.url, tc.params, tc.header); got != tc.want {
			t.Errorf("%s: Valid = %v, want %v", tc.name, got, tc.want)
		}
	}
}
