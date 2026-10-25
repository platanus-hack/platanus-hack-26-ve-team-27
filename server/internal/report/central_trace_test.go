package report

import (
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// The trace page is behind the central's key like every /central route.
func TestTraceNeedsTheKey(t *testing.T) {
	store := &fakeCentral{}
	store.setCase(sealedRecord(), CaseExtras{})
	srv := centralServer(t, store, testCentralKey)
	routes := []string{"/traza", "/traza/live", "/traza/estado.json"}
	for _, r := range routes {
		u := srv.URL + "/central/r/" + testIncidentID + r
		if resp, _ := cget(t, u, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("no key: GET %s = %d, want 404", r, resp.StatusCode)
		}
		if resp, _ := cget(t, u+"?key=wrong", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("wrong key: GET %s = %d, want 404", r, resp.StatusCode)
		}
	}
	ck := login(t, srv)
	resp, body := cget(t, srv.URL+"/central/r/"+testIncidentID+"/traza", ck)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `data-live-url="/central/r/`+testIncidentID+`/traza/live"`) {
		t.Fatalf("trace with the cookie = %d", resp.StatusCode)
	}
	checkPrivacy(t, "trace", resp.Header)
	if resp, _ := cget(t, srv.URL+"/central/r/00000000-0000-4000-8000-000000000000/traza", ck); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown case = %d, want 404", resp.StatusCode)
	}
	// The detail links to it.
	if _, body := cget(t, srv.URL+"/central/r/"+testIncidentID, ck); !strings.Contains(body, `href="/central/r/`+testIncidentID+`/traza">Ver traza</a>`) {
		t.Error("the detail has no Ver traza link")
	}
}

// Every section is there, with the phrase (the page is protected) and
// masked numbers only.
func TestTraceShowsEveryStep(t *testing.T) {
	rec := sealedRecord()
	rec.Device.UserName = "Mariana Fes"
	rec.Calls[0].TwilioStatus, rec.Calls[0].RingingAt = "completed", ptr(at("2026-10-24T13:10:03Z"))
	rec.Deliveries = []Delivery{
		{Recipient: "+58 424 ···33", Channel: "whatsapp", MessageID: "wamid.a", Status: "read", SentAt: at("2026-10-24T13:11:08Z"), ReadAt: ptr(at("2026-10-24T13:11:30Z"))},
		{Recipient: "+58 412 ···77", Channel: "sms", Status: "failed", Error: "21608: unverified", SentAt: at("2026-10-24T13:11:08Z")},
	}
	x := CaseExtras{SafePhrase: "todo bien mami", CallSIDs: map[int64]string{1: "CA0123456789"}}
	v := buildTrace(rec, x, Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, BaseURL: "https://mama.example.com", Now: at("2026-10-24T13:20:00Z")})
	s, err := traceSnapshot(v)
	if err != nil {
		t.Fatal(err)
	}
	text := html.UnescapeString(s.Sections["t-trace"])
	for _, want := range []string{
		"Resumen", "conectó (1 intento(s))", `tr-check ok"><span class="tr-mark" aria-hidden="true">✓</span><span class="tr-label">Webhook de ElevenLabs</span><span class="tr-value">llegó`,
		"encontrada en la llamada 1, turno 4 (0:15)", "duress · Coacción", "2 mensaje(s): 1 leído(s), 0 entregado(s), 0 enviado(s), 1 fallido(s)", "sellada",
		"Incidente", testIncidentID, "Mariana Fes · +58 414 ···67", "volume_pattern", "call · Caminando (4 km/h): Mamá llama",
		"09:10:00.253 VET · reloj del teléfono", "09:10:00.653 VET · reloj del servidor",
		"Llamadas", "conv_01", "CA0123456789", "completed", "Llegó el webhook de ElevenLabs (post_call_transcription): 2 turnos de la persona, 3 de Mamá.",
		"Detector de la frase", "«" + testPhrase + "»", "«todo bien mami»", "<code>carmen</code>",
		"Reglas, recalculadas ahora", "duress · Coacción: dijo la frase acordada", "Veredicto guardado",
		"Avisos", "+58 424 ···33", "wamid.a", "21608: unverified", `class="failed"`,
		"Sello", "Sellada", "Línea de tiempo", "Ficha sellada",
		"Grabación: 970 KB",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("trace lacks %q", want)
		}
	}
	if strings.Contains(text, "+584141234567") || strings.Contains(text, rec.Incident.DeviceID) {
		t.Error("the trace shows a full number or the device id")
	}
	if s.End || !strings.Contains(text, `tr-webhook tr-ok`) {
		t.Errorf("snapshot end %t", s.End)
	}
}

