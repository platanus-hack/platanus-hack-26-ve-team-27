package report

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// The states a case has on the central dashboard (#99), in the order
// caseState checks them.
const (
	CaseCancelled = "cancelado" // the person called it off: nobody was notified
	CaseAlert     = "alerta"    // a call's verdict is duress
	CaseCalling   = "llamada"   // Mamá is calling: dialing, ringing or talking
	CaseActive    = "activo"    // still happening: something stored in the last 30 min
	CaseClosed    = "cerrado"   // sealed, or nothing new for 30 min
)

// caseStates are the states in the order the counters show them.
var caseStates = []string{CaseAlert, CaseCalling, CaseActive, CaseCancelled, CaseClosed}

// caseStateLabel is the state's chip text.
func caseStateLabel(state string) string {
	switch state {
	case CaseCancelled:
		return "Cancelado por la persona"
	case CaseAlert:
		return "Alerta"
	case CaseCalling:
		return "En llamada"
	case CaseActive:
		return "Activo"
	}
	return "Cerrado"
}

// caseCountLabel names a counter of n cases.
func caseCountLabel(state string, n int) string {
	one := n == 1
	pick := func(singular, plural string) string {
		if one {
			return singular
		}
		return plural
	}
	switch state {
	case CaseCancelled:
		return pick("Cancelado", "Cancelados")
	case CaseAlert:
		return pick("Alerta", "Alertas")
	case CaseCalling:
		return "En llamada"
	case CaseActive:
		return pick("Activo", "Activos")
	}
	return pick("Cerrado", "Cerrados")
}

// callIsActive reports whether a call is still being placed or talked.
func callIsActive(status string) bool { return status == "requested" || status == "in_progress" }

// caseState is the case's state at now:
//   - cancelado: an explicit cancellation, or a no_duress verdict that sent
//     no family message (#106: the "todo bien" phrase notifies nobody);
//   - alerta: any call's verdict is duress, sealed or not;
//   - cerrado: sealed (and not an alert);
//   - llamada: the newest call is being placed or talked, and was asked for
//     less than 15 min ago (the report's limit for a live call);
//   - activo: something was stored in the last 30 min (the report's live
//     window);
//   - cerrado otherwise.
func caseState(c CaseItem, now time.Time) string {
	switch {
	case c.Cancelled || (c.Verdict == contract.VerdictNoDuress && c.NotifiedAt == nil):
		return CaseCancelled
	case c.Verdict == contract.VerdictDuress:
		return CaseAlert
	case c.SealedAt != nil:
		return CaseClosed
	case c.Call != nil && callIsActive(c.Call.Status) && now.Sub(c.Call.RequestedAt) < staleCall:
		return CaseCalling
	case now.Sub(c.LastAt) < liveWindow:
		return CaseActive
	}
	return CaseClosed
}

// CaseRow is one case as the central shows it: a list row, and the
// detail's header.
type CaseRow struct {
	Key        string // data-key of the row: one per incident
	ID         string
	URL        string
	Name       string // devices.user_name; "" when unknown
	Initials   string
	Number     string // masked, "+58 414 ···67"
	State      string // a Case* state
	StateLabel string
	Line       string // what is happening, one line
	LineSince  string // ISO: a running timer follows Line from here; "" for none
	Time       string // the gesture's time of day, VET
	Day        string // "sáb 24 oct"
	Triggered  string // "sáb 24 oct, 15:22"
	SinceISO   string // the gesture's instant, for "hace 3 min"
	Place      string // the newest point, "10.49610, -66.85310"; "" without one
	PlaceNote  string // "±12 m · caminando · 4 km/h"
	PlaceISO   string // when the newest point reached the server, for "hace 8 s"
	MapsURL    string
	Family     string // "✓✓ 2 de 2", "✓ 2 enviados", "Avisada" or ""
	FamilyDone bool   // every message delivered or read
	Sealed     bool
	New        bool // reached the server less than newCase ago
	Live       bool // still followed live (activo or en llamada)
}

// newCase is how long a case keeps its "Nuevo" tag in the list.
const newCase = 2 * time.Minute

func caseRow(c CaseItem, now time.Time) CaseRow {
	state := caseState(c, now)
	r := CaseRow{
		Key:        "case-" + c.ID,
		ID:         c.ID,
		URL:        CentralCasePath(c.ID),
		Name:       strings.TrimSpace(c.UserName),
		Number:     MaskRecipient(c.Phone),
		State:      state,
		StateLabel: caseStateLabel(state),
		Time:       clock(c.TriggeredAt),
		Day:        dayLabel(c.TriggeredAt),
		Triggered:  shortDate(c.TriggeredAt),
		SinceISO:   iso(c.TriggeredAt),
		Sealed:     c.SealedAt != nil,
		New:        !now.IsZero() && now.Sub(c.CreatedAt) < newCase && now.Sub(c.CreatedAt) >= -time.Minute,
		Live:       state == CaseActive || state == CaseCalling,
	}
	r.Initials = initials(r.Name)
	r.Line, r.LineSince = caseLine(c, state)
	if p := c.Last; p != nil {
		r.Place = fmt.Sprintf("%.5f, %.5f", p.Lat, p.Lng)
		note := []string{}
		if a := accuracy(p.AccuracyM); a != "—" {
			note = append(note, a)
		}
		r.PlaceNote = strings.Join(append(note, movement(p.SpeedMPS)), " · ")
		r.PlaceISO = iso(p.ReceivedAt)
		r.MapsURL = MapsURL(p.Lat, p.Lng)
	}
	switch {
	case c.Deliveries > 0 && c.Delivered > 0:
		r.Family = fmt.Sprintf("✓✓ %d de %d", c.Delivered, c.Deliveries)
		r.FamilyDone = c.Delivered == c.Deliveries
	case c.Deliveries == 1:
		r.Family = "✓ 1 enviado"
	case c.Deliveries > 1:
		r.Family = fmt.Sprintf("✓ %d enviados", c.Deliveries)
	case c.NotifiedAt != nil:
		r.Family = "Avisada"
	}
	return r
}

