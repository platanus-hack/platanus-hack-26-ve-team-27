package notify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

func TestKapsoSendRequest(t *testing.T) {
	var gotPath, gotKey, gotType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotType = r.URL.Path, r.Header.Get("X-API-Key"), r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	k := &Kapso{BaseURL: srv.URL, APIKey: "test-key", PhoneNumberID: "597907523413541", Client: srv.Client()}
	if err := k.Send(context.Background(), "+584226340289", "hola"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/597907523413541/messages" || gotKey != "test-key" || gotType != "application/json" {
		t.Errorf("path=%q key=%q content-type=%q", gotPath, gotKey, gotType)
	}
	// to is digits only: Kapso rejects the E.164 "+".
	want := `{"messaging_product":"whatsapp","to":"584226340289","type":"text","text":{"body":"hola","preview_url":false}}`
	if string(gotBody) != want {
		t.Errorf("body = %s, want %s", gotBody, want)
	}
}

func TestKapsoOutsideWindowIsAClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"Cannot send non-template messages outside the 24-hour window"}`))
	}))
	defer srv.Close()

	k := &Kapso{BaseURL: srv.URL, APIKey: "k", PhoneNumberID: "1", Client: srv.Client()}
	err := k.Send(context.Background(), "+584226340289", "hola")
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "24-hour window") {
		t.Fatalf("err = %v, want a 422 error naming the 24-hour window", err)
	}
}

func TestSMSSendRequest(t *testing.T) {
	var gotPath, gotUser, gotPass string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, gotPass, _ = r.BasicAuth()
		_ = r.ParseForm()
		form = r.PostForm
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	s := &SMS{BaseURL: srv.URL, AccountSID: "AC123", AuthToken: "tok", From: "+15550001111", Client: srv.Client()}
	if err := s.Send(context.Background(), "+584241112233", "hola"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/2010-04-01/Accounts/AC123/Messages.json" || gotUser != "AC123" || gotPass != "tok" {
		t.Errorf("path=%q auth=%q:%q", gotPath, gotUser, gotPass)
	}
	if form.Get("To") != "+584241112233" || form.Get("From") != "+15550001111" || form.Get("Body") != "hola" {
		t.Errorf("form = %v", form)
	}
}

func TestSendersWithoutCredentialsFailWithoutARequest(t *testing.T) {
	for name, s := range map[string]Sender{
		"kapso": &Kapso{BaseURL: "http://127.0.0.1:1"},
		"sms":   &SMS{BaseURL: "http://127.0.0.1:1"},
	} {
		if err := s.Send(context.Background(), "+584241112233", "hola"); !errors.Is(err, errMissingConfig) {
			t.Errorf("%s: err = %v, want errMissingConfig", name, err)
		}
	}
}

// Acceptance criterion 3: the env picks Kapso or SMS with no code change.
func TestNewSenderPicksChannelFromEnv(t *testing.T) {
	env := map[string]string{
		"KAPSO_API_KEY": "k", "KAPSO_PHONE_NUMBER_ID": "597907523413541",
		"TWILIO_ACCOUNT_SID": "AC1", "TWILIO_AUTH_TOKEN": "t", "TWILIO_PHONE_NUMBER": "+15550001111",
	}
	load := func(notifier string) config.Config {
		env["NOTIFIER"] = notifier
		return config.LoadFrom(func(k string) string { return env[k] })
	}

	for _, v := range []string{"", "kapso", " Kapso "} {
		s, err := NewSender(load(v))
		k, ok := s.(*Kapso)
		if err != nil || !ok {
			t.Fatalf("NOTIFIER=%q: got %T, %v; want *Kapso", v, s, err)
		}
		if k.BaseURL != KapsoBaseURL || k.APIKey != "k" || k.PhoneNumberID != "597907523413541" {
			t.Errorf("NOTIFIER=%q: Kapso = %+v", v, k)
		}
	}
	for _, v := range []string{"sms", "SMS"} {
		s, err := NewSender(load(v))
		sms, ok := s.(*SMS)
		if err != nil || !ok {
			t.Fatalf("NOTIFIER=%q: got %T, %v; want *SMS", v, s, err)
		}
		if sms.BaseURL != TwilioBaseURL || sms.AccountSID != "AC1" || sms.AuthToken != "t" || sms.From != "+15550001111" {
			t.Errorf("NOTIFIER=%q: SMS = %+v", v, sms)
		}
	}
	if s, err := NewSender(load("telegram")); err == nil {
		t.Fatalf("NOTIFIER=telegram: got %T, want an error", s)
	}
}
