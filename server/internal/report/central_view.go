package report

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
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

// situationOf is the call's one-line summary in a calls.ficha value (#106:
// its "situacion"), a JSON object or a string holding one. "" when there is
// none, or when it says the call did not tell ("no se sabe"). The other
// ficha fields (lugar, personas, vehículo, rumbo) are not shown here.
func situationOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for k, v := range m {
		if plainKey(k) == "situacion" {
			if t := valueText(v); !unknownValue(t) {
				return t
			}
		}
	}
	return ""
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

// ---------------------------------------------------------------- the route's times

// minLabelGap is how far apart, in drawing units, two labelled points must
// be. A time label is about 52 units wide at the CSS size (11 units), and
// neighbours sit on opposite sides of the path.
const minLabelGap = 44.0

// centralRoute is the route in the central's box, with the time of day
// written next to the points: always the newest and the first, and the
// others only when they are at least minLabelGap from every labelled one,
// so a dense route stays readable. On a mostly horizontal stretch a label
// sits centered above or below its point, on a mostly vertical one to its
// right or left, alternating sides along the route, with more room around
// the newest (pulsing) point; on a sloped stretch it leans away from where
// the path climbs. A label that would leave the box or overlap another
// tries the other side, then is pulled back inside, and a middle one is
// left out if nothing fits.
func centralRoute(points []Point) *Route {
	r := routeIn(points, centralRouteW, centralRouteH, centralRoutePad)
	if r == nil {
		return nil
	}
	n := len(r.Dots)
	var picked []int
	far := func(i int) bool {
		for _, j := range picked {
			if math.Hypot(r.Dots[i].x-r.Dots[j].x, r.Dots[i].y-r.Dots[j].y) < minLabelGap {
				return false
			}
		}
		return true
	}
	picked = append(picked, n-1) // the newest first, so it always keeps its label
	if n > 1 && far(0) {
		picked = append(picked, 0)
	}
	for i := 1; i < n-1; i++ {
		if far(i) {
			picked = append(picked, i)
		}
	}
	// Sides alternate in route order; placing goes newest, first, the rest.
	order := append([]int(nil), picked...)
	sort.Ints(order)
	flip := map[int]bool{}
	for k, i := range order {
		flip[i] = k%2 == 1
	}
	type box struct{ x0, x1, y0, y1 float64 }
	var placed []box
	free := func(b box) bool {
		if b.x0 < 1 || b.x1 > centralRouteW-1 || b.y0 < 0 || b.y1 > centralRouteH {
			return false
		}
		for _, p := range placed {
			if b.x0 < p.x1 && p.x0 < b.x1 && b.y0 < p.y1 && p.y0 < b.y1 {
				return false
			}
		}
		return true
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	byIndex := map[int]RouteLabel{}
	for _, i := range picked {
		d := r.Dots[i]
		a, b := r.Dots[max(i-1, 0)], r.Dots[min(i+1, n-1)]
		gap := 9.0
		if i == n-1 {
			gap = 15 // clear of the pulse
		}
		ux, uy := b.x-a.x, b.y-a.y
		if l := math.Hypot(ux, uy); l > 0 {
			ux, uy = ux/l, uy/l
		}
		horizontal := math.Abs(ux) >= math.Abs(uy)
		// On a sloped stretch the path climbs to one side of the point
		// (rise: +1 right, -1 left, 0 flat); a label above goes to the other
		// side, one below to that side, so it does not sit on the line.
		rise := 0.0
		if math.Abs(uy) > 0.15 {
			rise = math.Copysign(1, -ux*uy)
		}
		// place gives the label on one side (other: the opposite one), its
		// anchor turned near the box's edges.
		place := func(other, clamp bool) (x, y float64, anchor string, bx box) {
			const w, size = labelW, labelSize
			if horizontal {
				below := flip[i] != other
				x, y, anchor = d.x, d.y-gap, "middle"
				if below {
					y = d.y + gap + size
				}
				switch {
				case rise > 0 && !below, rise < 0 && below:
					x, anchor = d.x+3, "end"
				case rise < 0 && !below, rise > 0 && below:
					x, anchor = d.x-3, "start"
				}
				switch {
				case !clamp:
				case anchor == "middle" && x-w/2 < 1, anchor == "end" && x-w < 1:
					x, anchor = math.Max(d.x-w/4, 1), "start"
				case anchor == "middle" && x+w/2 > centralRouteW-1, anchor == "start" && x+w > centralRouteW-1:
					x, anchor = math.Min(d.x+w/4, centralRouteW-1), "end"
				}
			} else {
				y, x, anchor = d.y+size*0.35, d.x+gap+2, "start"
				if flip[i] != other {
					x, anchor = d.x-gap-2, "end"
				}
			}
			switch anchor {
			case "start":
				bx = box{x, x + w, y - size, y + 2}
			case "end":
				bx = box{x - w, x, y - size, y + 2}
			default:
				bx = box{x - w/2, x + w/2, y - size, y + 2}
			}
			return x, y, anchor, bx
		}
		// Its own side, then the other one, and only then pulled back
		// inside at an edge (where it may cross the path).
		x, y, anchor, bx := place(false, false)
		found := free(bx)
		for _, c := range [][2]bool{{true, false}, {false, true}, {true, true}} {
			if found {
				break
			}
			if x2, y2, a2, b2 := place(c[0], c[1]); free(b2) {
				x, y, anchor, bx, found = x2, y2, a2, b2, true
			}
		}
		if !found {
			if i != n-1 && i != 0 {
				continue // a middle point gives way
			}
			x, y, anchor, bx = place(false, true)
		}
		placed = append(placed, bx)
		byIndex[i] = RouteLabel{
			Key: "lbl-" + strconv.Itoa(i+1) + "-" + f(x) + "-" + f(y) + "-" + anchor, X: f(x), Y: f(y),
			Anchor: anchor, Text: d.at, Newest: i == n-1,
		}
	}
	for _, i := range order {
		if l, ok := byIndex[i]; ok {
			r.Labels = append(r.Labels, l)
		}
	}
	return r
}

// A time label's size in drawing units (the CSS sets 11px on the SVG).
const labelW, labelSize = 54.0, 11.0

// ---------------------------------------------------------------- notified people

// NotifiedView is the "Personas notificadas" panel: a one-line summary,
// then the relatives, then the person who asked for help (#102 writes to
// them too), apart.
type NotifiedView struct {
	Summary string // "3 personas avisadas · 2 ya lo leyeron"
	Family  []NotifiedPerson
	Self    *NotifiedPerson
}

// NotifiedPerson is one number the notifier wrote to, with each channel's
// message.
type NotifiedPerson struct {
	Key       string
	Role      string // "Familiar 1", or "Quien pidió ayuda"
	Recipient string // masked
	Sends     []NotifiedSend
	best      string // the furthest status of its messages
}

// NotifiedSend is one message: its channel and how far it got, in words.
type NotifiedSend struct {
	Key     string
	Channel string // WhatsApp or SMS
	Status  string // sent, delivered, read or failed
	Label   string // "Enviado", "Entregado", "Leído", "No se pudo enviar"
	Ticks   string // "✓", "✓✓" or "!"
	Time    string // when it got there (server clock)
}

// sendRank orders the statuses: a person's best message decides how far the
// alert got to them.
var sendRank = map[string]int{"failed": 0, "sent": 1, "delivered": 2, "read": 3}

// notifiedView groups the family chips by number, relatives in first-sent
// order and the reporter's own number (self) last.
func notifiedView(chips []FamilyChip, self string) NotifiedView {
	var v NotifiedView
	at := map[string]*NotifiedPerson{}
	var order []string
	for _, c := range chips {
		p, ok := at[c.Recipient]
		if !ok {
			p = &NotifiedPerson{Key: "person-" + c.Recipient, Recipient: c.Recipient, best: "failed"}
			at[c.Recipient] = p
			order = append(order, c.Recipient)
		}
		s := NotifiedSend{Channel: c.Channel, Status: c.Status, Ticks: c.Ticks, Time: c.Time}
		switch c.Status {
		case "read":
			s.Label = "Leído"
		case "delivered":
			s.Label = "Entregado"
		case "failed":
			s.Label = "No se pudo enviar"
		default:
			s.Status, s.Label = "sent", "Enviado"
		}
		s.Key = "send-" + c.Channel + "-" + s.Status + "-" + c.Time
		p.Sends = append(p.Sends, s)
		if sendRank[s.Status] > sendRank[p.best] {
			p.best = s.Status
		}
	}
	family := 0
	for _, r := range order {
		p := at[r]
		if r == self {
			p.Role = "Quien pidió ayuda"
			v.Self = p
			continue
		}
		family++
		p.Role = "Familiar " + strconv.Itoa(family)
		v.Family = append(v.Family, *p)
	}
	v.Summary = notifiedSummary(append(append([]NotifiedPerson(nil), v.Family...), selfList(v.Self)...))
	return v
}

func selfList(p *NotifiedPerson) []NotifiedPerson {
	if p == nil {
		return nil
	}
	return []NotifiedPerson{*p}
}

// notifiedSummary is "3 personas avisadas · 2 ya lo leyeron", or how far it
// got: received, waiting, or who could not be reached.
func notifiedSummary(people []NotifiedPerson) string {
	var reached, read, delivered, failed int
	for _, p := range people {
		switch p.best {
		case "failed":
			failed++
			continue
		case "read":
			read++
		case "delivered":
			delivered++
		}
		reached++
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	}
	var parts []string
	if reached > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", reached, plural(reached, "persona avisada", "personas avisadas")))
		switch {
		case read > 0:
			parts = append(parts, fmt.Sprintf("%d %s", read, plural(read, "ya lo leyó", "ya lo leyeron")))
		case delivered > 0:
			parts = append(parts, fmt.Sprintf("%d %s", delivered, plural(delivered, "ya lo recibió", "ya lo recibieron")))
		default:
			parts = append(parts, "esperando que llegue")
		}
	}
	switch {
	case failed > 0 && reached == 0:
		parts = append(parts, "No se pudo avisar a nadie")
	case failed > 0:
		parts = append(parts, fmt.Sprintf("a %d no se %s", failed, plural(failed, "le pudo avisar", "les pudo avisar")))
	}
	return strings.Join(parts, " · ")
}
