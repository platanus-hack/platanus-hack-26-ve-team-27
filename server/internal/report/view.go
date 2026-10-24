package report

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// vet is Venezuela time (UTC-4, no DST), the zone every time on the page is
// shown in. A fixed zone needs no tzdata on the server image.
var vet = time.FixedZone("VET", -4*60*60)

// Clock says which clock a time on the page comes from: the schema keeps the
// phone's, the server's and ElevenLabs' apart, and they need not agree.
type Clock string

const (
	ClockPhone  Clock = "phone"
	ClockServer Clock = "server"
	ClockCall   Clock = "elevenlabs" // the call's start and end, from the post-call webhook
)

// Label is the clock's name on the page.
func (c Clock) Label() string {
	switch c {
	case ClockPhone:
		return "teléfono"
	case ClockServer:
		return "servidor"
	case ClockCall:
		return "ElevenLabs"
	}
	return string(c)
}

// EventKind is one row kind of the timeline, numbered as in the #76 table.
type EventKind string

const (
	EventTrigger       EventKind = "trigger"        // 1: asked for help
	EventFirstPoint    EventKind = "first_point"    // 2: first location
	EventReceived      EventKind = "received"       // 3: reached the server
	EventBranch        EventKind = "branch"         // 4: branch picked
	EventCallRequested EventKind = "call_requested" // 5: "Mamá" called
	EventAnswered      EventKind = "answered"       // 6: answered
	EventPhrase        EventKind = "phrase"         // 7: the turn that says the phrase (every turn is in Page.Calls)
	EventHungUp        EventKind = "hung_up"        // 8: hung up, or the call failed
	EventVerdict       EventKind = "verdict"        // 9: verdict
	EventNotified      EventKind = "notified"       // 10: family message sent
	EventPoint         EventKind = "point"          // 11: later locations
	EventRecording     EventKind = "recording"      // 12: recording stored
	EventSealed        EventKind = "sealed"         // 13: report sealed

	// Beyond the #76 table: what else the database knows.
	EventRinging   EventKind = "ringing"   // Twilio said the phone rang (call_progress)
	EventFallback  EventKind = "fallback"  // every call failed: the WebRTC fallback opened (derived)
	EventEmergency EventKind = "emergency" // an offline emergency of the same device and time
)

// Event is one timeline row.
type Event struct {
	Key    string // stable across updates, for the live page
	Kind   EventKind
	At     time.Time
	Clock  Clock
	Title  string
	Detail string
	Quote  string  // EventPhrase: what was said
	Call   int     // 1-based call number for call events; 0 otherwise
	Seek   float64 // EventPhrase: the second in the call's recording
	Error  bool    // a failure (the only place the page uses red)
}

// Time is the event's time of day in VET.
func (e Event) Time() string { return e.At.In(vet).Format("15:04:05") }

// ISO is the event's instant for the <time> element.
func (e Event) ISO() string { return e.At.UTC().Format(time.RFC3339Nano) }

// Page is everything the template renders.
type Page struct {
	Token       string
	State       string // "open", "sealed" or "unsealed"
	StateLabel  string
	Character   string // the #72 pose: "escuchando" while open, "lista" once sealed
	Verdict     string // headline: the incident verdict
	VerdictKind string // a contract.Verdict, or "none"
	Triggered   string // long date of the trigger, VET
	Branch      string
	Timeline    []Event
	Points      []PointRow
	Route       *Route
	Calls       []CallView
	NoCall      string // why there was no call, for the call section
	MultiCall   bool
	Seal        SealView

	HasCallBranch bool   // the branch calls: ElevenLabs' clock can appear
	Live          bool   // not sealed yet: the page follows the case live
	TriggerISO    string // the gesture's instant, for the running clock
	CallActive    bool   // a call is being placed, ringing or talking
	CallPhase     string // "Marcando", "Sonando" or "En llamada"
	CallSince     string // when CallPhase started (ISO), for the call timer
	Latencies     []Latency
	Emergencies   []EmergencyView
	Fallback      bool // every call failed and the WebRTC fallback opened

	// The redesigned page (one screen on wide displays).
	Sealed         bool
	UserName       string // devices.user_name when it exists (#104)
	Title          string // header title: the verdict, or what the case waits for
	TitleKind      string // a contract.Verdict, "waiting" or "none"
	TriggeredShort string // "sáb 24 oct, 15:22"
	Elapsed        string // a closed case's length, "1:05"; "" while live
	Stages         []Stage
	RouteLast      string    // the newest point's arrival (ISO, server clock), for "hace 12 s"
	RouteLastClock string    // the newest point's time of day, once the case is closed
	RouteMaps      string    // "Abrir en Maps" for the newest point
	Call           *CallView // the finished call the call panel shows; nil while none
	CallNote       string    // the call panel's line when there is no card
	Family         []FamilyChip
	FamilyNote     string
	PDFURL         string
}

