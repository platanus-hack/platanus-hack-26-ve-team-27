package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
)

// sealed writes a sealed ficha.json and ficha.sig to a temp dir and returns
// their paths and the key in Base64, as the page shows it. The call has no
// context (#106): the shape of every report sealed before it existed.
func sealed(t *testing.T) (docPath, sigPath, key string, doc []byte) {
	t.Helper()
	return sealedWith(t, nil)
}

// sealedWith is sealed with the call's context, when ctx is not nil.
func sealedWith(t *testing.T, ctx *report.CallContext) (docPath, sigPath, key string, doc []byte) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32))
	pub := priv.Public().(ed25519.PublicKey)
	ended := time.Date(2026, 10, 24, 13, 11, 5, 0, time.UTC)
	d := 61
	rec := report.Record{
		Incident: report.Incident{
			ID: "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", DeviceID: "dev", Trigger: contract.TriggerVolumePattern,
			Branch: contract.BranchCall, TriggeredAt: ended.Add(-65 * time.Second), CreatedAt: ended.Add(-64 * time.Second),
		},
		Calls: []report.Call{{ConversationID: "conv_1", Status: "done", RequestedAt: ended.Add(-63 * time.Second), EndedAt: &ended, DurationSecs: &d,
			Transcript: []contract.TranscriptTurn{{Role: "user", Message: "dile a la tía Carmen que llego tarde", TimeInCallSecs: 15}},
			Context:    ctx}},
	}
	doc, err := report.Document(rec, ended.Add(20*time.Second), pub)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	docPath, sigPath = filepath.Join(dir, "ficha.json"), filepath.Join(dir, "ficha.sig")
	write(t, docPath, doc)
	write(t, sigPath, report.EncodeSignature(report.Sign(doc, priv)))
	return docPath, sigPath, base64.StdEncoding.EncodeToString(pub), doc
}

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func verify(args ...string) (int, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestAcceptsASealedReport(t *testing.T) {
	docPath, sigPath, key, _ := sealed(t)
	code, out := verify(docPath, sigPath, key)
	if code != 0 || !strings.HasPrefix(out, "firma válida\n") || !strings.Contains(out, "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f") || !strings.Contains(out, "No es una firma electrónica certificada") {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
}

// Changing any one byte of ficha.json breaks the seal: here, every byte in
// turn (a digit of a time, a letter of the transcript, a brace).
func TestRejectsTheSameReportWithOneByteChanged(t *testing.T) {
	docPath, sigPath, key, doc := sealed(t)
	for i := range doc {
		changed := bytes.Clone(doc)
		changed[i] ^= 0x01
		write(t, docPath, changed)
		code, out := verify(docPath, sigPath, key)
		if code != 1 || !strings.HasPrefix(out, "firma NO válida: la firma no corresponde") {
			t.Fatalf("byte %d changed (%q -> %q): code %d, output %q", i, doc[i], changed[i], code, out)
		}
	}
	// A trailing newline is a change too.
	write(t, docPath, append(bytes.Clone(doc), '\n'))
	if code, _ := verify(docPath, sigPath, key); code != 1 {
		t.Errorf("trailing newline: code %d, want 1", code)
	}
}

func TestRejectsAnotherKeyAndBadInputs(t *testing.T) {
	docPath, sigPath, key, _ := sealed(t)
	other := base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, 32)).Public().(ed25519.PublicKey))
	if code, out := verify(docPath, sigPath, other); code != 1 || !strings.Contains(out, "otra llave") {
		t.Errorf("other key: %d %q", code, out)
	}
	if code, out := verify(docPath, sigPath, "c2hvcnQ="); code != 1 || !strings.Contains(out, "32 bytes") {
		t.Errorf("short key: %d %q", code, out)
	}
	write(t, sigPath, []byte("AAAA\n"))
	if code, out := verify(docPath, sigPath, key); code != 1 || !strings.Contains(out, "64 bytes") {
		t.Errorf("short signature: %d %q", code, out)
	}
	if code, _ := verify(docPath, sigPath); code != 2 {
		t.Errorf("two args: code %d, want 2", code)
	}
	if code, _ := verify(filepath.Join(t.TempDir(), "nope.json"), sigPath, key); code != 2 {
		t.Errorf("missing file: code %d, want 2", code)
	}
}

// The raw 64-byte signature works as well as its Base64.
func TestAcceptsARawSignature(t *testing.T) {
	docPath, sigPath, key, _ := sealed(t)
	b64, _ := os.ReadFile(sigPath)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b64)))
	if err != nil {
		t.Fatal(err)
	}
	write(t, sigPath, raw)
	if code, out := verify(docPath, sigPath, key); code != 0 {
		t.Errorf("raw signature: %d %q", code, out)
	}
}

// A report sealed with the call's context (#106, calls[].context) verifies,
// and so does one sealed without it, as every report before it was; a
// one-byte change inside the context breaks the seal like any other.
func TestAcceptsReportsWithAndWithoutTheCallContext(t *testing.T) {
	ctx := &report.CallContext{Situacion: "Parece que se la llevan en un carro.", Lugar: "Autopista", Vehiculo: "Carro gris", Confianza: "alta"}
	for name, c := range map[string]*report.CallContext{"without": nil, "with": ctx} {
		docPath, sigPath, key, doc := sealedWith(t, c)
		if has := bytes.Contains(doc, []byte(`"context"`)); has != (c != nil) {
			t.Fatalf("%s: context in the document = %t", name, has)
		}
		if code, out := verify(docPath, sigPath, key); code != 0 || !strings.HasPrefix(out, "firma válida\n") {
			t.Fatalf("%s: code %d, output:\n%s", name, code, out)
		}
		if c == nil {
			continue
		}
		i := bytes.Index(doc, []byte("Carro gris"))
		changed := bytes.Clone(doc)
		changed[i] = 'B'
		write(t, docPath, changed)
		if code, out := verify(docPath, sigPath, key); code != 1 || !strings.HasPrefix(out, "firma NO válida") {
			t.Errorf("context changed: code %d, output %q", code, out)
		}
	}
}
