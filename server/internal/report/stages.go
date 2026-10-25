package report

import (
	"fmt"
	"strconv"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Stage is one node of the horizontal track at the top of the page: the
// case's main steps, left to right. GPS points are not stages; they feed
// the route panel.
type Stage struct {
	Key   string
	Label string // one to three words
	Sub   string // a short detail under the label ("Coacción", "por WhatsApp")
	Time  string // time of day in VET, "" when unknown
	State string // done, current (next expected, pulsing while live), pending, skipped (passed without a time)
	Error bool   // a failure: the call never connected
	Mark  string // a small marker: "2 intentos", "respaldo web"
}

// StageState values.
const (
	StageDone    = "done"
	StageCurrent = "current"
	StagePending = "pending"
	StageSkipped = "skipped"
)

// Stages are the track's nodes for the record: for a call incident,
// Pidió ayuda → Llegó al servidor → Mamá llamó → Sonó → Contestó → Colgó →
// Veredicto → Familia avisada → Sellado; without a call, Pidió ayuda →
// Llegó al servidor → Familia avisada (those are never sealed). A step with
// a time is done; one passed without a time (no Twilio data, so no "Sonó")
// is skipped; while live, the first step after the last done one is
// current.
func Stages(rec Record, channel string, live bool) []Stage {
	inc := rec.Incident
	type step struct {
		Stage
		at     *time.Time
		passed bool // reached with no time of its own: no family message was needed
	}
	at := func(t time.Time) *time.Time { return &t }
	var steps []step
	add := func(key, label, sub string, t *time.Time) *step {
		steps = append(steps, step{Stage: Stage{Key: "stage-" + key, Label: label, Sub: sub}, at: t})
		return &steps[len(steps)-1]
	}
	add("trigger", "Pidió ayuda", triggerShort(inc.Trigger), at(inc.TriggeredAt))
	add("server", "Llegó al servidor", "", at(inc.CreatedAt))

	if inc.Branch == contract.BranchCall {
		var called, rang, answered, ended *time.Time
		var failed bool
		for i, c := range rec.Calls {
			if i == 0 {
				called = at(c.RequestedAt)
			}
			if c.RingingAt != nil && rang == nil {
				rang = c.RingingAt
			}
			if a, ok := c.AnsweredAt(); ok {
				answered = at(a)
			} else if c.PickedUpAt != nil && answered == nil {
				answered = c.PickedUpAt
			}
			if c.EndedAt != nil {
				ended, failed = c.EndedAt, c.Status == "failed"
			} else {
				ended, failed = nil, false // a later attempt is still going
			}
		}
		s := add("called", "Mamá llamó", "", called)
		if n := len(rec.Calls); n > 1 {
			s.Mark = strconv.Itoa(n) + " intentos"
		}
		add("rang", "Sonó", "", rang)
		add("answered", "Contestó", "", answered)
		s = add("ended", "Colgó", "", ended)
		if failed {
			s.Label, s.Error = "No conectó", true
		}
		if fellBack(rec) {
			s.Mark = "respaldo web"
		}
		var verdictAt *time.Time
		verdict, kind := headline(rec)
		for _, c := range rec.Calls {
			if c.VerdictAt != nil && string(c.Verdict) == kind {
				verdictAt = c.VerdictAt
			}
		}
		sub := ""
		if verdictAt != nil {
			sub = verdict
		}
		add("verdict", "Veredicto", sub, verdictAt)
	}

	s := add("family", "Familia avisada", familySummary(rec, channel), inc.NotifiedAt)
	if familyNotNeeded(rec) {
		s.Label, s.Sub, s.passed = "Familia sin aviso", "no hizo falta", true
	}
	if inc.Branch == contract.BranchCall {
		var sealed *time.Time
		if rec.Seal != nil {
			sealed = at(rec.Seal.SealedAt)
		}
		add("sealed", "Sellado", "", sealed)
	}

	last := -1
	for i, s := range steps {
		if s.at != nil || s.passed {
			last = i
		}
	}
	out := make([]Stage, len(steps))
	for i, s := range steps {
		st := s.Stage
		switch {
		case s.at != nil:
			st.State, st.Time = StageDone, clock(*s.at)
		case s.passed, i < last:
			st.State = StageSkipped
		case i == last+1 && live:
			st.State = StageCurrent
		default:
			st.State = StagePending
		}
		out[i] = st
	}
	return out
}

// familySummary is the family node's detail: the channel, then how many of
// the messages reached their phone once Kapso said so.
func familySummary(rec Record, channel string) string {
	if channel == "" {
		channel = "WhatsApp"
	}
	n, delivered := len(rec.Deliveries), 0
	for _, d := range rec.Deliveries {
		if d.Status == "delivered" || d.Status == "read" {
			delivered++
		}
	}
	if n == 0 || delivered == 0 {
		return "por " + channel
	}
	return fmt.Sprintf("✓✓ %d de %d", delivered, n)
}

// FamilyChip is one relative's message in the family panel.
type FamilyChip struct {
	Key       string
	Recipient string // masked
	Channel   string // "WhatsApp" or "SMS"
	Ticks     string // "✓", "✓✓" or "!"
	Label     string // "enviado", "entregado", "leído", "no se envió"
	Status    string // the delivery status, for the color
	Time      string // when it reached that status (server clock)
}

func familyChips(ds []Delivery) []FamilyChip {
	var out []FamilyChip
	for i, d := range ds {
		c := FamilyChip{Key: "fam-" + strconv.Itoa(i) + "-" + d.Status, Recipient: d.Recipient, Channel: channelName(d.Channel), Status: d.Status, Time: clock(d.SentAt)}
		switch d.Status {
		case "read":
			c.Ticks, c.Label = "✓✓", "leído"
			if d.ReadAt != nil {
				c.Time = clock(*d.ReadAt)
			}
		case "delivered":
			c.Ticks, c.Label = "✓✓", "entregado"
			if d.DeliveredAt != nil {
				c.Time = clock(*d.DeliveredAt)
			}
		case "failed":
			c.Ticks, c.Label = "!", "no se envió"
		default: // accepted, sent
			c.Ticks, c.Label = "✓", "enviado"
		}
		out = append(out, c)
	}
	return out
}

// channelName is a notify_deliveries channel as the page names it.
func channelName(c string) string {
	switch c {
	case "whatsapp":
		return "WhatsApp"
	case "sms":
		return "SMS"
	}
	return c
}

// familyNote is the family panel's line when there are no chips.
func familyNote(rec Record, channel string, live bool) string {
	if channel == "" {
		channel = "WhatsApp"
	}
	_, kind := headline(rec)
	switch {
	case rec.Incident.NotifiedAt != nil:
		return notifiedLabel(channel, rec.Device.FamilyNumbers) + "."
	case familyNotNeeded(rec):
		return "No hizo falta avisar a la familia: no hubo coacción."
	case live && rec.Incident.Branch == contract.BranchCall && kind == "none":
		return "Se avisa a la familia apenas haya veredicto."
	case live:
		return "Avisando a la familia…"
	}
	return "No se registró el aviso a la familia."
}

// familyNotNeeded reports whether the verdict leaves the family without a
// message: the calls say no duress and nothing was sent (the notifier's
// rule, docs/contract.md).
func familyNotNeeded(rec Record) bool {
	_, kind := headline(rec)
	return rec.Incident.NotifiedAt == nil && kind == string(contract.VerdictNoDuress)
}

// title is the header's state title.
func title(rec Record, live, callActive bool) (text, kind string) {
	text, kind = headline(rec)
	if kind != "none" || rec.Incident.Branch != contract.BranchCall {
		return text, kind
	}
	switch {
	case callActive:
		return "Llamada en curso", "waiting"
	case !live:
		return "Sin veredicto", "none"
	case len(rec.Calls) == 0:
		return "Mamá va a llamar…", "waiting"
	}
	return "Esperando veredicto…", "waiting"
}

// triggerShort names the gesture in two words or fewer.
func triggerShort(t contract.Trigger) string {
	switch t {
	case contract.TriggerVolumePattern:
		return "gesto de volumen"
	case contract.TriggerWidget:
		return "widget"
	case contract.TriggerTile:
		return "ajustes rápidos"
	case contract.TriggerInApp:
		return "desde la app"
	case contract.TriggerOffline:
		return "sin conexión"
	}
	return ""
}

var (
	weekdaysShort = [...]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	monthsShort   = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
)

// shortDate is "sáb 24 oct, 15:22" in VET.
func shortDate(t time.Time) string {
	t = t.In(vet)
	return fmt.Sprintf("%s %d %s, %s", weekdaysShort[t.Weekday()], t.Day(), monthsShort[t.Month()-1], t.Format("15:04"))
}

// elapsedText is a finished case's length, "1:05" or "1:02:03".
func elapsedText(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
