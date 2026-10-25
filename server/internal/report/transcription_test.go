package report

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/twiliosig"
)

const (
	testTwilioToken      = "test-twilio-token" // secret-scan:allow (test fixture)
	testTranscriptionURL = "https://mama.example" + TranscriptionPath
)

// memLiveTurns is an in-memory LiveTurnStore.
type memLiveTurns struct {
	mu   sync.Mutex
	sids []string
	got  []LiveTurn
	err  error
}

func (m *memLiveTurns) SaveLiveTurn(_ context.Context, callSID string, t LiveTurn) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	m.sids, m.got = append(m.sids, callSID), append(m.got, t)
	return true, nil
}

func transcriptionHook(store LiveTurnStore) *TranscriptionWebhook {
	return &TranscriptionWebhook{AuthToken: testTwilioToken, URL: testTranscriptionURL, Store: store,
		Now: func() time.Time { return at("2026-10-24T13:10:30Z") }}
}

// contentEvent is a transcription-content callback as Twilio posts it.
func contentEvent(track, seq, transcript, final string) url.Values {
	return url.Values{
		"AccountSid":         {"ACtest"},
		"CallSid":            {"CA1"},
		"TranscriptionSid":   {"GT1"},
		"Timestamp":          {"2026-10-24T13:10:12.250Z"},
		"SequenceId":         {seq},
		"TranscriptionEvent": {"transcription-content"},
		"LanguageCode":       {"es-US"},
		"Track":              {track},
		"TranscriptionData":  {`{"transcript":"` + transcript + `","confidence":0.93}`},
		"Final":              {final},
	}
}

