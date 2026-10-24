package report

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

type row struct {
	kind   EventKind
	time   string // VET
	clock  Clock
	title  string
	detail string
}

func rows(ev []Event) []row {
	out := make([]row, len(ev))
	for i, e := range ev {
		out[i] = row{e.Kind, e.Time(), e.Clock, e.Title, e.Detail}
	}
	return out
}

// Every event of the #76 table, ordered by time, each with its clock.
func TestTimelineHasEveryEventInTimeOrderWithItsClock(t *testing.T) {
	rec := sealedRecord()
	got := rows(Timeline(rec, Options{Channel: "WhatsApp"}))
	want := []row{
		{EventFirstPoint, "09:10:00", ClockPhone, "Primera ubicación", "Ubicación ±12 m · caminando · 4 km/h"},
		{EventTrigger, "09:10:00", ClockPhone, "Pidió ayuda", "Pidió ayuda con el gesto de volumen"},
		{EventReceived, "09:10:00", ClockServer, "Llegó al servidor", "Recibido 0,4 s después"},
		{EventBranch, "09:10:00", ClockServer, "Rama elegida", "Caminando (4 km/h): Mamá llama"},
		{EventCallRequested, "09:10:01", ClockServer, "Mamá llamó", "Llamando a +58 ···67"},
		{EventAnswered, "09:10:04", ClockCall, "Contestó", "5 turnos de conversación"},
		{EventPhrase, "09:10:19", ClockCall, "Dijo la frase acordada", ""},
		{EventPoint, "09:10:45", ClockPhone, "Ubicación", "Ubicación ±8 m · caminando · 5 km/h"},
		{EventHungUp, "09:11:05", ClockCall, "Colgó", "Llamada de 1 min 1 s"},
		{EventVerdict, "09:11:07", ClockServer, "Veredicto", "Coacción: dijo la frase acordada"},
		{EventNotified, "09:11:08", ClockServer, "Aviso a la familia", "WhatsApp enviado a 2 familiares"},
		{EventRecording, "09:11:20", ClockServer, "Grabación guardada", "SHA-256 " + shortHex(rec.Calls[0].Recording.SHA256[:])},
		{EventSealed, "09:11:25", ClockServer, "Ficha sellada", "Firmada con la llave Ed25519 del servidor"},
		{EventPoint, "09:11:30", ClockPhone, "Ubicación", "Ubicación ±10 m · sin velocidad"},
	}
	if !reflect.DeepEqual(got, want) {
		for i := range max(len(got), len(want)) {
			var g, w row
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Errorf("event %d:\n got  %+v\n want %+v", i, g, w)
			}
		}
	}
}

func TestTimelinePhraseEventQuotesTheTurnAndSeeksToIt(t *testing.T) {
	for _, e := range Timeline(sealedRecord(), Options{}) {
		if e.Kind == EventPhrase {
			if e.Quote != "Sí, mami. Oye, dile a la tía Carmen que llego tarde." || e.Seek != 15 || e.Call != 1 {
				t.Errorf("phrase event = %+v", e)
			}
			return
		}
	}
	t.Fatal("no phrase event")
}

// A retried call: each calls row is its own block, and a failed one says so
// on the server clock (the engine stamped it).
func TestTimelineRetriedCallKeepsEachBlock(t *testing.T) {
	rec := openRecord()
	failed := Call{ID: 1, Status: "failed", RequestedAt: at("2026-10-24T13:10:01Z"), EndedAt: ptr(at("2026-10-24T13:10:20Z"))}
	second := rec.Calls[0]
	second.ID, second.RequestedAt = 2, at("2026-10-24T13:10:21Z")
	second.EndedAt, second.VerdictAt = ptr(at("2026-10-24T13:11:30Z")), ptr(at("2026-10-24T13:11:32Z")) // answered 13:10:29
	rec.Calls = []Call{failed, second}

	var calls []string
	for _, e := range Timeline(rec, Options{}) {
		if e.Call != 0 {
			calls = append(calls, string(e.Kind)+"#"+string(rune('0'+e.Call)))
		}
		if e.Kind == EventHungUp && e.Call == 1 && (!e.Error || e.Clock != ClockServer || e.Title != "La llamada falló") {
			t.Errorf("failed call end = %+v, want an error on the server clock", e)
		}
	}
	want := []string{"call_requested#1", "hung_up#1", "call_requested#2", "answered#2", "phrase#2", "hung_up#2", "verdict#2"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("call events = %v, want %v", calls, want)
	}
}