// PointRow is one row of the locations table.
type PointRow struct {
	Key      string
	N        int
	At       string // phone clock, VET
	Accuracy string
	Speed    string
	LatLng   string
	MapsURL  string
}

// CallView is one call in the call section.
type CallView struct {
	Key       string
	Retry     bool   // not the first call: a retry
	Active    bool   // being placed, ringing or talking
	Phase     string // "Marcando", "Sonando", "En llamada" while active
	Since     string // ISO start of Phase, for the live call timer
	Ringing   string // when Twilio said it rang (server clock), "" if unknown
	PickedUp  string // when Twilio said it was answered (server clock)
	N         int
	Number    string // the dialed number, masked
	Requested string
	Duration  string
	Outcome   string
	Failed    bool
	Verdict   string // "" until judged
	Turns     []TurnView
	Audio     *AudioView // nil: "Audio no disponible"
	NoAudio   string     // why there is no audio
	More      bool       // the collapsed transcript hides turns
}

// TurnView is one transcript turn.
type TurnView struct {
	Agent   bool
	Speaker string // "Mamá" or "Tu familiar"
	Message string
	Seek    float64 // seconds into the call
	Offset  string  // "0:04"
	At      string  // time of day on ElevenLabs' clock; "" when the start is unknown
	Phrase  bool    // the turn that says the duress phrase
	Keep    bool    // shown while the transcript is collapsed on a phone
}

// AudioView is a stored recording.
type AudioView struct {
	URL    string
	SHA256 string
	Size   string
	Stored string
}

// SealView is the seal section.
type SealView struct {
	Sealed      bool
	SealedAt    string
	DocSHA256   string
	Key         string // std Base64, as GET /config serves it
	Fingerprint string // SHA256 of the raw key, hex in groups
	JSONURL     string
	SigURL      string
	Recordings  []SealRecording
	Why         string // why it is not sealed
}

// SealRecording is one recording hash in the seal section.
type SealRecording struct {
	Call   int
	SHA256 string
}

// Options are the page settings that do not come from the record.
type Options struct {
	Token   string
	CanSeal bool   // the server has a signing key
	Channel string // the family message channel: "WhatsApp" or "SMS"
	BaseURL string // the server's public URL, for the PDF's links; blank gives paths
	// Now is the server time of the render; zero skips the checks that need
	// it (a call left "in progress" long ago is not shown as live).
	Now time.Time
}

// staleCall is how long a call may stay requested or in progress before the
// page stops calling it live: its webhook never came.
const staleCall = 15 * time.Minute

// liveWindow is how long after the last thing stored about an unsealed case
// the page still follows it live (clocks running, "esperando…").
const liveWindow = 30 * time.Minute

