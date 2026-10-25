package report

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

func pdfOpts() PDFOptions {
	return PDFOptions{Token: testToken, BaseURL: "https://mama.example.com", Channel: "WhatsApp", Now: at("2026-10-24T13:20:00Z")}
}

// Sealed, the PDF reads the facts back from the signed ficha.json: a row
// changed after the seal does not reach it.
func TestPDFOfASealedReportMatchesTheSeal(t *testing.T) {
	rec := sealedRecord()
	rec.Calls[0].Verdict = contract.VerdictNoDuress // changed after the seal
	rec.Points = rec.Points[:1]
	rep, err := buildPDF(rec, pdfOpts())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Draft || rep.Title != "Coacción" || len(rep.Points) != 3 || len(rep.Calls) != 1 || len(rep.Calls[0].Turns) != 5 {
		t.Fatalf("report: draft %t title %q points %d", rep.Draft, rep.Title, len(rep.Points))
	}
	if !rep.Calls[0].Turns[3].Phrase || rep.Calls[0].Turns[0].Speaker != "Mamá" {
		t.Errorf("turns = %+v", rep.Calls[0].Turns)
	}
	all, _ := json.Marshal(rep)
	text := string(all)
	for _, want := range []string{"Sellada el", "SHA-256 de ficha.json", "Firma Ed25519", Fingerprint(rec.Seal.PublicKey),
		"https://mama.example.com/i/" + testToken + "/ficha.json", "go run ./cmd/verify-ficha", honestText, "https://www.google.com/maps/search/", "SHA-256 de la grabación", "Quién"} {
		if !strings.Contains(text, want) {
			t.Errorf("PDF lacks %q", want)
		}
	}
	for _, leak := range []string{"+584141234567", "+584241112233", testPhrase + "\""} {
		if strings.Contains(text, leak) {
			t.Errorf("PDF leaks %q", leak)
		}
	}
}