func postTranscription(h http.Handler, form url.Values, sig string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, TranscriptionPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sig != "" {
		r.Header.Set(twiliosig.Header, sig)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func signTranscription(form url.Values) string {
	return base64.StdEncoding.EncodeToString(twiliosig.Sign(testTwilioToken, testTranscriptionURL, form))
}

// #143: each final phrase is stored on the call with its track, its
// sequence and Twilio's time.
func TestTranscriptionWebhookStoresFinalPhrases(t *testing.T) {
	store := &memLiveTurns{}
	h := transcriptionHook(store)
	for _, form := range []url.Values{
		contentEvent(TrackOutbound, "3", "¿Aló? Mi amor, ¿dónde estás?", "true"),
		contentEvent(TrackInbound, "4", "  Oye, dile a la tía Carmen que llego tarde. ", "true"),
	} {
		if w := postTranscription(h, form, signTranscription(form)); w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
	}
	said := at("2026-10-24T13:10:12.250Z")
	want := []LiveTurn{
		{Track: TrackOutbound, SequenceID: 3, Message: "¿Aló? Mi amor, ¿dónde estás?", SaidAt: said},
		{Track: TrackInbound, SequenceID: 4, Message: "Oye, dile a la tía Carmen que llego tarde.", SaidAt: said},
	}
	if !reflect.DeepEqual(store.got, want) || strings.Join(store.sids, " ") != "CA1 CA1" {
		t.Errorf("stored %+v on %v\nwant %+v", store.got, store.sids, want)
	}
}

// Partial results, other tracks, empty phrases and the other events are
// answered 200 and store nothing; a phrase with no Timestamp takes now.
func TestTranscriptionWebhookSkipsWhatIsNotAPhrase(t *testing.T) {
	store := &memLiveTurns{}
	h := transcriptionHook(store)
	partial := contentEvent(TrackInbound, "5", "dile a la", "false")
	otherTrack := contentEvent("both_tracks", "6", "hola", "true")
	empty := contentEvent(TrackInbound, "7", "  ", "true")
	badData := contentEvent(TrackInbound, "8", "x", "true")
	badData.Set("TranscriptionData", "not json")
	started := url.Values{"CallSid": {"CA1"}, "TranscriptionEvent": {"transcription-started"}, "SequenceId": {"1"}}
	stopped := url.Values{"CallSid": {"CA1"}, "TranscriptionEvent": {"transcription-stopped"}, "SequenceId": {"9"}}
	failed := url.Values{"CallSid": {"CA1"}, "TranscriptionEvent": {"transcription-error"},
		"TranscriptionErrorCode": {"32655"}, "TranscriptionError": {"Provider Unavailable"}}
	for _, form := range []url.Values{partial, otherTrack, empty, badData, started, stopped, failed} {
		if w := postTranscription(h, form, signTranscription(form)); w.Code != http.StatusOK {
			t.Fatalf("%v: status %d", form, w.Code)
		}
	}
	if len(store.got) != 0 {
		t.Fatalf("stored %+v, want nothing", store.got)
	}
	noTime := contentEvent(TrackInbound, "10", "ya voy", "true")
	noTime.Del("Timestamp")
	postTranscription(h, noTime, signTranscription(noTime))
	if len(store.got) != 1 || !store.got[0].SaidAt.Equal(at("2026-10-24T13:10:30Z")) {
		t.Errorf("stored %+v, want the phrase at the server's now", store.got)
	}
}

func TestTranscriptionWebhookRejects(t *testing.T) {
	form := contentEvent(TrackInbound, "4", "hola", "true")
	otherURL := base64.StdEncoding.EncodeToString(twiliosig.Sign(testTwilioToken, "https://other.example"+TranscriptionPath, form))
	for _, tc := range []struct {
		name string
		h    *TranscriptionWebhook
		sig  string
		want int
	}{
		{"no signature", transcriptionHook(&memLiveTurns{}), "", http.StatusForbidden},
		{"signed for another URL", transcriptionHook(&memLiveTurns{}), otherURL, http.StatusForbidden},
		{"no auth token", &TranscriptionWebhook{URL: testTranscriptionURL, Store: &memLiveTurns{}}, signTranscription(form), http.StatusServiceUnavailable},
		{"store fails", transcriptionHook(&memLiveTurns{err: errors.New("db down")}), signTranscription(form), http.StatusInternalServerError},
	} {
		if w := postTranscription(tc.h, form, tc.sig); w.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

// liveRecord is openRecord while its call is on: answered at 13:10:06, no
// post-call transcript yet, and what Twilio heard so far, the duress phrase
// included.
func liveRecord() Record {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Status, c.Transcript, c.EndedAt, c.DurationSecs = "in_progress", nil, nil, nil
	c.Verdict, c.VerdictAt, c.VerdictReason = "", nil, ""
	c.RingingAt, c.PickedUpAt = ptr(at("2026-10-24T13:10:03Z")), ptr(at("2026-10-24T13:10:06Z"))
	c.LiveTurns = []contract.TranscriptTurn{
		{Role: "agent", Message: "¿Aló? Mi amor, ¿dónde estás?", TimeInCallSecs: 1},
		{Role: "user", Message: "Hola mami. Oye, dile a la tía Carmen que llego tarde.", TimeInCallSecs: 6.5},
	}
	rec.Incident.NotifiedAt = nil
	return rec
}

var liveNow = at("2026-10-24T13:10:20Z")

// While the call is on, the family page shows what Twilio heard, every turn
// visible on a phone, and the turn that says the phrase marked as the alarm
// (#143): only the post-call transcript is judged, but the page alarms at once.
func TestFamilyPageShowsTheLiveTurnsDuringTheCall(t *testing.T) {
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: liveNow}
	p := Build(liveRecord(), opts)
	if !p.CallActive || p.Call == nil || !p.Call.Provisional || len(p.Call.Turns) != 2 || p.Call.More {
		t.Fatalf("active %v call %+v", p.CallActive, p.Call)
	}
	for i, tv := range p.Call.Turns {
		if marked := i == 1; tv.Phrase != marked || tv.Alarm != marked || !tv.Keep {
			t.Errorf("turn %d = %+v, want kept and marked %v", i, tv, marked)
		}
	}
	if p.Call.Turns[0].Speaker != "Mamá" || p.Call.Turns[1].Speaker != "Tu familiar" || p.Call.Turns[1].Offset != "0:06" {
		t.Errorf("turns %+v", p.Call.Turns)
	}
	s, err := RenderSections(liveRecord(), opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	call := html.UnescapeString(s.Sections["call"])
	for _, want := range []string{"Llamada en curso", `data-key="call-card-1"`, `data-key="turn-1"`, "data-provisional",
		"dile a la tía Carmen", "En vivo: al colgar llegan la grabación y la transcripción definitiva."} {
		if !strings.Contains(call, want) {
			t.Errorf("call section lacks %q:\n%s", want, call)
		}
	}
	if strings.Contains(call, "La frase acordada") || !strings.Contains(call, "Frase de alerta · en vivo") || !strings.Contains(call, "phrase alarm") {
		t.Errorf("the live phrase turn is not tagged as live and red:\n%s", call)
	}
}

// The post-call transcript replaces the live turns, in the same card: the
// phrase is marked, and the recording comes as before.
func TestPostCallTranscriptReplacesTheLiveTurns(t *testing.T) {
	rec := openRecord()
	rec.Calls[0].LiveTurns = liveRecord().Calls[0].LiveTurns
	p := Build(rec, Options{Token: testToken, Now: at("2026-10-24T13:11:10Z")})
	if p.Call == nil || p.Call.Provisional || len(p.Call.Turns) != len(testTranscript()) || !p.Call.Turns[3].Phrase {
		t.Fatalf("call %+v", p.Call)
	}
	if p.Call.N != 1 {
		t.Errorf("not the same call card: %+v", p.Call)
	}
}

// A sealed page shows only what the seal covers, even with live turns
// stored and no post-call transcript.
func TestSealedPageNeverShowsLiveTurns(t *testing.T) {
	rec := sealedRecord()
	rec.Calls[0].Transcript, rec.Calls[0].Status = nil, "in_progress"
	rec.Calls[0].LiveTurns = liveRecord().Calls[0].LiveTurns
	p := Build(rec, Options{Token: testToken, Now: at("2026-10-24T13:12:00Z")})
	for _, c := range p.Calls {
		if c.Provisional || len(c.Turns) != 0 {
			t.Errorf("sealed call view %+v", c)
		}
	}
}

// The seal and the PDF are built from the post-call transcript only: live
// turns change neither.
func TestSealAndPDFIgnoreLiveTurns(t *testing.T) {
	rec := openRecord()
	with := openRecord()
	with.Calls[0].LiveTurns = []contract.TranscriptTurn{{Role: "user", Message: "algo que Twilio oyó", TimeInCallSecs: 3}}
	pub := testKey().Public().(ed25519.PublicKey)
	sealedAt := at("2026-10-24T13:11:25Z")
	a, err := Document(rec, sealedAt, pub)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Document(with, sealedAt, pub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("the seal's document changed with live turns:\n%s\n%s", a, b)
	}
	opts := PDFOptions{Token: testToken, Channel: "WhatsApp", Now: at("2026-10-24T13:12:00Z")}
	pa, err := buildPDF(rec, opts)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := buildPDF(with, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pa, pb) {
		t.Error("the PDF changed with live turns")
	}
}

// Stored live turns load in the order they were said across both tracks,
// counted from the answer, and stay loaded after the post-call transcript
// (the alarm still needs them, #143). A repeated callback and an unknown call
// store nothing.
func TestSaveAndLoadLiveTurns(t *testing.T) {
	pool := newDB(t)
	s := NewDBStore(pool)
	ctx := context.Background()
	exec(t, pool, `UPDATE calls SET status = 'in_progress', transcript = NULL, call_sid = 'CA1' WHERE conversation_id = 'conv_01'`)
	var callID int64
	if err := pool.QueryRow(`SELECT id FROM calls WHERE conversation_id = 'conv_01'`).Scan(&callID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgress(ctx, callID, "in-progress", at("2026-10-24T13:10:06Z")); err != nil {
		t.Fatal(err)
	}
	turns := []LiveTurn{
		{Track: TrackInbound, SequenceID: 2, Message: "Hola mami.", SaidAt: at("2026-10-24T13:10:10Z")},
		{Track: TrackOutbound, SequenceID: 1, Message: "¿Aló? Mi amor.", SaidAt: at("2026-10-24T13:10:07.5Z")},
		{Track: TrackOutbound, SequenceID: 2, Message: "¿Dónde estás?", SaidAt: at("2026-10-24T13:10:12Z")},
	}
	for _, lt := range turns {
		if ok, err := s.SaveLiveTurn(ctx, "CA1", lt); err != nil || !ok {
			t.Fatalf("SaveLiveTurn(%+v) = %v, %v", lt, ok, err)
		}
	}
	if ok, err := s.SaveLiveTurn(ctx, "CA1", LiveTurn{Track: TrackInbound, SequenceID: 2, Message: "otra cosa", SaidAt: at("2026-10-24T13:10:11Z")}); err != nil || ok {
		t.Errorf("repeated callback stored = %v, %v", ok, err)
	}
	if ok, err := s.SaveLiveTurn(ctx, "CA404", turns[0]); err != nil || ok {
		t.Errorf("unknown call stored = %v, %v", ok, err)
	}
	rec, err := s.Load(ctx, testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	want := []contract.TranscriptTurn{
		{Role: "agent", Message: "¿Aló? Mi amor.", TimeInCallSecs: 1.5},
		{Role: "user", Message: "Hola mami.", TimeInCallSecs: 4},
		{Role: "agent", Message: "¿Dónde estás?", TimeInCallSecs: 6},
	}
	if got := rec.Calls[0].LiveTurns; !reflect.DeepEqual(got, want) {
		t.Errorf("LiveTurns = %+v\nwant %+v", got, want)
	}
	exec(t, pool, `UPDATE calls SET status = 'done', transcript = '[{"role":"agent","message":"¿Aló?","time_in_call_secs":0}]' WHERE id = $1`, callID)
	if rec, err = s.Load(ctx, testIncidentID); err != nil || !reflect.DeepEqual(rec.Calls[0].LiveTurns, want) {
		t.Errorf("after the post-call transcript: LiveTurns %+v, %v", rec.Calls[0].LiveTurns, err)
	}
	if p := Build(rec, Options{Token: testToken, Now: at("2026-10-24T13:12:00Z")}); p.Call == nil || p.Call.Provisional || len(p.Call.Turns) != 1 {
		t.Errorf("after the post-call transcript the page shows %+v, want the transcript", p.Call)
	}
}

// The central's detail shows the same live turns while the call is on.
func TestCentralCaseShowsTheLiveTurns(t *testing.T) {
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: liveNow}
	d := buildCentralCase(liveRecord(), CaseExtras{}, opts)
	if d.Call == nil || !d.Call.Provisional || len(d.Call.Turns) != 2 {
		t.Fatalf("central call %+v", d.Call)
	}
	s, err := centralCaseSnapshot(d, opts.Now)
	if err != nil {
		t.Fatal(err)
	}
	if call := html.UnescapeString(s.Sections["call"]); !strings.Contains(call, "dile a la tía Carmen") || !strings.Contains(call, "data-provisional") {
		t.Errorf("central call section: %s", call)
	}
}
