package report

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
)

func TestDocumentHoldsTheRecordAndNoSecrets(t *testing.T) {
	rec := sealedRecord()
	doc := rec.Seal.Document
	var d map[string]any
	if err := json.Unmarshal(doc, &d); err != nil {
		t.Fatal(err)
	}
	if d["format"] != SignatureContext || d["sealed_at"] != "2026-10-24T13:11:25Z" {
		t.Errorf("format %v sealed_at %v", d["format"], d["sealed_at"])
	}
	s := string(doc)
	sum := rec.Calls[0].Recording.SHA256
	for _, want := range []string{
		`"incident_id": "` + testIncidentID + `"`, `"triggered_at": "2026-10-24T13:10:00.253Z"`, `"received_at": "2026-10-24T13:10:00.653Z"`,
		`"notified_at": "2026-10-24T13:11:08Z"`, `"family_numbers": 2`, `"verdict": "duress"`, `"verdict_at": "2026-10-24T13:11:07Z"`,
		`"sha256": "` + hex.EncodeToString(sum[:]) + `"`, `"time_in_call_secs": 15`, "dile a la tía Carmen", `"accuracy_m": 12.5`,
		`"clocks"`, `"server_ed25519_public_key": "`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("document lacks %s", want)
		}
	}
	// Phone numbers stay out; the phrase is only where the person said it.
	for _, leak := range []string{"+584141234567", "+584241112233", "duress_phrase"} {
		if strings.Contains(s, leak) {
			t.Errorf("document leaks %q", leak)
		}
	}
	again, err := Document(openRecordWith(rec), rec.Seal.SealedAt, rec.Seal.PublicKey)
	if err != nil || !bytes.Equal(again, doc) {
		t.Error("Document is not deterministic")
	}
}

// openRecordWith is rec without its seal (the state Document reads).
func openRecordWith(rec Record) Record {
	rec.Seal = nil
	return rec
}

func TestSignAndVerify(t *testing.T) {
	rec := sealedRecord()
	s := rec.Seal
	got, err := Verify(s.Document, s.Signature, s.PublicKey)
	if err != nil || got.IncidentID != testIncidentID || got.Calls != 1 || got.SealedAt != "2026-10-24T13:11:25Z" {
		t.Fatalf("Verify = %+v, %v", got, err)
	}
	changed := bytes.Replace(s.Document, []byte(`"duress"`), []byte(`"unclear"`), 1)
	if _, err := Verify(changed, s.Signature, s.PublicKey); !errors.Is(err, ErrSealInvalid) {
		t.Errorf("changed verdict: %v, want ErrSealInvalid", err)
	}
	other := ed25519.NewKeyFromSeed(make([]byte, 32)).Public().(ed25519.PublicKey)
	if _, err := Verify(s.Document, s.Signature, other); !errors.Is(err, ErrSealInvalid) {
		t.Errorf("other key: %v", err)
	}
	sig, err := ParseSignature(EncodeSignature(s.Signature))
	if err != nil || !bytes.Equal(sig, s.Signature) {
		t.Errorf("ParseSignature(EncodeSignature) = %v", err)
	}
	if _, err := ParsePublicKey("AAAA"); !errors.Is(err, ErrBadKey) {
		t.Errorf("ParsePublicKey(short) = %v", err)
	}
}

// The signature covers the context too: it is not valid over the bare
// document, nor under another MAMÁ context (ADR 0001's offline payloads),
// so one signed thing never passes for another.
func TestSignatureIsDomainSeparated(t *testing.T) {
	key := testKey()
	pub := key.Public().(ed25519.PublicKey)
	doc := sealedRecord().Seal.Document
	sig := Sign(doc, key)
	if ed25519.Verify(pub, doc, sig) {
		t.Error("the seal verifies over the bare document")
	}
	if ed25519.Verify(pub, append([]byte("MAMA-offline-emergency-v1"), doc...), sig) {
		t.Error("the seal verifies under the offline payload context")
	}
	offline := ed25519.Sign(key, append([]byte("MAMA-offline-emergency-v1"), doc...))
	if _, err := Verify(doc, offline, pub); !errors.Is(err, ErrSealInvalid) {
		t.Errorf("an offline-context signature passes as a seal: %v", err)
	}
	if _, err := Verify([]byte(`{"format":"something else"}`), Sign([]byte(`{"format":"something else"}`), key), pub); !errors.Is(err, ErrNotAReport) {
		t.Errorf("a signed non-report: %v, want ErrNotAReport", err)
	}
}

