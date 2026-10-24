package report

import (
	"bytes"
	"encoding/json"
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