// Build turns a record into the page. It is pure: same record, same page.
func Build(rec Record, opts Options) Page {
	p := Page{
		Token:     opts.Token,
		Triggered: longDate(rec.Incident.TriggeredAt),
		Branch:    branchLabel(rec.Incident.Branch, rec.Incident.SpeedMPS),
		Timeline:  Timeline(rec, opts),
		Points:    pointRows(rec.Points),
		Route:     route(rec.Points),
		MultiCall: len(rec.Calls) > 1,
	}
	p.Verdict, p.VerdictKind = headline(rec)
	switch {
	case rec.Seal != nil:
		p.State, p.StateLabel, p.Character = "sealed", "Cerrado y sellado", "lista"
	case rec.Incident.Branch != contract.BranchCall || !opts.CanSeal:
		p.State, p.StateLabel, p.Character = "unsealed", "Sin sellar", "escuchando"
	default:
		p.State, p.StateLabel, p.Character = "open", "Abierto", "escuchando"
	}
	_, last := rec.span()
	p.Live = rec.Seal == nil && (opts.Now.IsZero() || opts.Now.Sub(last) < liveWindow)
	p.TriggerISO = iso(rec.Incident.TriggeredAt)
	p.HasCallBranch = rec.Incident.Branch == contract.BranchCall
	for i, c := range rec.Calls {
		v := callView(i+1, c, rec.Device, opts.Token)
		if v.Active && (opts.Now.IsZero() || opts.Now.Sub(c.RequestedAt) < staleCall) && rec.Seal == nil {
			p.CallActive, p.CallPhase, p.CallSince = true, v.Phase, v.Since
		} else {
			v.Active = false
		}
		p.Calls = append(p.Calls, v)
	}
	if p.CallActive {
		p.Character = "llamando"
	}
	if len(rec.Calls) == 0 {
		p.NoCall = noCallReason(rec.Incident.Branch)
	}
	p.Fallback = fellBack(rec)
	p.Latencies = Latencies(rec, p.Live, p.CallActive)
	p.Emergencies = emergencyViews(rec.Emergencies)
	p.Seal = sealView(rec, opts)

	p.Sealed = rec.Seal != nil
	p.UserName = rec.Device.UserName
	p.Title, p.TitleKind = title(rec, p.Live, p.CallActive)
	p.TriggeredShort = shortDate(rec.Incident.TriggeredAt)
	if !p.Live {
		end := last
		if rec.Seal != nil {
			end = rec.Seal.SealedAt
		}
		p.Elapsed = elapsedText(end.Sub(rec.Incident.TriggeredAt))
	}
	p.Stages = Stages(rec, opts.Channel, p.Live)
	if n := len(rec.Points); n > 0 {
		pt := rec.Points[n-1]
		p.RouteLast, p.RouteLastClock, p.RouteMaps = iso(pt.ReceivedAt), clock(pt.RecordedAt), MapsURL(pt.Lat, pt.Lng)
	}
	if !p.CallActive {
		for i := len(p.Calls) - 1; i >= 0; i-- {
			if c := p.Calls[i]; len(c.Turns) > 0 || c.Audio != nil {
				p.Call = &p.Calls[i]
				break
			}
		}
	}
	switch {
	case p.Call != nil || p.CallActive:
	case rec.Incident.Branch != contract.BranchCall:
		p.CallNote = noCallReason(rec.Incident.Branch)
	case len(rec.Calls) > 0 && p.Fallback:
		p.CallNote = "Ninguna llamada conectó: MAMÁ abrió la página de respaldo para hablar por el navegador."
	case len(rec.Calls) > 0:
		p.CallNote = "La llamada no conectó."
	case p.Live:
		p.CallNote = "Mamá va a llamar en un momento."
	default:
		p.CallNote = "No se registró la llamada."
	}
	p.Family = familyChips(rec.Deliveries)
	if len(p.Family) == 0 {
		p.FamilyNote = familyNote(rec, opts.Channel, p.Live)
	}
	p.PDFURL = PDFPath(opts.Token)
	return p
}

// fellBack reports whether the engine opened the WebRTC fallback: no call
// connected and the last one failed, after the retry (two failed calls) or
// at once for a number it does not dial (not +58). The engine keeps the
// fallback in memory, so this reads it back from the calls rows.
func fellBack(rec Record) bool {
	if len(rec.Calls) == 0 || rec.Calls[len(rec.Calls)-1].Status != "failed" {
		return false
	}
	for _, c := range rec.Calls {
		if c.Status == "done" {
			return false
		}
	}
	return len(rec.Calls) >= 2 || !strings.HasPrefix(rec.Device.PhoneNumber, "+58")
}

