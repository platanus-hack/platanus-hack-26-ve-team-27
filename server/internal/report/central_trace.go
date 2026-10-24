package report

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/trace"
)

// The trace page, GET /central/r/{incident_id}/traza: every step of one
// case read back from the database, to check a real call end to end, above
// all that the duress phrase was heard. It sits behind CENTRAL_KEY like the
// rest of /central, so it may show the device's phrases (never a full phone
// number). It is live through the same loop as the detail.

// CentralTraceSections are the trace page's live sections.
var CentralTraceSections = []string{"t-trace"}

// webhookWait is how long after a call is asked for that its post-call
// webhook counts as missing, when Twilio did not already say it ended.
const webhookWait = 4 * time.Minute

// traceView is what the trace page renders.
type traceView struct {
	Theme    string
	Case     CaseRow
	ID       string
	Incident [][2]string
	Events   []Event // the report's timeline, every clock labelled
	Calls    []traceCall
	Phrase   string // the device's duress phrase (shown: the page is protected)
	Safe     string // its safe phrase (#104), "" without one
	Keys     []string
	Notified []traceDelivery
	Note     string // the notifications' line when there are none
	Seal     [][2]string
	Summary  []traceCheck // the answers first: did each step work?
	at       time.Time
	follow   bool
}

// traceCheck is one line of the summary at the top.
type traceCheck struct {
	Label string
	Value string
	State string // ok, bad or wait
}

// traceCall is one call attempt: its facts, whether ElevenLabs' post-call
// webhook arrived, and the detector over its transcript.
type traceCall struct {
	Key          string
	Title        string
	Facts        [][2]string
	Webhook      string // what is known about the post-call webhook
	WebhookState string // ok, waiting or missing
	Turns        []traceTurn
	Matched      bool   // a user turn holds the phrase (diagnostic matcher)
	Rules        string // the rules re-run on the stored transcript now
	Stored       string // the stored verdict and reason
	Recording    string
}

// traceTurn is one transcript turn on the trace page.
type traceTurn struct {
	Who    string // Mamá or Persona
	Agent  bool
	Offset string
	Text   string
	Match  bool // holds every key word of the duress phrase
}

// traceDelivery is one family message on the trace page.
type traceDelivery struct {
	Recipient, Channel, Status, Sent, Delivered, Read, MessageID, Error string
	Failed                                                              bool
}