func TestPDFDraftBeforeTheSeal(t *testing.T) {
	rep, err := buildPDF(openRecord(), pdfOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Draft || rep.SealNote == "" || len(rep.Seal) != 0 || rep.Honest != honestText {
		t.Errorf("draft = %+v", rep)
	}
	b, err := renderPDF(rep)
	if err != nil || !bytes.HasPrefix(b, []byte("%PDF-1.")) || !bytes.HasSuffix(bytes.TrimSpace(b), []byte("%%EOF")) || len(b) < 20000 {
		t.Fatalf("render: %v (%d bytes)", err, len(b))
	}
}

func TestPDFListsEachRelativeAndLinkedEmergencies(t *testing.T) {
	rec := sealedRecord()
	rec.Deliveries = []Delivery{
		{Recipient: "+58 424 ···33", Status: "delivered", SentAt: at("2026-10-24T13:11:08Z"), DeliveredAt: ptr(at("2026-10-24T13:11:10Z"))},
		{Recipient: "+58 412 ···77", Status: "failed", Error: "Re-engagement message", SentAt: at("2026-10-24T13:11:08Z")},
	}
	rec.Emergencies = []Emergency{{ID: "00000000000000aa", Status: "open", Channels: []string{"sms"}, FirstReceivedAt: at("2026-10-24T13:09:50Z")}}
	rep, err := buildPDF(rec, pdfOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Family) != 2 || rep.Family[0].Status != "entregado" || rep.Family[0].Time != "09:11:10" || rep.Family[1].Error != "Re-engagement message" {
		t.Errorf("family = %+v", rep.Family)
	}
	if len(rep.Emergencies) != 1 || !strings.Contains(rep.Emergencies[0], "llegó por SMS") {
		t.Errorf("emergencies = %v", rep.Emergencies)
	}
	if _, err := renderPDF(rep); err != nil {
		t.Fatal(err)
	}
}

func TestPDFRoute(t *testing.T) {
	rec := serve(t, fakeSource{testToken: sealedRecord()}, "/i/"+testToken+"/reporte.pdf")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="ficha-mama-2026-10-24-0910.pdf"` ||
		!bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("GET reporte.pdf = %d %v", rec.Code, rec.Header())
	}
	checkPrivacyHeaders(t, rec)
	if rec := serve(t, fakeSource{}, "/i/"+testToken+"/reporte.pdf"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown token = %d", rec.Code)
	}
}

// testContext is a call's ficha (#106) with every field.
func testContext() *CallContext {
	return &CallContext{Situacion: "Parece que se la llevan en un carro contra su voluntad.", Lugar: "Autopista Caracas-La Guaira",
		Personas: "Dos hombres con ella", Vehiculo: "Carro gris", Rumbo: "Hacia La Guaira", Confianza: "alta"}
}

// sealedWithContext is sealedRecord sealed with the call's context, the way
// the closer seals once #106 stored it.
func sealedWithContext(t *testing.T) Record {
	t.Helper()
	rec := openRecordWith(sealedRecord())
	rec.Calls[0].Context = testContext()
	key := testKey()
	sealedAt := at("2026-10-24T13:11:25Z")
	doc, err := Document(rec, sealedAt, key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec.Seal = &Seal{SealedAt: sealedAt, Document: doc, Signature: Sign(doc, key), PublicKey: key.Public().(ed25519.PublicKey)}
	return rec
}

// The sealed document carries the call's context as an optional object, in
// a fixed order, and the PDF built from it keeps it: a "Contexto de la
// llamada" section after the call, with every field that has a value and
// the note that an AI extracted it. Without a ficha there is no object and
// no section, and the document is what it was before (it still verifies).
func TestSealAndPDFCarryTheCallContext(t *testing.T) {
	rec := sealedWithContext(t)
	doc := string(rec.Seal.Document)
	want := `"context": {
        "situacion": "Parece que se la llevan en un carro contra su voluntad.",
        "lugar": "Autopista Caracas-La Guaira",
        "personas": "Dos hombres con ella",
        "vehiculo": "Carro gris",
        "rumbo": "Hacia La Guaira",
        "confianza": "alta"
      }`
	if !strings.Contains(doc, want) {
		t.Errorf("ficha.json lacks the context object:\n%s", doc)
	}
	if _, err := Verify(rec.Seal.Document, rec.Seal.Signature, rec.Seal.PublicKey); err != nil {
		t.Errorf("Verify with the context: %v", err)
	}
	if _, err := Verify([]byte(strings.Replace(doc, "Carro gris", "Carro azul", 1)), rec.Seal.Signature, rec.Seal.PublicKey); !errors.Is(err, ErrSealInvalid) {
		t.Errorf("a changed context still verifies: %v", err)
	}

	// The PDF reads it from the sealed bytes, not from the live row.
	live := rec
	live.Calls = append([]Call(nil), rec.Calls...)
	live.Calls[0].Context = &CallContext{Situacion: "cambiado después del sello"}
	rep, err := buildPDF(live, pdfOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Contexts) != 1 || rep.Contexts[0].Title != "Contexto de la llamada" || len(rep.Contexts[0].Fields) != 6 ||
		rep.Contexts[0].Fields[0] != [2]string{"Situación", testContext().Situacion} || rep.Contexts[0].Fields[5] != [2]string{"Confianza", "alta"} {
		t.Errorf("sealed PDF contexts = %+v", rep.Contexts)
	}
	b, err := renderPDF(rep)
	if err != nil || len(b) < 1000 {
		t.Fatalf("render: %v (%d bytes)", err, len(b))
	}

	// A draft reads the live context, and leaves out fields with no value.
	draft := openRecordWith(sealedRecord())
	draft.Calls[0].Context = &CallContext{Situacion: "Se la llevan.", Vehiculo: "Moto"}
	if rep, _ := buildPDF(draft, pdfOpts()); len(rep.Contexts) != 1 || len(rep.Contexts[0].Fields) != 2 || rep.Contexts[0].Fields[1][0] != "Vehículo" {
		t.Errorf("draft PDF contexts = %+v", rep.Contexts)
	}

	// Without a ficha: no object, no section, and the seal verifies.
	plain := sealedRecord()
	if strings.Contains(string(plain.Seal.Document), `"context"`) {
		t.Error("a document without a ficha has a context object")
	}
	if _, err := Verify(plain.Seal.Document, plain.Seal.Signature, plain.Seal.PublicKey); err != nil {
		t.Errorf("Verify without the context: %v", err)
	}
	if rep, _ := buildPDF(plain, pdfOpts()); len(rep.Contexts) != 0 {
		t.Errorf("PDF without a ficha has contexts: %+v", rep.Contexts)
	}
}