// Timeline builds the 13-event timeline (#76) of a record, ordered by time.
// Events at the same instant keep the table's order. Every turn of a call is
// in Page.Calls; the timeline carries the one that says the phrase.
func Timeline(rec Record, opts Options) []Event {
	inc := rec.Incident
	var ev []Event
	add := func(e Event) { ev = append(ev, e) }

	add(Event{Key: "trigger", Kind: EventTrigger, At: inc.TriggeredAt, Clock: ClockPhone, Title: "Pidió ayuda", Detail: "Pidió ayuda " + triggerLabel(inc.Trigger)})
	for i, pt := range rec.Points {
		e := Event{Key: "point-" + strconv.Itoa(i+1), Kind: EventPoint, At: pt.RecordedAt, Clock: ClockPhone, Title: "Ubicación", Detail: pointSummary(pt)}
		if i == 0 {
			e.Kind, e.Title = EventFirstPoint, "Primera ubicación"
		}
		add(e)
	}
	add(Event{Key: "received", Kind: EventReceived, At: inc.CreatedAt, Clock: ClockServer, Title: "Llegó al servidor", Detail: receivedLabel(inc.CreatedAt.Sub(inc.TriggeredAt))})
	add(Event{Key: "branch", Kind: EventBranch, At: inc.CreatedAt, Clock: ClockServer, Title: "Rama elegida", Detail: branchLabel(inc.Branch, inc.SpeedMPS)})

	for i, c := range rec.Calls {
		n := i + 1
		key := func(k string) string { return "call-" + strconv.Itoa(n) + "-" + k }
		req := Event{Key: key("requested"), Kind: EventCallRequested, At: c.RequestedAt, Clock: ClockServer, Call: n, Title: "Mamá llamó", Detail: "Llamando a " + maskPhone(rec.Device.PhoneNumber)}
		if n > 1 {
			req.Title, req.Detail = "Mamá volvió a llamar", "Reintento · llamando a "+maskPhone(rec.Device.PhoneNumber)
		}
		add(req)
		if c.RingingAt != nil {
			add(Event{Key: key("ringing"), Kind: EventRinging, At: *c.RingingAt, Clock: ClockServer, Call: n, Title: "Sonó su teléfono", Detail: "Según el estado de la llamada en Twilio"})
		}
		answered, ok := c.AnsweredAt()
		if ok {
			add(Event{Key: key("answered"), Kind: EventAnswered, At: answered, Clock: ClockCall, Call: n, Title: "Contestó", Detail: turnsLabel(len(c.Transcript))})
			phrase := phraseTurns(c.Transcript, rec.Device.DuressPhrase)
			for j, t := range c.Transcript {
				if phrase[j] {
					add(Event{Key: key("phrase-" + strconv.Itoa(j)), Kind: EventPhrase, At: answered.Add(secs(t.TimeInCallSecs)), Clock: ClockCall, Call: n,
						Title: "Dijo la frase acordada", Quote: t.Message, Seek: t.TimeInCallSecs})
				}
			}
		} else if c.PickedUpAt != nil {
			add(Event{Key: key("answered"), Kind: EventAnswered, At: *c.PickedUpAt, Clock: ClockServer, Call: n, Title: "Contestó", Detail: "Según el estado de la llamada en Twilio"})
		}
		if c.EndedAt != nil {
			e := Event{Key: key("ended"), Kind: EventHungUp, At: *c.EndedAt, Clock: ClockServer, Call: n}
			if c.DurationSecs != nil {
				e.Clock = ClockCall
			}
			if c.Status == "failed" {
				e.Title, e.Detail, e.Error = "La llamada falló", "No se pudo hablar con ella", true
			} else {
				e.Title, e.Detail = "Colgó", "Llamada de "+duration(time.Duration(derefInt(c.DurationSecs))*time.Second)
			}
			add(e)
		}
		if c.Verdict != "" && c.VerdictAt != nil {
			add(Event{Key: key("verdict"), Kind: EventVerdict, At: *c.VerdictAt, Clock: ClockServer, Call: n, Title: "Veredicto", Detail: verdictLine(c.Verdict, c.VerdictReason)})
		}
		if r := c.Recording; r != nil {
			add(Event{Key: key("recording"), Kind: EventRecording, At: r.StoredAt, Clock: ClockServer, Call: n, Title: "Grabación guardada", Detail: "SHA-256 " + shortHex(r.SHA256[:])})
		}
	}
	if fellBack(rec) {
		last := rec.Calls[len(rec.Calls)-1]
		at := last.RequestedAt
		if last.EndedAt != nil {
			at = *last.EndedAt
		}
		add(Event{Key: "fallback", Kind: EventFallback, At: at, Clock: ClockServer, Error: true,
			Title: "Se abrió el respaldo por el navegador", Detail: "Ninguna llamada conectó: MAMÁ abrió la página de respaldo (WebRTC). Deducido del registro de llamadas."})
	}
	for _, em := range rec.Emergencies {
		add(Event{Key: "emergency-" + em.ID, Kind: EventEmergency, At: em.FirstReceivedAt, Clock: ClockServer,
			Title: "Emergencia sin conexión", Detail: "Llegó por " + channelsLabel(em.Channels) + hopsLabel(em.MinHops) + " · mismo teléfono, misma hora"})
		if em.CancelledAt != nil {
			add(Event{Key: "emergency-" + em.ID + "-cancelled", Kind: EventEmergency, At: *em.CancelledAt, Clock: ClockServer,
				Title: "Emergencia sin conexión cancelada", Detail: "La persona la canceló desde el teléfono"})
		}
	}
	if inc.NotifiedAt != nil {
		add(Event{Key: "notified", Kind: EventNotified, At: *inc.NotifiedAt, Clock: ClockServer, Title: "Aviso a la familia", Detail: notifiedLabel(opts.Channel, rec.Device.FamilyNumbers)})
	}
	if s := rec.Seal; s != nil {
		add(Event{Key: "sealed", Kind: EventSealed, At: s.SealedAt, Clock: ClockServer, Title: "Ficha sellada", Detail: "Firmada con la llave Ed25519 del servidor"})
	}

	sort.SliceStable(ev, func(i, j int) bool { return ev[i].At.Before(ev[j].At) })
	return ev
}