// buildTrace turns the record into the trace page.
func buildTrace(rec Record, x CaseExtras, opts Options) traceView {
	inc := rec.Incident
	now := opts.Now
	page := Build(rec, opts)
	v := traceView{
		Case: caseRow(caseItemOf(rec, x), now), ID: inc.ID, Events: page.Timeline,
		Phrase: rec.Device.DuressPhrase, Safe: x.SafePhrase, Keys: trace.KeyWords(rec.Device.DuressPhrase), at: now,
		follow: page.Live || (rec.Seal != nil && now.Sub(rec.Seal.SealedAt) < followSeal),
	}
	speed := "sin velocidad"
	if inc.SpeedMPS != nil {
		speed = kmh(*inc.SpeedMPS) + fmt.Sprintf(" (%.2f m/s)", *inc.SpeedMPS)
	}
	v.Incident = [][2]string{
		{"incident_id", inc.ID},
		{"Quién", nonEmpty(rec.Device.UserName, "sin nombre") + " · " + MaskRecipient(rec.Device.PhoneNumber)},
		{"Gatillo", string(inc.Trigger) + " · " + triggerLabel(inc.Trigger)},
		{"Rama", string(inc.Branch) + " · " + branchLabel(inc.Branch, inc.SpeedMPS)},
		{"Velocidad", speed},
		{"Pidió ayuda", stampVET(inc.TriggeredAt) + " · reloj del teléfono"},
		{"Llegó al servidor", stampVET(inc.CreatedAt) + " · reloj del servidor · " + receivedLabel(inc.CreatedAt.Sub(inc.TriggeredAt))},
		{"Familia avisada (claim)", stampPtrVET(inc.NotifiedAt, "todavía no") + " · reloj del servidor"},
		{"Puntos GPS", strconv.Itoa(len(rec.Points))},
		{"Estado en la central", v.Case.StateLabel},
	}
	base := strings.TrimSuffix(opts.BaseURL, "/")
	if base == "" {
		base = "<PUBLIC_URL>"
	}
	keys := trace.KeyWords(rec.Device.DuressPhrase)
	for i, c := range rec.Calls {
		n := i + 1
		tc := traceCall{Key: "call-" + strconv.Itoa(n), Title: "Llamada " + strconv.Itoa(n)}
		if n > 1 {
			tc.Title += " (reintento)"
		}
		tc.Facts = [][2]string{
			{"Estado (calls.status)", c.Status},
			{"conversation_id", nonEmpty(c.ConversationID, "— (ElevenLabs no respondió al pedido)")},
			{"call_sid (Twilio)", nonEmpty(x.CallSIDs[c.ID], "—")},
			{"Pedida", stampVET(c.RequestedAt) + " · servidor"},
			{"Último estado de Twilio", nonEmpty(c.TwilioStatus, "— (sin consultar)")},
			{"Sonó", stampPtrVET(c.RingingAt, "—") + " · servidor (Twilio)"},
			{"Contestó", stampPtrVET(c.PickedUpAt, "—") + " · servidor (Twilio)"},
			{"Terminó", stampPtrVET(c.EndedAt, "—")},
		}
		if c.DurationSecs != nil {
			tc.Facts = append(tc.Facts, [2]string{"Duración (ElevenLabs)", duration(time.Duration(*c.DurationSecs) * time.Second)})
		}
		switch {
		case c.DurationSecs != nil:
			u, a := trace.Turns(c.Transcript)
			tc.WebhookState = "ok"
			tc.Webhook = fmt.Sprintf("Llegó el webhook de ElevenLabs (post_call_transcription): %d turnos de la persona, %d de Mamá.", u, a)
		case c.Status == "failed":
			tc.WebhookState = "waiting"
			tc.Webhook = "La llamada no conectó: no hay transcripción que esperar."
		case c.TwilioStatus == "completed" || now.Sub(c.RequestedAt) > webhookWait:
			tc.WebhookState = "missing"
			tc.Webhook = "No llegó el webhook de ElevenLabs: revisa que apunte a " + base + "/webhooks/elevenlabs y que ELEVENLABS_WEBHOOK_SECRET coincida."
		default:
			tc.WebhookState = "waiting"
			tc.Webhook = "Esperando el webhook de ElevenLabs: llega cuando la llamada termina."
		}
		match := map[int]bool{}
		for _, j := range trace.PhraseTurns(c.Transcript, rec.Device.DuressPhrase) {
			match[j] = true
			tc.Matched = true
		}
		for j, t := range c.Transcript {
			agent := t.Role == duress.RoleAgent
			who := "Persona"
			if agent {
				who = "Mamá"
			}
			tc.Turns = append(tc.Turns, traceTurn{Who: who, Agent: agent, Offset: offset(t.TimeInCallSecs), Text: t.Message, Match: match[j]})
		}
		if len(c.Transcript) > 0 && len(keys) > 0 {
			if r, err := (duress.Rules{}).Detect(context.Background(), c.Transcript, duress.Phrases{Duress: rec.Device.DuressPhrase, Safe: x.SafePhrase}); err == nil {
				tc.Rules = string(r.Verdict) + " · " + verdictLine(r.Verdict, r.Reason) + " (" + r.Reason + ")"
			}
		}
		if c.Verdict != "" {
			tc.Stored = string(c.Verdict) + " · " + verdictLine(c.Verdict, c.VerdictReason) + " · motivo: " + nonEmpty(c.VerdictReason, "—") + " · " + stampPtrVET(c.VerdictAt, "")
		}
		if r := c.Recording; r != nil {
			tc.Recording = fmt.Sprintf("%s, %s, guardada %s · SHA-256 %s", size(r.Size), r.ContentType, stampVET(r.StoredAt), shortHex(r.SHA256[:]))
		}
		v.Calls = append(v.Calls, tc)
	}
	for _, d := range rec.Deliveries {
		v.Notified = append(v.Notified, traceDelivery{
			Recipient: d.Recipient, Channel: channelName(d.Channel), Status: d.Status, Sent: stampVET(d.SentAt),
			Delivered: stampPtrVET(d.DeliveredAt, "—"), Read: stampPtrVET(d.ReadAt, "—"),
			MessageID: nonEmpty(d.MessageID, "—"), Error: d.Error, Failed: d.Status == "failed",
		})
	}
	if len(v.Notified) == 0 {
		v.Note = page.FamilyNote
	}
	v.Summary = traceSummary(rec, v)
	if s := rec.Seal; s != nil {
		v.Seal = [][2]string{
			{"Sellada", stampVET(s.SealedAt)},
			{"ficha.json", fmt.Sprintf("%d bytes", len(s.Document))},
			{"Llave", Fingerprint(s.PublicKey)},
		}
	}
	return v
}

// stampVET is a time to the millisecond in VET, for the trace.
func stampVET(t time.Time) string { return t.In(vet).Format("15:04:05.000") + " VET" }

func stampPtrVET(t *time.Time, none string) string {
	if t == nil {
		return none
	}
	return stampVET(*t)
}

// ---------------------------------------------------------------- handlers

func (h *Handler) traceOf(ctx context.Context, id string) (traceView, error) {
	rec, err := h.central.Store.Load(ctx, id)
	if err != nil {
		return traceView{}, err
	}
	x, err := h.central.Store.Extras(ctx, id)
	if err != nil {
		return traceView{}, err
	}
	opts := h.opts
	opts.Now = h.now()
	return buildTrace(rec, x, opts), nil
}

func traceSnapshot(v traceView) (Snapshot, error) {
	return renderSnapshot(v, v.Case.State, v.follow, false, v.at, CentralTraceSections)
}

