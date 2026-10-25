package report

import (
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// The duress alarm (#143): the moment the person says the duress phrase, the
// family page and the central detail turn into an alarm, before any verdict;
// then the verdict confirms it, softens it (unclear) or steps it down (no
// duress). It only changes what the page shows: the verdict, the family
// message, the PDF and the seal still come from the post-call transcript.
// The phrase itself never reaches the page: only that it was said, and when.

// Alarm levels.
const (
	AlarmHeard     = "heard"     // the phrase was heard; no verdict yet
	AlarmConfirmed = "confirmed" // the verdict is duress
	AlarmUnclear   = "unclear"   // the verdict is unclear: the family is told anyway
	AlarmCleared   = "cleared"   // heard while the call was on, then the verdict said no duress
)

// AlarmView is the alarm banner across the top of the page.
type AlarmView struct {
	Level  string
	Title  string // "¡Dijo la frase de alerta!", "Coacción confirmada"…
	Detail string // when the phrase was said, or why
	Status string // the family's side: "Familia avisada 22:56:23", "Confirmando…"; "" for none
	Pulse  bool   // strong motion: a red alarm on a case still followed live
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
	var a AlarmView
	switch contract.Verdict(kind) {
	case contract.VerdictDuress:
		a = AlarmView{Level: AlarmConfirmed, Title: "Coacción confirmada"}
	case contract.VerdictUnclear:
		a = AlarmView{Level: AlarmUnclear, Title: "Posible coacción: no está claro"}
	case contract.VerdictNoDuress:
		if !said {
			return nil
		}
		return &AlarmView{Level: AlarmCleared, Title: "La frase en vivo no se confirmó",
			Detail: "La transcripción final de la llamada dice: sin coacción."}
	default:
		if !said || rec.Incident.Branch != contract.BranchCall {
			return nil
		}
		a = AlarmView{Level: AlarmHeard, Title: "¡Dijo la frase de alerta!"}
	}
	switch {
	case said:
		a.Detail = "A los " + offset(h.secs) + " de la llamada"
		if !h.at.IsZero() {
			a.Detail += " · " + clock(h.at)
		}
	case a.Level == AlarmConfirmed:
		a.Detail = verdictReason(rec, contract.VerdictDuress)
	case a.Level == AlarmUnclear:
		a.Detail = verdictReason(rec, contract.VerdictUnclear)
	}
	switch {
	case a.Level == AlarmHeard && live:
		a.Status = "Confirmando…"
	case a.Level == AlarmHeard:
		a.Status = "Sin confirmar"
	case rec.Incident.NotifiedAt != nil:
		a.Status = "Familia avisada " + clock(*rec.Incident.NotifiedAt)
	case live:
		a.Status = "Avisando a la familia…"
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
