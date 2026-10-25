package report

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"html"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// The moment Twilio hears the phrase, the page alarms: red, pulsing, with
// when it was said in the call and on the clock, before any verdict (#143).
func TestAlarmFiresWhenThePhraseIsHeardLive(t *testing.T) {
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: liveNow}
	p := Build(liveRecord(), opts)
	want := &AlarmView{Level: AlarmHeard, Title: "¡Dijo la frase de alerta!", Detail: "A los 0:06 de la llamada · 09:10:12", Status: "Confirmando…", Pulse: true}
	if !reflect.DeepEqual(p.Alarm, want) {
		t.Fatalf("alarm = %+v\nwant  %+v", p.Alarm, want)
	}
	s, err := RenderSections(liveRecord(), opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	alarm := html.UnescapeString(s.Sections["alarm"])
	for _, w := range []string{`data-alarm-level="heard"`, "¡Dijo la frase de alerta!", "A los 0:06 de la llamada · 09:10:12", "Confirmando…", `class="alarm-frame"`, `role="alert"`} {
		if !strings.Contains(alarm, w) {
			t.Errorf("alarm section lacks %q:\n%s", w, alarm)
		}
	}
}

// Live turns that do not say the phrase raise no alarm, and the section is
// empty so the page keeps its layout.
func TestNoAlarmWithoutThePhrase(t *testing.T) {
	rec := liveRecord()
	rec.Calls[0].LiveTurns[1].Message = "Hola mami, todo bien, ya voy saliendo."
	opts := Options{Token: testToken, Now: liveNow}
	if p := Build(rec, opts); p.Alarm != nil {
		t.Errorf("alarm = %+v", p.Alarm)
	}
	s, err := RenderSections(rec, opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(s.Sections["alarm"]) != "" {
		t.Errorf("alarm section = %q", s.Sections["alarm"])
	}
}

// The duress verdict confirms it and says when the family was told; once
// sealed the banner stays, still: no pulse and no frame.
func TestAlarmConfirmedThenStill(t *testing.T) {
	rec := openRecord()
	p := Build(rec, Options{Token: testToken, CanSeal: true, Now: at("2026-10-24T13:11:10Z")})
	want := &AlarmView{Level: AlarmConfirmed, Title: "Coacción confirmada", Detail: "A los 0:15 de la llamada · 09:10:19", Status: "Familia avisada 09:11:08", Pulse: true}
	if !reflect.DeepEqual(p.Alarm, want) {
		t.Fatalf("alarm = %+v\nwant  %+v", p.Alarm, want)
	}
	if v := p.Stages[6]; v.Key != "stage-verdict" || !v.Alarm {
		t.Errorf("verdict node %+v is not the alarm", v)
	}
	if !p.Call.Turns[3].Phrase || !p.Call.Turns[3].Alarm {
		t.Errorf("phrase turn %+v is not red", p.Call.Turns[3])
	}

	sealed := sealedRecord()
	opts := Options{Token: testToken, CanSeal: true, Now: at("2026-10-24T13:12:00Z")}
	if p := Build(sealed, opts); p.Alarm == nil || p.Alarm.Level != AlarmConfirmed || p.Alarm.Pulse {
		t.Errorf("sealed alarm = %+v, want confirmed and still", p.Alarm)
	}
	s, err := RenderSections(sealed, opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	if a := s.Sections["alarm"]; !strings.Contains(a, `data-alarm-level="confirmed"`) || strings.Contains(a, "alarm-frame") || strings.Contains(a, " pulse") {
		t.Errorf("sealed alarm section:\n%s", a)
	}
}

// An unclear verdict is the softer warning: amber, never the red frame.
func TestAlarmUnclearIsSofter(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Transcript = []contract.TranscriptTurn{{Role: "user", Message: "Sí, mami, vine con Pablo… bueno, solo.", TimeInCallSecs: 4}}
	c.Verdict, c.VerdictReason = contract.VerdictUnclear, "the user said part of the duress phrase (2 of 4 key words)"
	p := Build(rec, Options{Token: testToken, Now: at("2026-10-24T13:11:10Z")})
	if p.Alarm == nil || p.Alarm.Level != AlarmUnclear || p.Alarm.Red() || p.Alarm.Pulse || p.Alarm.Status != "Familia avisada 09:11:08" {
		t.Errorf("alarm = %+v", p.Alarm)
	}
}

// Twilio heard the phrase but the final transcript did not, and the verdict
// says no duress: the alarm steps down to a calm note, never left red.
func TestAlarmStepsDownWhenTheVerdictSaysNoDuress(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.LiveTurns = liveRecord().Calls[0].LiveTurns // the phrase, heard live
	c.PickedUpAt = ptr(at("2026-10-24T13:10:04Z"))
	c.Transcript = []contract.TranscriptTurn{{Role: "user", Message: "Hola mami, ya voy llegando a la casa.", TimeInCallSecs: 4}}
	c.Verdict, c.VerdictReason = contract.VerdictNoDuress, "the user did not say the duress phrase"
	rec.Incident.NotifiedAt = nil
	opts := Options{Token: testToken, Now: at("2026-10-24T13:11:10Z")}
	p := Build(rec, opts)
	if p.Alarm == nil || p.Alarm.Level != AlarmCleared || p.Alarm.Red() || p.Alarm.Pulse || p.Alarm.Title != "La frase en vivo no se confirmó" {
		t.Fatalf("alarm = %+v", p.Alarm)
	}
	for _, tv := range p.Call.Turns {
		if tv.Phrase || tv.Alarm {
			t.Errorf("a final turn is still marked: %+v", tv)
		}
	}
	s, err := RenderSections(rec, opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	if a := s.Sections["alarm"]; !strings.Contains(a, `data-alarm-level="cleared"`) || strings.Contains(a, "alarm-frame") {
		t.Errorf("alarm section:\n%s", a)
	}
}

// The phrase itself never reaches the page: not in the alarm, not in any
// attribute, and nowhere at all while nobody has said it.
func TestAlarmKeepsThePhraseOutOfThePage(t *testing.T) {
	const secret = "el gato azul come mangos"
	rec := liveRecord()
	rec.Device.DuressPhrase = secret
	rec.Calls[0].LiveTurns[1].Message = "Hola mami. Oye, el gato azul come mangos."
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: liveNow}
	s, err := RenderSections(rec, opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Sections["alarm"], `data-alarm-level="heard"`) {
		t.Fatalf("no alarm:\n%s", s.Sections["alarm"])
	}
	if strings.Contains(html.UnescapeString(s.Sections["alarm"]), "gato") {
		t.Errorf("the alarm quotes the phrase:\n%s", s.Sections["alarm"])
	}

	rec.Calls[0].LiveTurns[1].Message = "Hola mami, todo bien."
	s, err = RenderSections(rec, opts, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.UnescapeString(string(raw)), "gato azul") {
		t.Errorf("the phrase is in the snapshot:\n%s", raw)
	}
	page := serve(t, fakeSource{testToken: rec}, "/i/"+testToken)
	if strings.Contains(html.UnescapeString(page.Body.String()), "gato azul") {
		t.Error("the phrase is in the page")
	}
}

// The alarm is the page only: a phrase in the live turns changes neither
// the seal's document nor the PDF, which come from the post-call transcript.
func TestLiveAlarmChangesNeitherSealNorPDF(t *testing.T) {
	rec := openRecord()
	rec.Calls[0].Transcript = []contract.TranscriptTurn{{Role: "user", Message: "Hola mami, ya voy llegando.", TimeInCallSecs: 4}}
	rec.Calls[0].Verdict, rec.Calls[0].VerdictReason = contract.VerdictNoDuress, "the user did not say the duress phrase"
	with := openRecord()
	with.Calls[0].Transcript, with.Calls[0].Verdict, with.Calls[0].VerdictReason = rec.Calls[0].Transcript, rec.Calls[0].Verdict, rec.Calls[0].VerdictReason
	with.Calls[0].LiveTurns = liveRecord().Calls[0].LiveTurns
	if Build(with, Options{Token: testToken}).Alarm == nil {
		t.Fatal("no alarm from the live phrase")
	}
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
		t.Errorf("the seal's document changed with a live phrase:\n%s\n%s", a, b)
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
		t.Error("the PDF changed with a live phrase")
	}
}

// The central detail mounts the same alarm, and its list turns a duress
// case's row into the red alarm, pulsing while the case has news.
func TestCentralShowsTheAlarm(t *testing.T) {
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: liveNow}
	d := buildCentralCase(liveRecord(), CaseExtras{}, opts)
	s, err := centralCaseSnapshot(d, opts.Now)
	if err != nil {
		t.Fatal(err)
	}
	if a := s.Sections["alarm"]; !strings.Contains(a, `data-alarm-level="heard"`) || !strings.Contains(a, "alarm-frame") {
		t.Errorf("central alarm section:\n%s", a)
	}

	now := at("2026-10-24T13:12:00Z")
	item := CaseItem{ID: testIncidentID, Branch: contract.BranchCall, TriggeredAt: now.Add(-2 * time.Minute), CreatedAt: now.Add(-2 * time.Minute),
		Verdict: contract.VerdictDuress, VerdictReason: "the user said the duress phrase", LastAt: now.Add(-time.Minute)}
	if r := caseRow(item, now); !r.Alarm || !r.AlarmPulse {
		t.Errorf("fresh duress row = alarm %v pulse %v", r.Alarm, r.AlarmPulse)
	}
	if r := caseRow(item, now.Add(time.Hour)); !r.Alarm || r.AlarmPulse {
		t.Errorf("old duress row = alarm %v pulse %v, want still", r.Alarm, r.AlarmPulse)
	}
	item.Verdict = contract.VerdictUnclear
	if r := caseRow(item, now); r.Alarm {
		t.Error("an unclear row is the red alarm")
	}
}