// headline is the incident verdict: duress if any call says duress, else
// the newest judged call's (the notifier's rule, docs/contract.md).
func headline(rec Record) (label, kind string) {
	var newest contract.Verdict
	for _, c := range rec.Calls {
		if c.Verdict == contract.VerdictDuress {
			return verdictLabel(c.Verdict), string(c.Verdict)
		}
		if c.Verdict != "" {
			newest = c.Verdict
		}
	}
	if newest != "" {
		return verdictLabel(newest), string(newest)
	}
	switch rec.Incident.Branch {
	case contract.BranchImmediateAlert:
		return "Alerta sin llamada", "none"
	case contract.BranchSilentTracker:
		return "Rastreo silencioso", "none"
	}
	if len(rec.Calls) == 0 {
		return "Llamada pendiente", "none"
	}
	return "Sin veredicto todavía", "none"
}

func callView(n int, c Call, dev Device, token string) CallView {
	v := CallView{
		Key:       "call-" + strconv.Itoa(n),
		N:         n,
		Retry:     n > 1,
		Number:    maskPhone(dev.PhoneNumber),
		Requested: clock(c.RequestedAt),
		Failed:    c.Status == "failed",
	}
	if c.RingingAt != nil {
		v.Ringing = clock(*c.RingingAt)
	}
	if c.PickedUpAt != nil {
		v.PickedUp = clock(*c.PickedUpAt)
	}
	switch c.Status {
	case "done":
		v.Outcome = "Terminó"
		if c.DurationSecs != nil {
			v.Duration = duration(time.Duration(*c.DurationSecs) * time.Second)
		}
	case "failed":
		v.Outcome = "La llamada falló"
	default:
		v.Outcome = "En curso"
		v.Active = true
		v.Phase, v.Since = "Marcando", iso(c.RequestedAt)
		if c.RingingAt != nil {
			v.Phase, v.Since = "Sonando", iso(*c.RingingAt)
		}
		if c.PickedUpAt != nil {
			v.Phase, v.Since = "En llamada", iso(*c.PickedUpAt)
		}
	}
	if c.Verdict != "" {
		v.Verdict = verdictLine(c.Verdict, c.VerdictReason)
	}
	answered, known := c.AnsweredAt()
	phrase := phraseTurns(c.Transcript, dev.DuressPhrase)
	for j, t := range c.Transcript {
		tv := TurnView{
			Agent:   t.Role == duress.RoleAgent,
			Speaker: "Tu familiar",
			Message: t.Message,
			Seek:    t.TimeInCallSecs,
			Offset:  offset(t.TimeInCallSecs),
			Phrase:  phrase[j],
		}
		if tv.Agent {
			tv.Speaker = "Mamá"
		}
		if known {
			tv.At = clock(answered.Add(secs(t.TimeInCallSecs)))
		}
		v.Turns = append(v.Turns, tv)
	}
	// Collapsed on a phone, the transcript keeps the phrase turn, or the
	// first two turns when none says it.
	kept := 0
	for i := range v.Turns {
		if v.Turns[i].Phrase {
			v.Turns[i].Keep = true
			kept++
		}
	}
	if kept == 0 {
		for i := range v.Turns {
			if i < 2 {
				v.Turns[i].Keep = true
				kept++
			}
		}
	}
	v.More = kept < len(v.Turns)
	if r := c.Recording; r != nil {
		v.Audio = &AudioView{
			URL:    AudioPath(token, n),
			SHA256: hex.EncodeToString(r.SHA256[:]),
			Size:   size(r.Size),
			Stored: clock(r.StoredAt),
		}
	} else {
		v.NoAudio = noAudioReason(c)
	}
	return v
}

func noAudioReason(c Call) string {
	switch c.Status {
	case "failed":
		return "Audio no disponible: la llamada no se conectó."
	case "done":
		return "Audio no disponible: ElevenLabs todavía no entregó la grabación, o no la guardó. La transcripción sí está."
	}
	return "La grabación aparece aquí cuando la llamada termine."
}

func noCallReason(b contract.Branch) string {
	switch b {
	case contract.BranchImmediateAlert:
		return "No hubo llamada: estaba quieta, así que MAMÁ avisó a la familia de una vez."
	case contract.BranchSilentTracker:
		return "No hubo llamada: iba rápido (en carro), así que MAMÁ rastrea en silencio."
	}
	return "Todavía no hay llamada registrada."
}