func TestTimelineWithoutCall(t *testing.T) {
	rec := openRecord()
	rec.Incident.Branch = contract.BranchImmediateAlert
	rec.Incident.SpeedMPS = ptr(0.2)
	rec.Calls = nil
	var kinds []EventKind
	for _, e := range Timeline(rec, Options{Channel: "SMS"}) {
		kinds = append(kinds, e.Kind)
		if e.Kind == EventBranch && e.Detail != "Quieta: aviso inmediato" {
			t.Errorf("branch = %q", e.Detail)
		}
		if e.Kind == EventNotified && e.Detail != "SMS enviado a 2 familiares" {
			t.Errorf("notified = %q", e.Detail)
		}
	}
	want := []EventKind{EventFirstPoint, EventTrigger, EventReceived, EventBranch, EventPoint, EventNotified, EventPoint}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

func TestTimelinePhoneClockAheadOfServer(t *testing.T) {
	rec := openRecord()
	rec.Incident.TriggeredAt = rec.Incident.CreatedAt.Add(2 * time.Second)
	for _, e := range Timeline(rec, Options{}) {
		if e.Kind == EventReceived && e.Detail != "Recibido; el reloj del teléfono va 2 s adelantado" {
			t.Errorf("received = %q", e.Detail)
		}
	}
}

func TestBuildStates(t *testing.T) {
	sealed := Build(sealedRecord(), Options{CanSeal: true})
	if sealed.State != "sealed" || sealed.StateLabel != "Cerrado y sellado" || sealed.Character != "lista" || sealed.Verdict != "Coacción" {
		t.Errorf("sealed: %s %q %s %q", sealed.State, sealed.StateLabel, sealed.Character, sealed.Verdict)
	}
	open := Build(openRecord(), Options{CanSeal: true})
	if open.State != "open" || open.StateLabel != "Abierto" || open.Character != "escuchando" || open.Seal.Sealed {
		t.Errorf("open: %s %q %s", open.State, open.StateLabel, open.Character)
	}
	noKey := Build(openRecord(), Options{CanSeal: false})
	if noKey.StateLabel != "Sin sellar" || !strings.Contains(noKey.Seal.Why, "llave") {
		t.Errorf("no key: %q %q", noKey.StateLabel, noKey.Seal.Why)
	}
	rec := openRecord()
	rec.Incident.Branch, rec.Calls = contract.BranchSilentTracker, nil
	tracker := Build(rec, Options{CanSeal: true})
	if tracker.StateLabel != "Sin sellar" || tracker.Verdict != "Rastreo silencioso" || tracker.NoCall == "" || !strings.Contains(tracker.Seal.Why, "Solo se sellan") {
		t.Errorf("tracker: %q %q %q %q", tracker.StateLabel, tracker.Verdict, tracker.NoCall, tracker.Seal.Why)
	}
}

func TestBuildCallViewTurnsAndAudio(t *testing.T) {
	p := Build(sealedRecord(), Options{Token: testToken})
	c := p.Calls[0]
	if c.Number != "+58 ···67" || c.Duration != "1 min 1 s" || c.Audio == nil || c.Audio.URL != "/i/"+testToken+"/audio.mp3?llamada=1" {
		t.Fatalf("call = %+v audio %+v", c, c.Audio)
	}
	if len(c.Turns) != 5 {
		t.Fatalf("turns = %d", len(c.Turns))
	}
	phrase := c.Turns[3]
	if !phrase.Phrase || phrase.Agent || phrase.Speaker != "Tu familiar" || phrase.Offset != "0:15" || phrase.At != "09:10:19" || phrase.Seek != 15 {
		t.Errorf("phrase turn = %+v", phrase)
	}
	for i, tv := range c.Turns {
		if i != 3 && tv.Phrase {
			t.Errorf("turn %d highlighted: %+v", i, tv)
		}
	}
	if !c.Turns[0].Agent || c.Turns[0].Speaker != "Mamá" {
		t.Errorf("turn 0 = %+v", c.Turns[0])
	}
	if p.Seal.JSONURL != "/i/"+testToken+"/ficha.json" || p.Seal.SigURL != "/i/"+testToken+"/ficha.sig" || len(p.Seal.Recordings) != 1 {
		t.Errorf("seal = %+v", p.Seal)
	}

	noAudio := Build(openRecord(), Options{Token: testToken}).Calls[0]
	if noAudio.Audio != nil || noAudio.NoAudio != "Guardando la grabación de la llamada…" {
		t.Errorf("no audio = %+v", noAudio)
	}
}

func TestPhraseTurnsSkipsSilenceAndOtherSpeakers(t *testing.T) {
	transcript := []contract.TranscriptTurn{
		{Role: "agent", Message: testPhrase},                            // Mamá saying it is not the user saying it
		{Role: "user", Message: "[silencio]"},                           // silence is duress for the rules, but nothing to highlight
		{Role: "user", Message: "no le digas nada"},                     // not the phrase
		{Role: "user", Message: "Dile a la tía Carmen que llegó tarde"}, // accents and case fold
	}
	got := phraseTurns(transcript, testPhrase)
	if !reflect.DeepEqual(got, map[int]bool{3: true}) {
		t.Errorf("phraseTurns = %v, want only turn 3", got)
	}
	if len(phraseTurns(transcript, "  ")) != 0 {
		t.Error("an empty phrase highlights a turn")
	}
}

// Every reason the rules detector writes has Spanish copy, so a change in
// internal/duress shows up here instead of as English on the page.
func TestEveryRulesReasonIsTranslated(t *testing.T) {
	user := func(s string) []contract.TranscriptTurn {
		return []contract.TranscriptTurn{{Role: "agent", Message: "¿Aló?"}, {Role: "user", Message: s}}
	}
	cases := []struct {
		transcript []contract.TranscriptTurn
		phrase     string
	}{
		{nil, testPhrase},
		{user("[silencio]"), testPhrase},
		{user("hola"), ""},
		{user(testPhrase), testPhrase},
		{user("dile a la tía Carmen que voy tarde"), testPhrase},
		{user("no le digas a la tía Carmen que llego tarde"), testPhrase},
		{user("la tía Carmen llego"), testPhrase},
		{user("todo bien, mami"), testPhrase},
	}
	verdicts := map[contract.Verdict]bool{}
	for _, tc := range cases {
		r, err := duress.Rules{}.Detect(context.Background(), tc.transcript, tc.phrase)
		if err != nil {
			t.Fatal(err)
		}
		verdicts[r.Verdict] = true
		if reasonES(r.Reason) == "" {
			t.Errorf("reason %q has no Spanish copy", r.Reason)
		}
		chained := "rules: " + r.Reason + "; LLM: something"
		if !strings.HasSuffix(reasonES(chained), "(revisado con IA)") {
			t.Errorf("chained reason %q = %q", chained, reasonES(chained))
		}
	}
	if len(verdicts) != 3 {
		t.Errorf("cases cover verdicts %v, want all three", verdicts)
	}
	if got := verdictLine(contract.VerdictUnclear, "something new"); got != "No está claro" {
		t.Errorf("unknown reason = %q, want the verdict alone", got)
	}
}

func TestFormatting(t *testing.T) {
	for d, want := range map[time.Duration]string{
		400 * time.Millisecond: "0,4 s",
		42 * time.Second:       "42 s",
		61 * time.Second:       "1 min 1 s",
		120 * time.Second:      "2 min",
		83 * time.Second:       "1 min 23 s",
	} {
		if got := duration(d); got != want {
			t.Errorf("duration(%s) = %q, want %q", d, got, want)
		}
	}
	if got := longDate(at("2026-10-24T13:10:00.253Z")); got != "sábado 24 de octubre de 2026, 09:10 VET" {
		t.Errorf("longDate = %q", got)
	}
	if got := maskPhone("+584141234567"); got != "+58 ···67" {
		t.Errorf("maskPhone = %q", got)
	}
	if got := maskPhone("+15555550100"); got != "···00" {
		t.Errorf("maskPhone(+1) = %q", got)
	}
	if got := offset(75.6); got != "1:15" {
		t.Errorf("offset = %q", got)
	}
	if got := size(993280); got != "970 KB" {
		t.Errorf("size = %q", got)
	}
	if got := Fingerprint(make([]byte, 32)); len(got) != 64+15 {
		t.Errorf("Fingerprint = %q", got)
	}
}

func TestRouteDrawsThePathInsideTheBox(t *testing.T) {
	r := route(sealedRecord().Points)
	if r == nil || len(r.Segments) != 2 || len(r.Dots) != 3 || r.DistanceKM == "" || !r.Dots[0].First || !r.Dots[2].Newest || r.Dots[1].Newest {
		t.Fatalf("route = %+v", r)
	}
	for _, d := range r.Dots {
		x, _ := strconv.ParseFloat(d.X, 64)
		y, _ := strconv.ParseFloat(d.Y, 64)
		if x < 0 || x > 320 || y < 0 || y > 180 || !strings.HasPrefix(d.MapsURL, "https://www.google.com/maps/") {
			t.Errorf("dot %+v outside the box or without a Maps link", d)
		}
	}
	if one := route(sealedRecord().Points[:1]); one == nil || len(one.Segments) != 0 || len(one.Dots) != 1 || !one.Dots[0].Newest {
		t.Errorf("a single point = %+v, want one pulsing dot", one)
	}
	if route(nil) != nil {
		t.Error("no points draws a route")
	}
	same := []Point{{Lat: 1, Lng: 1}, {Lat: 1, Lng: 1}}
	if r := route(same); r == nil || r.Dots[0].X != "160.0" || r.Dots[0].Y != "90.0" || r.DistanceKM != "0 m" {
		t.Errorf("equal points = %+v, want centered", r)
	}
	// 16 m of GPS jitter stays small: well under a tenth of the box.
	jitter := route([]Point{{Lat: 10.4806, Lng: -66.9036}, {Lat: 10.4807, Lng: -66.9035}})
	sx, _ := strconv.ParseFloat(jitter.Dots[0].X, 64)
	ex, _ := strconv.ParseFloat(jitter.Dots[1].X, 64)
	if math.Abs(ex-sx) > 32 || jitter.DistanceKM != "16 m" {
		t.Errorf("jitter route = %+v, want a short line and 16 m", jitter)
	}
	// A new point that widens the drawing redraws every segment (new keys).
	more := append(sealedRecord().Points, Point{Lat: 10.51, Lng: -66.84})
	if r2 := route(more); r2.Segments[0].Key == r.Segments[0].Key {
		t.Error("a rescaled segment kept its key")
	}
}