func (h *Handler) centralTrace(w http.ResponseWriter, r *http.Request) {
	v, err := h.traceOf(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrUnknownIncident) {
		centralNotFound(w)
		return
	}
	if err != nil {
		log.Printf("report: central: trace: %v", err)
		http.Error(w, "No se pudo cargar la traza.", http.StatusInternalServerError)
		return
	}
	v.Theme = theme(r)
	render(w, http.StatusOK, "central-trace", v)
}

func (h *Handler) centralTraceLive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.traceOf(r.Context(), id); err != nil {
		if errors.Is(err, ErrUnknownIncident) {
			centralNotFound(w)
			return
		}
		log.Printf("report: central: trace: %v", err)
		http.Error(w, "No se pudo cargar la traza.", http.StatusInternalServerError)
		return
	}
	h.serveSnapshots(w, r, func(ctx context.Context) (Snapshot, error) {
		v, err := h.traceOf(ctx, id)
		if err != nil {
			return Snapshot{}, err
		}
		return traceSnapshot(v)
	})
}

func (h *Handler) centralTraceState(w http.ResponseWriter, r *http.Request) {
	v, err := h.traceOf(r.Context(), r.PathValue("id"))
	if err == nil {
		var s Snapshot
		if s, err = traceSnapshot(v); err == nil {
			writeJSON(w, s)
			return
		}
	}
	if errors.Is(err, ErrUnknownIncident) {
		centralNotFound(w)
		return
	}
	log.Printf("report: central: trace: %v", err)
	http.Error(w, "No se pudo cargar la traza.", http.StatusInternalServerError)
}

// traceSummary answers, in order, what a test call needs checked: did the
// call connect, did ElevenLabs' webhook arrive, was the phrase heard, what
// was the verdict, did the family messages go out, is it sealed.
func traceSummary(rec Record, v traceView) []traceCheck {
	var out []traceCheck
	add := func(label, value, state string) { out = append(out, traceCheck{label, value, state}) }
	if len(rec.Calls) == 0 {
		add("Llamada", "esta rama no llama ("+string(rec.Incident.Branch)+")", "wait")
	} else {
		connected := false
		for _, c := range rec.Calls {
			if c.DurationSecs != nil || c.PickedUpAt != nil {
				connected = true
			}
		}
		switch {
		case connected:
			add("Llamada", fmt.Sprintf("conectó (%d intento(s))", len(rec.Calls)), "ok")
		case rec.Calls[len(rec.Calls)-1].Status == "failed":
			add("Llamada", fmt.Sprintf("no conectó en %d intento(s)", len(rec.Calls)), "bad")
		default:
			add("Llamada", "en curso", "wait")
		}
		last := v.Calls[len(v.Calls)-1]
		for _, c := range v.Calls { // the webhook that matters: any that arrived, else the newest
			if c.WebhookState == "ok" {
				last = c
			}
		}
		switch last.WebhookState {
		case "ok":
			add("Webhook de ElevenLabs", "llegó", "ok")
		case "missing":
			add("Webhook de ElevenLabs", "NO llegó: revisa la URL y ELEVENLABS_WEBHOOK_SECRET", "bad")
		default:
			add("Webhook de ElevenLabs", "esperando", "wait")
		}
		found, transcript := "", false
		for i, c := range rec.Calls {
			if len(c.Transcript) > 0 {
				transcript = true
			}
			if turns := trace.PhraseTurns(c.Transcript, rec.Device.DuressPhrase); len(turns) > 0 && found == "" {
				t := c.Transcript[turns[0]]
				found = fmt.Sprintf("encontrada en la llamada %d, turno %d (%s)", i+1, turns[0]+1, offset(t.TimeInCallSecs))
			}
		}
		switch {
		case found != "":
			add("Frase de alerta", found, "ok")
		case transcript:
			add("Frase de alerta", "no aparece en lo que dijo la persona", "bad")
		default:
			add("Frase de alerta", "sin transcripción todavía", "wait")
		}
		verdict, kind := headline(rec)
		if kind == "none" {
			add("Veredicto", "todavía no hay", "wait")
		} else {
			add("Veredicto", kind+" · "+verdict, "ok")
		}
	}
	sent, failed, delivered, read := 0, 0, 0, 0
	for _, d := range rec.Deliveries {
		switch d.Status {
		case "failed":
			failed++
		case "read":
			read++
		case "delivered":
			delivered++
		default:
			sent++
		}
	}
	switch n := len(rec.Deliveries); {
	case n == 0 && rec.Incident.NotifiedAt == nil:
		add("Avisos", "ninguno todavía", "wait")
	case n == 0:
		add("Avisos", "reclamado sin envíos registrados", "wait")
	case failed == n:
		add("Avisos", fmt.Sprintf("los %d fallaron", n), "bad")
	default:
		state := "ok"
		if failed > 0 {
			state = "bad"
		}
		add("Avisos", fmt.Sprintf("%d mensaje(s): %d leído(s), %d entregado(s), %d enviado(s), %d fallido(s)", n, read, delivered, sent, failed), state)
	}
	if rec.Seal != nil {
		add("Sello", "sellada", "ok")
	} else {
		add("Sello", "sin sellar todavía", "wait")
	}
	return out
}
