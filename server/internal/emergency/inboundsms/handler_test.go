package inboundsms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

type fakeIngester struct {
	err    error
	gotCh  ingest.Channel
	gotHop int
	gotLen int
	calls  int
}

func (f *fakeIngester) Ingest(_ context.Context, signed []byte, ch ingest.Channel, hops int) (ingest.Result, error) {
	f.calls++
	f.gotCh, f.gotHop, f.gotLen = ch, hops, len(signed)
	if f.err != nil {
		return ingest.Result{}, f.err
	}
	return ingest.Result{Ack: bytes.Repeat([]byte{7}, 76)}, nil
}

// stubProvider shows the handler needs nothing Twilio-specific.
type stubProvider struct {
	configured bool
	msg        Message
	err        error
}

func (stubProvider) Name() string       { return "stub" }
func (s stubProvider) Configured() bool { return s.configured }
func (s stubProvider) Parse(*http.Request) (Message, error) {
	return s.msg, s.err
}
func (stubProvider) Reply(w http.ResponseWriter, text string) {
	fmt.Fprintf(w, "reply:%q", text)
}

// smsBody is a well-formed SMS text: 98 bytes, the size of a signed payload.
var smsBody = payload.SMSText(bytes.Repeat([]byte{1}, 98))

func post(h *Handler, provider string, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sms/inbound/"+provider, strings.NewReader(body)))
	return rec
}

func TestHandlerRepliesWithTheAckSMS(t *testing.T) {
	ing := &fakeIngester{}
	h := newHandler(ing, stubProvider{configured: true, msg: Message{From: "+58414", Body: smsBody}})
	rec := post(h, "stub", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if want := fmt.Sprintf("reply:%q", payload.SMSText(bytes.Repeat([]byte{7}, 76))); rec.Body.String() != want {
		t.Errorf("body = %s, want %s", rec.Body, want)
	}
	if ing.calls != 1 || ing.gotCh != ingest.ChannelSMS || ing.gotHop != 0 || ing.gotLen != 98 {
		t.Errorf("Ingest calls=%d channel=%v hops=%d len=%d; want one sms call, 0 hops, 98 bytes", ing.calls, ing.gotCh, ing.gotHop, ing.gotLen)
	}
}

func TestHandlerStatusCodes(t *testing.T) {
	good := Message{From: "+58414", Body: smsBody}
	for _, c := range []struct {
		name     string
		provider Provider
		ing      Ingester
		path     string
		want     int
		wantBody string
	}{
		{"unknown provider", stubProvider{configured: true, msg: good}, &fakeIngester{}, "nope", 404, ""},
		{"provider not configured", stubProvider{msg: good}, &fakeIngester{}, "stub", 503, ""},
		{"no database", stubProvider{configured: true, msg: good}, nil, "stub", 503, ""},
		{"bad signature", stubProvider{configured: true, err: ErrUnauthenticated}, &fakeIngester{}, "stub", 403, ""},
		{"malformed form", stubProvider{configured: true, err: errors.New("bad form")}, &fakeIngester{}, "stub", 400, ""},
		{"body too large", stubProvider{configured: true, err: &http.MaxBytesError{Limit: 1}}, &fakeIngester{}, "stub", 413, ""},
		{"text not a payload", stubProvider{configured: true, msg: Message{Body: "hello"}}, &fakeIngester{}, "stub", 200, `reply:""`},
		{"bad payload signature", stubProvider{configured: true, msg: good}, &fakeIngester{err: payload.ErrSignature}, "stub", 200, `reply:""`},
		{"unknown device", stubProvider{configured: true, msg: good}, &fakeIngester{err: fmt.Errorf("x: %w", ingest.ErrUnknownDevice)}, "stub", 200, `reply:""`},
		{"conflict", stubProvider{configured: true, msg: good}, &fakeIngester{err: ingest.ErrConflict}, "stub", 200, `reply:""`},
		{"transient error", stubProvider{configured: true, msg: good}, &fakeIngester{err: errors.New("db down")}, "stub", 500, ""},
	} {
		rec := post(newHandler(c.ing, c.provider), c.path, "")
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
		if c.wantBody != "" && rec.Body.String() != c.wantBody {
			t.Errorf("%s: body = %s, want %s", c.name, rec.Body, c.wantBody)
		}
	}
}

func TestNewTreatsANilServiceAsNoDatabase(t *testing.T) {
	rec := post(New(nil, stubProvider{configured: true}), "stub", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandlerNeverLogsTheNumberOrTheText(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	msg := Message{From: "+584149998877", Body: smsBody}
	post(newHandler(&fakeIngester{err: ingest.ErrConflict}, stubProvider{configured: true, msg: msg}), "stub", "")
	post(newHandler(&fakeIngester{err: errors.New("db down")}, stubProvider{configured: true, msg: msg}), "stub", "")
	post(newHandler(&fakeIngester{}, stubProvider{configured: true, msg: Message{From: msg.From, Body: "MAMA1 junk"}}), "stub", "")

	if buf.Len() == 0 {
		t.Fatal("nothing was logged")
	}
	if out := buf.String(); strings.Contains(out, "584149998877") || strings.Contains(out, smsBody) || strings.Contains(out, "junk") {
		t.Errorf("log leaks the number or the text:\n%s", out)
	}
}

func TestTwilioEndToEndThroughTheHandler(t *testing.T) {
	ing := &fakeIngester{}
	h := newHandler(ing, NewTwilio(sampleToken, sampleBase))
	// The sample body is not a real payload, so it is dropped with an empty
	// TwiML reply; a bad signature never reaches ingest.
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, sampleRequest(sampleForm, sampleSignature))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<Response/>") {
		t.Errorf("signed sample = %d %s, want 200 with an empty TwiML reply", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, sampleRequest(sampleForm, "AAAAAAAAAAAAAAAAAAAAAAAAAAA="))
	if rec.Code != http.StatusForbidden || ing.calls != 0 {
		t.Errorf("bad signature = %d with %d ingest calls, want 403 and 0", rec.Code, ing.calls)
	}
}

func TestHandlerCapsTheBody(t *testing.T) {
	h := newHandler(&fakeIngester{}, NewTwilio(sampleToken, sampleBase))
	form := "Body=" + strings.Repeat("a", maxBodyBytes)
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, sampleRequest(form, sampleSignature))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}
