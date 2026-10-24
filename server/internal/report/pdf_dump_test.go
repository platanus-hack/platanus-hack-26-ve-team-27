package report

import (
	"os"
	"testing"
)

// TestDumpPDF writes sample PDFs when PDF_DUMP_DIR is set (manual check only).
func TestDumpPDF(t *testing.T) {
	dir := os.Getenv("PDF_DUMP_DIR")
	if dir == "" {
		t.Skip("PDF_DUMP_DIR not set")
	}
	for name, rec := range map[string]Record{"sealed": sealedRecord(), "draft": openRecord()} {
		rep, err := buildPDF(rec, PDFOptions{Token: testToken, BaseURL: "https://mama.example.com", Channel: "WhatsApp", Now: at("2026-10-24T13:20:00Z")})
		if err != nil {
			t.Fatal(err)
		}
		b, err := renderPDF(rep)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+name+".pdf", b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
