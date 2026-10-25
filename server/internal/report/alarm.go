package report

import (
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// The duress alarm (#143): the moment the person says the duress phrase, a
// red chip beside the person's name on the family page and the central
// detail says so, before any verdict; then the verdict confirms it, softens
// it (unclear) or steps it down (no duress). It only changes what the page shows: the verdict, the family
// message, the PDF and the seal still come from the post-call transcript.
// The phrase itself never reaches the page: only that it was said, and when.

// Alarm levels.
const (
	AlarmHeard     = "heard"     // the phrase was heard; no verdict yet
	AlarmConfirmed = "confirmed" // the verdict is duress
	AlarmUnclear   = "unclear"   // the verdict is unclear: the family is told anyway
	AlarmCleared   = "cleared"   // heard while the call was on, then the verdict said no duress
)

// AlarmView is the alarm chip beside the person's name in the header: a
// short text, a smaller second line, and the whole sentence as a tooltip.
type AlarmView struct {
	Level string
	Chip  string // "Frase de alerta · 0:14", "Coacción confirmada"…
	Sub   string // the smaller line: "23:10:50 · confirmando…", "Familia avisada 23:14:57"
	Hint  string // the tooltip: what happened, in one sentence
	Pulse bool   // a red alarm on a case still followed live: it pulses, with a frame around the screen
}

// Red reports whether the alarm is the red one: the phrase heard, or the
// duress confirmed.
func (a AlarmView) Red() bool { return a.Level == AlarmHeard || a.Level == AlarmConfirmed }

// heard is where the duress phrase was said: a user turn that says it on its
// own, by the rules phraseTurns marks the transcript with.
type heard struct {
	secs  float64   // seconds into the call
	at    time.Time // time of day; zero when the call's start is unknown
	final bool      // in the post-call transcript, not only in the live turns
}

// phraseHeard looks for the phrase in the newest call that has it: first in
// its post-call transcript, then in what Twilio heard while it lasted (#143,
// the live turns, kept after the call so a phrase heard live and not
// confirmed can still step the alarm down).
func phraseHeard(rec Record) (heard, bool) {
	phrase := rec.Device.DuressPhrase
	for i := len(rec.Calls) - 1; i >= 0; i-- {
		c := rec.Calls[i]
		if j, ok := firstMarked(c.Transcript, phrase); ok {
			h := heard{secs: c.Transcript[j].TimeInCallSecs, final: true}
			if start, known := c.AnsweredAt(); known {
				h.at = start.Add(secs(h.secs))
			}
			return h, true
		}
		if j, ok := firstMarked(c.LiveTurns, phrase); ok {
			h := heard{secs: c.LiveTurns[j].TimeInCallSecs}
			if c.PickedUpAt != nil {
				h.at = c.PickedUpAt.Add(secs(h.secs))
			}
			return h, true
		}
	}
	return heard{}, false
}

// firstMarked is the first turn phraseTurns marks.
func firstMarked(turns []contract.TranscriptTurn, phrase string) (int, bool) {
	marked := phraseTurns(turns, phrase)
	for j := range turns {
		if marked[j] {
			return j, true
		}
	}
	return 0, false
}

// alarmView is the page's alarm, or nil when there is nothing to alarm
// about. live is the page's: the case is still followed live.
func alarmView(rec Record, live bool) *AlarmView {
	h, said := phraseHeard(rec)
	_, kind := headline(rec)
	when := ""
	if said {
		when = "a los " + offset(h.secs) + " de la llamada"
		if !h.at.IsZero() {
			when += " (" + clock(h.at) + ")"
		}
	}
	family := ""
	switch {
	case rec.Incident.NotifiedAt != nil:
		family = "Familia avisada " + clock(*rec.Incident.NotifiedAt)
	case live:
		family = "Avisando a la familia…"
	}
	var a AlarmView
	switch contract.Verdict(kind) {
	case contract.VerdictDuress:
		a = AlarmView{Level: AlarmConfirmed, Chip: "Coacción confirmada", Sub: family, Hint: "Coacción confirmada por la transcripción final"}
	case contract.VerdictUnclear:
		a = AlarmView{Level: AlarmUnclear, Chip: "Posible coacción", Sub: family, Hint: "El veredicto no está claro: igual se avisa a la familia"}
	case contract.VerdictNoDuress:
		if !said {
			return nil
		}
		return &AlarmView{Level: AlarmCleared, Chip: "Frase no confirmada", Sub: "la transcripción final dice sin coacción",
			Hint: "En vivo se oyó la frase de alerta " + when + ", pero la transcripción final dice sin coacción."}
	default:
		if !said || rec.Incident.Branch != contract.BranchCall {
			return nil
		}
		a = AlarmView{Level: AlarmHeard, Chip: "Frase de alerta · " + offset(h.secs), Hint: "Dijo la frase de alerta"}
		sub := "sin confirmar"
		if live {
			sub = "confirmando…"
		}
		if !h.at.IsZero() {
			sub = clock(h.at) + " · " + sub
		}
		a.Sub = sub
	}
	switch {
	case said:
		a.Hint += " " + when + "."
	default:
		if r := verdictReason(rec, contract.Verdict(kind)); r != "" {
			a.Hint += ". " + r
		} else {
			a.Hint += "."
		}
	}
	if family != "" && a.Level != AlarmHeard {
		a.Hint += " " + family + "."
	}
	a.Pulse = a.Red() && live
	return &a
}

// verdictReason is the Spanish reason of the newest call with verdict v, or
// "" when the detector's reason is not one the page knows.
func verdictReason(rec Record, v contract.Verdict) string {
	for i := len(rec.Calls) - 1; i >= 0; i-- {
		if c := rec.Calls[i]; c.Verdict == v {
			if es := reasonES(c.VerdictReason); es != "" {
				return "Según la llamada, " + es + "."
			}
			return ""
		}
	}
	return ""
}