func sealView(rec Record, opts Options) SealView {
	if s := rec.Seal; s != nil {
		doc := sha256.Sum256(s.Document)
		v := SealView{
			Sealed:      true,
			SealedAt:    longClock(s.SealedAt),
			DocSHA256:   hex.EncodeToString(doc[:]),
			Key:         base64.StdEncoding.EncodeToString(s.PublicKey),
			Fingerprint: Fingerprint(s.PublicKey),
			JSONURL:     DocumentPath(opts.Token),
			SigURL:      SignaturePath(opts.Token),
		}
		// The hashes the seal covers come from the sealed bytes, not from
		// the rows, which may have changed since.
		var d document
		if json.Unmarshal(s.Document, &d) == nil {
			for i, c := range d.Calls {
				if c.Recording != nil {
					v.Recordings = append(v.Recordings, SealRecording{Call: i + 1, SHA256: c.Recording.SHA256})
				}
			}
		}
		return v
	}
	switch {
	case rec.Incident.Branch != contract.BranchCall:
		return SealView{Why: "Solo se sellan las fichas con llamada. Esta alerta no tuvo llamada, así que la ficha queda sin sellar."}
	case !opts.CanSeal:
		return SealView{Why: "Este servidor no tiene configurada su llave para sellar, así que la ficha queda sin sellar."}
	}
	return SealView{Why: "La ficha se sella cuando termina la llamada y se guarda la grabación."}
}

// Fingerprint is the SHA-256 of the raw public key, as hex in groups of 4.
func Fingerprint(pub []byte) string {
	sum := sha256.Sum256(pub)
	h := hex.EncodeToString(sum[:])
	var b strings.Builder
	for i := 0; i < len(h); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(h[i : i+4])
	}
	return b.String()
}

// phraseTurns marks the user turns that say the duress phrase on their own,
// by asking the rules detector (#19) about each turn alone, so the page and
// the verdict match the same way. A phrase split across turns marks none. A
// turn with no spoken word (only [tags]) is skipped: the rules call silence
// duress, but there is nothing to highlight.
func phraseTurns(transcript []contract.TranscriptTurn, phrase string) map[int]bool {
	marked := map[int]bool{}
	if strings.TrimSpace(phrase) == "" {
		return marked
	}
	for i, t := range transcript {
		if t.Role != duress.RoleUser || !spoken(t.Message) {
			continue
		}
		r, err := duress.Rules{}.Detect(context.Background(), []contract.TranscriptTurn{t}, phrase)
		if err == nil && r.Verdict == contract.VerdictDuress {
			marked[i] = true
		}
	}
	return marked
}

// spoken reports whether s has a letter or digit outside [bracketed tags].
func spoken(s string) bool {
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case depth == 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			return true
		}
	}
	return false
}

func pointRows(points []Point) []PointRow {
	rows := make([]PointRow, 0, len(points))
	for i, p := range points {
		rows = append(rows, PointRow{
			Key:      "pt-" + strconv.Itoa(i+1),
			N:        i + 1,
			At:       clock(p.RecordedAt),
			Accuracy: accuracy(p.AccuracyM),
			Speed:    movement(p.SpeedMPS),
			LatLng:   fmt.Sprintf("%.5f, %.5f", p.Lat, p.Lng),
			MapsURL:  MapsURL(p.Lat, p.Lng),
		})
	}
	return rows
}

// MapsURL opens the point in Google Maps. It is a link the reader chooses
// to follow; the page itself never loads a map.
func MapsURL(lat, lng float64) string {
	return fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%.6f,%.6f", lat, lng)
}

// Route is the path drawn in SVG from the points, without map tiles: one
// segment per pair of points and one dot per point, each keyed so the live
// page animates only what is new.
type Route struct {
	Segments   []RouteSegment
	Dots       []RouteDot
	Width      int
	Height     int
	DistanceKM string
}

// RouteSegment is the line between two consecutive points.
type RouteSegment struct {
	Key            string
	X1, Y1, X2, Y2 string
}

// RouteDot is one point on the drawing; Newest pulses.
type RouteDot struct {
	Key     string
	X, Y    string
	Title   string
	MapsURL string
	First   bool
	Newest  bool
}

// minRouteSpanM is the smallest span the route box shows, so GPS jitter of a
// few meters is drawn as the small thing it is, not across the whole box.
const minRouteSpanM = 200.0

