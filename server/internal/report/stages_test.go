package report

import (
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

func stageStates(st []Stage) string {
	var b strings.Builder
	for i, s := range st {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strings.TrimPrefix(s.Key, "stage-") + "=" + s.State)
	}
	return b.String()
}

// The track of a sealed call: every step done, "Sonó" passed without a time
// (no Twilio data).
func TestStagesOfASealedCall(t *testing.T) {
	st := Stages(sealedRecord(), "WhatsApp", false)
	want := "trigger=done server=done called=done rang=skipped answered=done ended=done verdict=done family=done sealed=done"
	if got := stageStates(st); got != want {
		t.Fatalf("stages = %s\nwant      %s", got, want)
	}
	if st[0].Time != "09:10:00" || st[0].Sub != "gesto de volumen" || st[6].Sub != "Coacción" || st[7].Label != "Familia avisada" || st[7].Sub != "por WhatsApp" || st[8].Time != "09:11:25" {
		t.Errorf("stages = %+v", st)
	}
}

// While live, the step after the last done one is current, the rest pending.
func TestStagesWhileTheCallRings(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Status, c.EndedAt, c.DurationSecs, c.Transcript, c.Verdict, c.VerdictAt = "in_progress", nil, nil, nil, "", nil
	c.RingingAt = ptr(at("2026-10-24T13:10:04Z"))
	rec.Incident.NotifiedAt = nil
	got := stageStates(Stages(rec, "WhatsApp", true))
	want := "trigger=done server=done called=done rang=done answered=current ended=pending verdict=pending family=pending sealed=pending"
	if got != want {
		t.Errorf("stages = %s\nwant      %s", got, want)
	}
	if got := stageStates(Stages(rec, "WhatsApp", false)); strings.Contains(got, "current") {
		t.Errorf("a case that is not live has a current step: %s", got)
	}
}

func TestStagesMarkRetriesAndTheFallback(t *testing.T) {
	rec := openRecord()
	failed := func(req string) Call {
		return Call{Status: "failed", RequestedAt: at(req), EndedAt: ptr(at(req).Add(20 * time.Second))}
	}
	rec.Calls = []Call{failed("2026-10-24T13:10:01Z"), failed("2026-10-24T13:10:22Z")}
	rec.Incident.NotifiedAt = nil
	st := Stages(rec, "WhatsApp", true)
	called, ended := st[2], st[5]
	if called.Mark != "2 intentos" || called.Time != "09:10:01" {
		t.Errorf("called = %+v", called)
	}
	if ended.Label != "No conectó" || !ended.Error || ended.Mark != "respaldo web" || ended.Time != "09:10:42" {
		t.Errorf("ended = %+v", ended)
	}
	if got := stageStates(st); !strings.Contains(got, "rang=skipped answered=skipped ended=done verdict=current") {
		t.Errorf("stages = %s", got)
	}
}

func TestStagesWithoutACall(t *testing.T) {
	rec := openRecord()
	rec.Incident.Branch, rec.Calls = contract.BranchImmediateAlert, nil
	got := stageStates(Stages(rec, "SMS", true))
	if got != "trigger=done server=done family=done" {
		t.Errorf("stages = %s", got)
	}
	if Stages(rec, "SMS", true)[2].Sub != "por SMS" {
		t.Error("the family step does not name the channel")
	}
}

func TestFamilyChipsAndSummary(t *testing.T) {
	rec := sealedRecord()
	rec.Deliveries = []Delivery{
		{Recipient: "+58 424 ···33", Status: "read", SentAt: at("2026-10-24T13:11:08Z"), DeliveredAt: ptr(at("2026-10-24T13:11:10Z")), ReadAt: ptr(at("2026-10-24T13:11:30Z"))},
		{Recipient: "+58 412 ···77", Status: "delivered", SentAt: at("2026-10-24T13:11:08Z"), DeliveredAt: ptr(at("2026-10-24T13:11:12Z"))},
		{Recipient: "+58 414 ···11", Status: "failed", Error: "Re-engagement message", SentAt: at("2026-10-24T13:11:08Z")},
		{Recipient: "+58 416 ···00", Status: "accepted", SentAt: at("2026-10-24T13:11:08Z")},
	}
	chips := familyChips(rec.Deliveries)
	want := []string{"✓✓ leído 09:11:30", "✓✓ entregado 09:11:12", "! no se envió 09:11:08", "✓ enviado 09:11:08"}
	for i, c := range chips {
		if got := c.Ticks + " " + c.Label + " " + c.Time; got != want[i] {
			t.Errorf("chip %d = %q, want %q", i, got, want[i])
		}
	}
	if s := familySummary(rec, "WhatsApp"); s != "✓✓ 2 de 4" {
		t.Errorf("summary = %q", s)
	}
	p := Build(rec, Options{Channel: "WhatsApp"})
	if len(p.Family) != 4 || p.FamilyNote != "" {
		t.Errorf("Build family = %+v %q", p.Family, p.FamilyNote)
	}
}

func TestTitles(t *testing.T) {
	rec := openRecord()
	rec.Calls[0].Verdict, rec.Calls[0].VerdictAt = "", nil
	if got, kind := title(rec, true, false); got != "Esperando veredicto…" || kind != "waiting" {
		t.Errorf("waiting = %q %q", got, kind)
	}
	if got, _ := title(rec, true, true); got != "Llamada en curso" {
		t.Errorf("calling = %q", got)
	}
	if got, _ := title(rec, false, false); got != "Sin veredicto" {
		t.Errorf("closed = %q", got)
	}
	rec.Calls = nil
	if got, _ := title(rec, true, false); got != "Mamá va a llamar…" {
		t.Errorf("before the call = %q", got)
	}
	if got, kind := title(sealedRecord(), false, false); got != "Coacción" || kind != "duress" {
		t.Errorf("verdict = %q %q", got, kind)
	}
	if got := shortDate(at("2026-10-24T13:10:00Z")); got != "sáb 24 oct, 09:10" {
		t.Errorf("shortDate = %q", got)
	}
	if got := elapsedText(65 * time.Second); got != "1:05" {
		t.Errorf("elapsed = %q", got)
	}
}

// While the case is live, a finished call waiting for its recording says so
// instead of "Audio no disponible".
func TestRecordingOnItsWay(t *testing.T) {
	if got := Build(openRecord(), Options{Now: at("2026-10-24T13:11:40Z")}).Calls[0].NoAudio; got != "Guardando la grabación de la llamada…" {
		t.Errorf("live = %q", got)
	}
	if got := Build(openRecord(), Options{Now: at("2026-10-24T15:00:00Z")}).Calls[0].NoAudio; !strings.HasPrefix(got, "Audio no disponible") {
		t.Errorf("closed = %q", got)
	}
}