// End to end on Postgres: the post-call webhook runs the hooks in order, a
// stand-in for #21 stores the verdict and claims the family message, then
// the report stores the recording and seals. ficha.json is served byte for
// byte and verifies; the page says "Cerrado y sellado".
func TestWebhookSealsTheReportWithVerdictAndRecording(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `UPDATE calls SET verdict = NULL, verdict_reason = NULL, verdict_at = NULL`)
	exec(t, pool, `UPDATE incidents SET notified_at = NULL`)
	store := NewDBStore(pool)
	f := &fakeElevenLabs{audio: []byte("ID3 the call")}
	f.hasAudio.Store(true)
	srv := f.server(t)
	closer := &Closer{
		Store: store, Signer: testKey(),
		Audio:       &ElevenLabsAudio{BaseURL: srv.URL, APIKey: "k", Client: srv.Client()},
		VerdictWait: 5 * time.Second, PollEvery: 10 * time.Millisecond,
	}
	var detector sync.WaitGroup
	verdict21 := func(ctx context.Context, fin call.Finished) {
		detector.Add(1)
		go func() { // #21 runs the detector in its own goroutine
			defer detector.Done()
			time.Sleep(50 * time.Millisecond)
			if err := db.SaveVerdict(context.Background(), pool, fin.ConversationID, contract.VerdictDuress, "the user said the duress phrase"); err != nil {
				t.Error(err)
			}
			exec(t, pool, `UPDATE incidents SET notified_at = now() WHERE id = $1 AND notified_at IS NULL`, fin.IncidentID)
		}()
	}
	rt := call.NewRoutes(configWithSecret(), call.NewDBStore(pool))
	rt.SetFinished(call.Hooks(verdict21, hookFor(closer)))
	mux := http.NewServeMux()
	rt.Register(mux)
	postWebhook(t, mux)
	closer.Wait()
	detector.Wait()

	rec, err := store.Load(context.Background(), testIncidentID)
	if err != nil || rec.Seal == nil {
		t.Fatalf("not sealed: %v", err)
	}
	sealed, err := Verify(rec.Seal.Document, rec.Seal.Signature, testKey().Public().(ed25519.PublicKey))
	if err != nil || sealed.IncidentID != testIncidentID {
		t.Fatalf("Verify = %+v, %v", sealed, err)
	}
	sum := sha256.Sum256([]byte("ID3 the call"))
	for _, want := range []string{`"verdict": "duress"`, `"notified_at": "`, `"sha256": "` + hex.EncodeToString(sum[:]) + `"`} {
		if !strings.Contains(string(rec.Seal.Document), want) {
			t.Errorf("sealed document lacks %s", want)
		}
	}
	if !strings.Contains(string(rec.Seal.Document), `"sealed_at": "`+stamp(rec.Seal.SealedAt)+`"`) {
		t.Errorf("stored sealed_at %s differs from the document's", stamp(rec.Seal.SealedAt))
	}

	link, _ := store.Ensure(context.Background(), testIncidentID, 30)
	page := http.NewServeMux()
	NewHandler(store, Options{CanSeal: true}).Register(page)
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		page.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}
	if j := get("/i/" + link.Token + "/ficha.json"); j.Code != http.StatusOK || !bytes.Equal(j.Body.Bytes(), rec.Seal.Document) ||
		j.Header().Get("Content-Disposition") != `attachment; filename="ficha.json"` || j.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("ficha.json = %d %q, not the stored bytes", j.Code, j.Header())
	}
	sigResp := get("/i/" + link.Token + "/ficha.sig")
	sig, err := ParseSignature(sigResp.Body.Bytes())
	if sigResp.Code != http.StatusOK || err != nil || !bytes.Equal(sig, rec.Seal.Signature) {
		t.Errorf("ficha.sig = %d %q (%v)", sigResp.Code, sigResp.Body.String(), err)
	}
	body := html.UnescapeString(get("/i/" + link.Token).Body.String())
	for _, want := range []string{"Cerrado y sellado", "mama_lista.webp", "Ficha sellada", hex.EncodeToString(sum[:]), Fingerprint(testKey().Public().(ed25519.PublicKey))} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}

	// Sealed once: a repeated webhook changes nothing.
	before := rec.Seal.Document
	postWebhook(t, mux)
	closer.Wait()
	detector.Wait()
	again, _ := store.Load(context.Background(), testIncidentID)
	if !bytes.Equal(again.Seal.Document, before) {
		t.Error("a repeated webhook resealed the report")
	}
}