// route projects the points onto a 320x180 box (equirectangular meters,
// fine at a city's scale), at least minRouteSpanM across. nil without
// points.
func route(points []Point) *Route {
	if len(points) == 0 {
		return nil
	}
	const w, h, pad = 320.0, 180.0, 16.0
	const mPerDegLat = 110540.0
	meanLat := 0.0
	for _, p := range points {
		meanLat += p.Lat
	}
	meanLat /= float64(len(points))
	mPerDegLng := 111320.0 * math.Cos(meanLat*math.Pi/180)
	minX, maxX, minY, maxY := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	xy := make([][2]float64, len(points))
	for i, p := range points {
		x, y := p.Lng*mPerDegLng, -p.Lat*mPerDegLat
		xy[i] = [2]float64{x, y}
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	scale := math.Min((w-2*pad)/math.Max(maxX-minX, minRouteSpanM), (h-2*pad)/math.Max(maxY-minY, minRouteSpanM))
	offX := (w - (maxX-minX)*scale) / 2
	offY := (h - (maxY-minY)*scale) / 2
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	r := &Route{Width: int(w), Height: int(h), DistanceKM: distance(points)}
	var px, py string
	for i, c := range xy {
		x, y := f(offX+(c[0]-minX)*scale), f(offY+(c[1]-minY)*scale)
		n := strconv.Itoa(i + 1)
		if i > 0 {
			// The key carries the coordinates: when a new point rescales the
			// drawing, every segment is new and draws itself again.
			r.Segments = append(r.Segments, RouteSegment{Key: "seg-" + n + "-" + x + "-" + y + "-" + px + "-" + py, X1: px, Y1: py, X2: x, Y2: y})
		}
		r.Dots = append(r.Dots, RouteDot{
			Key: "dot-" + n + "-" + x + "-" + y, X: x, Y: y, MapsURL: MapsURL(points[i].Lat, points[i].Lng),
			Title: clock(points[i].RecordedAt) + " · " + accuracy(points[i].AccuracyM) + " · " + movement(points[i].SpeedMPS),
			First: i == 0, Newest: i == len(xy)-1,
		})
		px, py = x, y
	}
	return r
}

// distance is the path length (haversine): "15 m" under a kilometer, else
// "1,4 km".
func distance(points []Point) string {
	const earth = 6371.0
	total := 0.0
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
		dLat, dLng := la2-la1, (b.Lng-a.Lng)*math.Pi/180
		h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLng/2)*math.Sin(dLng/2)
		total += 2 * earth * math.Asin(math.Min(1, math.Sqrt(h)))
	}
	if total < 1 {
		return fmt.Sprintf("%.0f m", total*1000)
	}
	return decimal(total, 1) + " km"
}

// ---------------------------------------------------------------- Spanish copy

func triggerLabel(t contract.Trigger) string {
	switch t {
	case contract.TriggerVolumePattern:
		return "con el gesto de volumen"
	case contract.TriggerWidget:
		return "con el widget"
	case contract.TriggerTile:
		return "con los ajustes rápidos"
	case contract.TriggerInApp:
		return "desde la app"
	}
	return "(" + string(t) + ")"
}

func branchLabel(b contract.Branch, speed *float64) string {
	switch b {
	case contract.BranchImmediateAlert:
		return "Quieta: aviso inmediato"
	case contract.BranchSilentTracker:
		if speed != nil {
			return "En carro (" + kmh(*speed) + "): rastreo silencioso"
		}
		return "En carro: rastreo silencioso"
	case contract.BranchCall:
		if speed == nil || *speed < 0 {
			return "Sin velocidad: Mamá llama"
		}
		return "Caminando (" + kmh(*speed) + "): Mamá llama"
	}
	return string(b)
}

// movement describes a speed with the branch thresholds.
func movement(speed *float64) string {
	switch {
	case speed == nil || !(*speed >= 0):
		return "sin velocidad"
	case *speed < contract.StillBelowMPS:
		return "quieta"
	case *speed >= contract.FastFromMPS:
		return "en carro · " + kmh(*speed)
	}
	return "caminando · " + kmh(*speed)
}

func pointSummary(p Point) string {
	s := "Ubicación"
	if a := accuracy(p.AccuracyM); a != "—" {
		s += " " + a
	}
	return s + " · " + movement(p.SpeedMPS)
}

func accuracy(m *float64) string {
	if m == nil || !(*m >= 0) || math.IsInf(*m, 0) {
		return "—"
	}
	return fmt.Sprintf("±%.0f m", *m)
}

func kmh(mps float64) string { return fmt.Sprintf("%.0f km/h", mps*3.6) }

func receivedLabel(d time.Duration) string {
	if d < 0 {
		return "Recibido; el reloj del teléfono va " + duration(-d) + " adelantado"
	}
	return "Recibido " + duration(d) + " después"
}

