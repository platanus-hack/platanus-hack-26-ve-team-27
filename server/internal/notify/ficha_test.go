package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

func carFicha() *contract.CallContext {
	return &contract.CallContext{
		Lugar: "la autopista", Personas: "dos panas", Vehiculo: "un carro", Rumbo: contract.CallContextUnknown,
		Situacion: "Va en un carro con dos panas y dijo la frase de alerta.", Confianza: contract.ConfianzaMedia,
	}
}

// #106: the WhatsApp shows the call's context, only the fields it knows,
// between the verdict and the time.
func TestFormatShowsOnlyTheKnownFichaFields(t *testing.T) {
	msg := sampleMessage()
	msg.Ficha = carFicha()
	want := "🚨 Alerta de Aló Mamá\n" +
		"Ubicación: 10.4961, -66.8531 (±12 m)\n" +
		"Veredicto: coacción\n" +
		"Dijo que está: la autopista\n" +
		"Con: dos panas\n" +
		"En: un carro\n" +
		"Lo que parece: Va en un carro con dos panas y dijo la frase de alerta. (confianza media)\n" +
		"Hora: 09:10 VET"
	if got := Format(msg); got != want {
		t.Fatalf("Format =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatFichaEdgeCases(t *testing.T) {
	unknown := contract.CallContext{
		Lugar: contract.CallContextUnknown, Personas: "", Vehiculo: "No se sabe.", Rumbo: contract.CallContextUnknown,
		Situacion: contract.CallContextUnknown, Confianza: contract.ConfianzaBaja,
	}
	msg := sampleMessage()
	msg.Ficha = &unknown
	if got, plain := Format(msg), Format(sampleMessage()); got != plain {
		t.Errorf("a ficha with nothing known changes the message:\n%s", got)
	}

	onlyRumbo := unknown
	onlyRumbo.Rumbo = "hacia\nValencia"
	msg.Ficha = &onlyRumbo
	got := Format(msg)
	if !strings.Contains(got, "Veredicto: coacción\nVa hacia: hacia Valencia\nHora:") || strings.Contains(got, "Lo que parece") {
		t.Errorf("Format =\n%s\nwant only the rumbo line, on one line", got)
	}
}

func TestFormatUnclearSaysNotConfirmed(t *testing.T) {
	msg := sampleMessage()
	msg.Verdict, msg.Ficha = contract.VerdictUnclear, carFicha()
	got := Format(msg)
	if !strings.Contains(got, "Veredicto: no confirmó que está bien\nDijo que está: la autopista\n") {
		t.Errorf("Format =\n%s", got)
	}
}

// The safe phrase (no_duress) notifies nobody, the reporter included.
func TestNotifySendsNothingOnNoDuress(t *testing.T) {
	msg := sampleMessage()
	msg.Verdict, msg.Reporter = contract.VerdictNoDuress, "+584149998877"
	store := &fakeStore{msg: msg}
	sender := &bodySender{}
	New(store, sender).WithSelf().Notify(context.Background(), intake.Incident{ID: incidentID})
	if store.claims != 0 || len(sender.bodies) != 0 {
		t.Fatalf("claims=%d bodies=%v, want nothing sent and the incident unclaimed", store.claims, sender.bodies)
	}
}

func TestNotifySendsUnclear(t *testing.T) {
	msg := sampleMessage()
	msg.Verdict = contract.VerdictUnclear
	sender := &bodySender{}
	New(&fakeStore{msg: msg}, sender).Notify(context.Background(), intake.Incident{ID: incidentID})
	if len(sender.bodies) != 1 || !strings.Contains(sender.bodies[0], "no confirmó que está bien") {
		t.Fatalf("bodies %v, want one unclear message", sender.bodies)
	}
}

// The WhatsApp carries the ficha; the SMS fallback does not.
func TestOnlyWhatsAppCarriesTheFicha(t *testing.T) {
	msg := sampleMessage()
	msg.Ficha = carFicha()

	k := newKapsoServer(t, http.StatusOK)
	New(&fakeStore{msg: msg}, k.sender()).Notify(context.Background(), intake.Incident{ID: incidentID})
	if k.count() != 1 || !strings.Contains(k.messages[0].Text.Body, "Con: dos panas") {
		t.Fatalf("Kapso got %+v, want the ficha in the text", k.messages)
	}

	var mu sync.Mutex
	var bodies []string
	twilio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		bodies = append(bodies, r.PostForm.Get("Body"))
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(twilio.Close)
	sms := &SMS{BaseURL: twilio.URL, AccountSID: "AC1", AuthToken: "t", From: "+15550001111", Client: twilio.Client()}
	New(&fakeStore{msg: msg}, sms).Notify(context.Background(), intake.Incident{ID: incidentID})
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || bodies[0] != Format(sampleMessage()) {
		t.Fatalf("SMS bodies %q, want the message without the ficha", bodies)
	}
}