// Without a verdict in time (today, before #21), the seal waits VerdictWait
// and seals what there is.
func TestSealsWithoutAVerdictAfterTheWait(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `UPDATE calls SET verdict = NULL, verdict_reason = NULL, verdict_at = NULL`)
	store := NewDBStore(pool)
	closer := &Closer{Store: store, Signer: testKey(), VerdictWait: 100 * time.Millisecond, PollEvery: 10 * time.Millisecond}
	start := time.Now()
	closer.Close(context.Background(), testIncidentID, "conv_01")
	if time.Since(start) < 100*time.Millisecond {
		t.Error("sealed before the verdict wait")
	}
	rec, _ := store.Load(context.Background(), testIncidentID)
	if rec.Seal == nil || strings.Contains(string(rec.Seal.Document), `"verdict"`) || strings.Contains(string(rec.Seal.Document), `"recording"`) {
		t.Fatalf("seal = %v", rec.Seal)
	}
}

func TestOnlyCallIncidentsAreSealed(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `UPDATE incidents SET branch = 'immediate_alert'`)
	closer := &Closer{Store: NewDBStore(pool), Signer: testKey()}
	if sealed, err := closer.Seal(context.Background(), testIncidentID); sealed || err != nil {
		t.Errorf("Seal(immediate_alert) = %t, %v", sealed, err)
	}
	if sealed, err := (&Closer{Store: NewDBStore(pool)}).Seal(context.Background(), testIncidentID); sealed || err == nil {
		t.Errorf("Seal without a key = %t, %v", sealed, err)
	}
}

// A restart lost the close: the first view after RecoverAfter restarts it,
// once.
func TestPageViewRecoversALostSeal(t *testing.T) {
	pool := newDB(t)
	store := NewDBStore(pool)
	closer := &Closer{Store: store, Signer: testKey(), VerdictWait: time.Second, PollEvery: 10 * time.Millisecond}
	link, _ := store.Ensure(context.Background(), testIncidentID, 30)
	mux := http.NewServeMux()
	NewHandler(store, Options{CanSeal: true}).RecoverWith(closer).Register(mux)
	view := func() {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/i/"+link.Token, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("view = %d", rr.Code)
		}
	}

	closer.Now = func() time.Time { return at("2026-10-24T13:12:00Z") } // 55 s after the call ended
	view()
	closer.Wait()
	if rec, _ := store.Load(context.Background(), testIncidentID); rec.Seal != nil {
		t.Fatal("recovered before RecoverAfter")
	}
	closer.Now = func() time.Time { return at("2026-10-24T13:30:00Z") }
	view()
	closer.Wait()
	rec, _ := store.Load(context.Background(), testIncidentID)
	if rec.Seal == nil {
		t.Fatal("a view after RecoverAfter did not seal")
	}
	if _, err := Verify(rec.Seal.Document, rec.Seal.Signature, testKey().Public().(ed25519.PublicKey)); err != nil {
		t.Errorf("recovered seal: %v", err)
	}
}

func TestSealFilesAre404UntilSealed(t *testing.T) {
	src := fakeSource{testToken: openRecord()}
	for _, path := range []string{"/i/" + testToken + "/ficha.json", "/i/" + testToken + "/ficha.sig", "/i/BBBBBBBBBBBBBBBBBBBBBB/ficha.json"} {
		if rec := serve(t, src, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}