func turnsLabel(n int) string {
	if n == 1 {
		return "1 turno de conversación"
	}
	return strconv.Itoa(n) + " turnos de conversación"
}

func notifiedLabel(channel string, family int) string {
	if channel == "" {
		channel = "WhatsApp"
	}
	who := strconv.Itoa(family) + " familiares"
	if family == 1 {
		who = "1 familiar"
	}
	return channel + " enviado a " + who
}

func verdictLabel(v contract.Verdict) string {
	switch v {
	case contract.VerdictDuress:
		return "Coacción"
	case contract.VerdictNoDuress:
		return "Sin coacción"
	case contract.VerdictUnclear:
		return "No está claro"
	}
	return string(v)
}

// verdictLine is "Coacción: dijo la frase acordada". The detector writes
// its reason in English; reasonES translates the ones it knows.
func verdictLine(v contract.Verdict, reason string) string {
	if es := reasonES(reason); es != "" {
		return verdictLabel(v) + ": " + es
	}
	return verdictLabel(v)
}

// reasonES translates the duress detector's reasons (internal/duress). A
// test runs the rules detector and checks each of its reasons is here.
func reasonES(reason string) string {
	r := reason
	llm := false
	if rest, ok := strings.CutPrefix(r, "rules: "); ok {
		r, _, _ = strings.Cut(rest, "; LLM: ")
		llm = true
	}
	if base, _, ok := strings.Cut(r, " (LLM unavailable"); ok {
		r = base
	}
	var es string
	switch {
	case r == "the user said the duress phrase":
		es = "dijo la frase acordada"
	case strings.HasPrefix(r, "the user said a close variant of the duress phrase"):
		es = "dijo una variante cercana de la frase acordada"
	case r == "the user stayed silent the whole call":
		es = "no habló en toda la llamada"
	case r == "the user did not say the duress phrase":
		es = "no dijo la frase acordada"
	case strings.HasPrefix(r, "the user said the duress phrase's words"):
		es = "dijo palabras de la frase, pero negadas o en otro orden"
	case strings.HasPrefix(r, "the user said part of the duress phrase"):
		es = "dijo parte de la frase acordada"
	case r == "the transcript is empty":
		es = "la transcripción está vacía"
	case r == "no duress phrase is configured":
		es = "no hay frase acordada configurada"
	case strings.HasPrefix(r, "the detector failed"):
		es = "el detector falló"
	default:
		return ""
	}
	if llm {
		es += " (revisado con IA)"
	}
	return es
}

// maskPhone keeps the country code of a +58 number and the last 2 digits:
// "+58 ···34".
func maskPhone(phone string) string {
	if len(phone) < 4 {
		return "···"
	}
	last := phone[len(phone)-2:]
	if strings.HasPrefix(phone, "+58") {
		return "+58 ···" + last
	}
	return "···" + last
}

// ---------------------------------------------------------------- formatting

var (
	weekdays = [...]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	months   = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
)

// longDate is "sábado 24 de octubre de 2026, 09:10 VET".
func longDate(t time.Time) string {
	t = t.In(vet)
	return fmt.Sprintf("%s %d de %s de %d, %s VET", weekdays[t.Weekday()], t.Day(), months[t.Month()-1], t.Year(), t.Format("15:04"))
}

// longClock is "24 de octubre de 2026, 09:12:31 VET".
func longClock(t time.Time) string {
	t = t.In(vet)
	return fmt.Sprintf("%d de %s de %d, %s VET", t.Day(), months[t.Month()-1], t.Year(), t.Format("15:04:05"))
}

func clock(t time.Time) string { return t.In(vet).Format("15:04:05") }

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// duration is "0,4 s", "42 s" or "1 min 23 s".
func duration(d time.Duration) string {
	s := d.Seconds()
	switch {
	case s < 10 && d%time.Second != 0:
		return decimal(s, 1) + " s"
	case s < 60:
		return fmt.Sprintf("%.0f s", math.Floor(s))
	}
	total := int(s)
	if total%60 == 0 {
		return fmt.Sprintf("%d min", total/60)
	}
	return fmt.Sprintf("%d min %d s", total/60, total%60)
}

// offset is a position in the call, "0:04" or "1:02".
func offset(secs float64) string {
	s := int(math.Max(0, secs))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// decimal formats with a decimal comma.
func decimal(v float64, places int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', places, 64), ".", ",", 1)
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return decimal(float64(n)/(1<<20), 1) + " MB"
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

func shortHex(b []byte) string { return hex.EncodeToString(b)[:16] + "…" }

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