// caseLine is the row's second line: what the case is at, in a few words,
// and when a running timer follows it, its start.
func caseLine(c CaseItem, state string) (line, since string) {
	switch state {
	case CaseCancelled:
		line = "Canceló la alerta"
		if c.Verdict == contract.VerdictNoDuress {
			line = "Dijo que está bien"
		}
		if c.NotifiedAt == nil {
			line += " · no se avisó a nadie"
		}
		return line, ""
	case CaseAlert:
		return verdictLine(c.Verdict, c.VerdictReason), ""
	case CaseCalling:
		cl := c.Call
		switch {
		case cl.PickedUpAt != nil:
			return "Hablando con Mamá", iso(*cl.PickedUpAt)
		case cl.RingingAt != nil:
			return "Sonando", iso(*cl.RingingAt)
		}
		return "Mamá está llamando", iso(cl.RequestedAt)
	}
	live := state == CaseActive
	switch {
	case c.Branch != contract.BranchCall:
		return branchLabel(c.Branch, c.SpeedMPS), ""
	case c.Verdict != "":
		return verdictLine(c.Verdict, c.VerdictReason), ""
	case c.Call != nil && c.Call.Status == "failed":
		return "La llamada no conectó", ""
	case c.Call == nil && live:
		return "Mamá va a llamar…", ""
	case c.Call == nil:
		return "No se registró la llamada", ""
	case live:
		return "Esperando veredicto…", ""
	}
	return "Sin veredicto", ""
}

// initials are up to two letters of a name, for the row's avatar.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		for _, r := range w {
			if unicode.IsLetter(r) {
				out = append(out, unicode.ToUpper(r))
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

// dayLabel is "sáb 24 oct" in VET.
func dayLabel(t time.Time) string {
	t = t.In(vet)
	return fmt.Sprintf("%s %d %s", weekdaysShort[t.Weekday()], t.Day(), monthsShort[t.Month()-1])
}

// CaseCount is one counter above the list.
type CaseCount struct {
	State string
	Label string
	N     int
}

func caseCounts(rows []CaseRow) []CaseCount {
	n := map[string]int{}
	for _, r := range rows {
		n[r.State]++
	}
	out := make([]CaseCount, 0, len(caseStates))
	for _, s := range caseStates {
		out = append(out, CaseCount{State: s, Label: caseCountLabel(s, n[s]), N: n[s]})
	}
	return out
}

// ContextView is the call's context card (#106's calls.ficha).
type ContextView struct {
	Situation  string
	Fields     []ContextField
	Confidence string // baja, media or alta; "" when not given
}

// ContextField is one line of the context card.
type ContextField struct {
	Key     string
	Label   string
	Value   string
	Unknown bool // the call did not say ("no se sabe")
}

// contextFields are the ficha's fields in the card's order.
var contextFields = []struct{ key, label string }{
	{"lugar", "Lugar"},
	{"personas", "Personas"},
	{"vehiculo", "Vehículo"},
	{"rumbo", "Rumbo"},
}

// contextView reads a calls.ficha value: a JSON object, or a string that
// holds one. nil when it is empty or not one.
func contextView(raw json.RawMessage) *ContextView {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return nil
	}
	vals := map[string]string{}
	for k, v := range m {
		vals[plainKey(k)] = valueText(v)
	}
	v := &ContextView{Situation: vals["situacion"], Confidence: strings.ToLower(vals["confianza"])}
	if unknownValue(v.Situation) {
		v.Situation = ""
	}
	for _, f := range contextFields {
		val := vals[f.key]
		v.Fields = append(v.Fields, ContextField{Key: f.key, Label: f.label, Value: val, Unknown: unknownValue(val)})
	}
	return v
}

// accents drops the Spanish accents from lowercase text.
var accents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u")

// plainKey lowercases a key and drops its accents: "Vehículo" is vehiculo.
func plainKey(k string) string { return accents.Replace(strings.ToLower(strings.TrimSpace(k))) }

func valueText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "sí"
		}
		return "no"
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := valueText(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func unknownValue(s string) bool {
	switch plainKey(s) {
	case "", "no se sabe", "desconocido", "n/a", "-", "—":
		return true
	}
	return false
}