// The phrase is found in a user turn whatever its case, accents or
// punctuation, and only there; Mamá saying it never counts.
func TestTraceHighlightsThePhraseTurn(t *testing.T) {
	rec := openRecord()
	rec.Device.DuressPhrase = "Dile a la tía Carmen que llegó tarde"
	rec.Calls[0].Transcript = []contract.TranscriptTurn{
		{Role: "agent", Message: "¿Le digo a la tía Carmen que llegó tarde?", TimeInCallSecs: 0},
		{Role: "user", Message: "Hola mami.", TimeInCallSecs: 4},
		{Role: "user", Message: "OYE, DILE A LA TIA carmen... que LLEGO tarde!", TimeInCallSecs: 15},
	}
	v := buildTrace(rec, CaseExtras{}, Options{Token: testToken, Now: at("2026-10-24T13:11:40Z")})
	if !v.Calls[0].Matched || v.Calls[0].Turns[0].Match || v.Calls[0].Turns[1].Match || !v.Calls[0].Turns[2].Match {
		t.Fatalf("matches = %+v", v.Calls[0].Turns)
	}
	s, _ := traceSnapshot(v)
	text := html.UnescapeString(s.Sections["t-trace"])
	if !strings.Contains(text, `<li class="user match"><span class="tr-at">0:15</span><span class="tr-who">Persona</span><span class="tr-text">OYE, DILE A LA TIA carmen... que LLEGO tarde!</span><span class="tr-tag">frase encontrada</span></li>`) ||
		!strings.Contains(text, "✓ La frase aparece en un turno de la persona.") {
		t.Errorf("highlight:\n%s", text)
	}
	rec.Calls[0].Transcript = rec.Calls[0].Transcript[:2]
	v = buildTrace(rec, CaseExtras{}, Options{Token: testToken, Now: at("2026-10-24T13:11:40Z")})
	if s, _ := traceSnapshot(v); v.Calls[0].Matched || !strings.Contains(html.UnescapeString(s.Sections["t-trace"]), "✗ La frase no aparece en ningún turno de la persona.") {
		t.Error("a transcript without the phrase is not said so")
	}
}

// A call that ended (Twilio completed, or long ago) with no post-call
// webhook says so in red, naming the URL and the secret to check; one still
// running waits.
func TestTraceSaysWhenTheWebhookNeverCame(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Status, c.DurationSecs, c.EndedAt, c.Transcript, c.Verdict, c.VerdictAt, c.Recording = "in_progress", nil, nil, nil, "", nil, nil
	rec.Incident.NotifiedAt = nil
	opts := Options{Token: testToken, BaseURL: "https://mama-server.onrender.com/", Now: c.RequestedAt.Add(30 * time.Second)}
	missing := "No llegó el webhook de ElevenLabs: revisa que apunte a https://mama-server.onrender.com/webhooks/elevenlabs y que ELEVENLABS_WEBHOOK_SECRET coincida."

	if v := buildTrace(rec, CaseExtras{}, opts); v.Calls[0].WebhookState != "waiting" {
		t.Errorf("a running call: %s %q", v.Calls[0].WebhookState, v.Calls[0].Webhook)
	}
	c.TwilioStatus = "completed"
	v := buildTrace(rec, CaseExtras{}, opts)
	s, _ := traceSnapshot(v)
	if v.Calls[0].WebhookState != "missing" || !strings.Contains(html.UnescapeString(s.Sections["t-trace"]), `<p class="tr-webhook tr-missing">`+missing) ||
		!strings.Contains(html.UnescapeString(s.Sections["t-trace"]), "NO llegó: revisa la URL y ELEVENLABS_WEBHOOK_SECRET") {
		t.Errorf("Twilio completed, no webhook: %s %q", v.Calls[0].WebhookState, v.Calls[0].Webhook)
	}
	c.TwilioStatus = ""
	opts.Now = c.RequestedAt.Add(webhookWait + time.Second)
	if v := buildTrace(rec, CaseExtras{}, opts); v.Calls[0].WebhookState != "missing" || !strings.Contains(v.Calls[0].Webhook, "/webhooks/elevenlabs") {
		t.Errorf("long after the call: %s", v.Calls[0].WebhookState)
	}
	opts.BaseURL = ""
	if v := buildTrace(rec, CaseExtras{}, opts); !strings.Contains(v.Calls[0].Webhook, "<PUBLIC_URL>/webhooks/elevenlabs") {
		t.Errorf("without PUBLIC_URL: %q", v.Calls[0].Webhook)
	}
	c.Status = "failed"
	if v := buildTrace(rec, CaseExtras{}, opts); v.Calls[0].WebhookState == "missing" {
		t.Error("a failed call asks for its transcript webhook")
	}
}
