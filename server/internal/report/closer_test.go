package report

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

const webhookSecret = "whsec_test"

// webhookBody is the post-call webhook for the fixture's call conv_01.
const webhookBody = `{"type":"post_call_transcription","event_timestamp":1761311465,"data":{
	"agent_id":"agent_x","conversation_id":"conv_01","status":"done",
	"transcript":[
		{"role":"agent","message":"¿Aló? Mi amor, ¿dónde estás?","time_in_call_secs":0},
		{"role":"user","message":"Sí, mami. Oye, dile a la tía Carmen que llego tarde.","time_in_call_secs":15}],
	"metadata":{"start_time_unix_secs":1761311404,"call_duration_secs":61},
	"conversation_initiation_client_data":{"dynamic_variables":{"incident_id":"` + testIncidentID + `"}}}}`

// callMux is the real call webhook on pool with hook as its post-call hook.
func callMux(t *testing.T, pool *sql.DB, hook func(context.Context, call.Finished)) *http.ServeMux {
	t.Helper()
	rt := call.NewRoutes(configWithSecret(), call.NewDBStore(pool))
	rt.SetFinished(call.Hooks(hook))
	mux := http.NewServeMux()
	rt.Register(mux)
	return mux
}

func configWithSecret() config.Config {
	return config.Config{ElevenLabs: config.ElevenLabs{WebhookSecret: webhookSecret}}
}

// retryWebhookBody is the post-call webhook of the retry, conv_02.
const retryWebhookBody = `{"type":"post_call_transcription","event_timestamp":1761311530,"data":{
	"agent_id":"agent_x","conversation_id":"conv_02","status":"done",
	"transcript":[{"role":"user","message":"Mami, ya voy en el metro.","time_in_call_secs":3}],
	"metadata":{"start_time_unix_secs":1761311490,"call_duration_secs":40},
	"conversation_initiation_client_data":{"dynamic_variables":{"incident_id":"` + testIncidentID + `"}}}}`

func postWebhook(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	postWebhookBody(t, mux, webhookBody)
}

// postWebhookBody posts body to the call webhook, signed.
func postWebhookBody(t *testing.T, mux *http.ServeMux, body string) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", strings.NewReader(body))
	req.Header.Set(call.SignatureHeader, "t="+ts+",v0="+hex.EncodeToString(call.Sign(webhookSecret, ts, []byte(body))))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook = %d %s", rec.Code, rec.Body)
	}
}

func hookFor(c *Closer) func(context.Context, call.Finished) {
	return func(ctx context.Context, f call.Finished) {
		c.CallFinished(ctx, f.IncidentID, f.ConversationID, f.Status)
	}
}

// The post-call webhook stores the transcript; the report's hook then waits
// for ElevenLabs to have the audio, stores it with its SHA-256, and the page
// plays it from our server. A repeated webhook downloads nothing again.
func TestWebhookStoresTheRecordingAndThePagePlaysIt(t *testing.T) {
	pool := newDB(t)
	store := NewDBStore(pool)
	f := &fakeElevenLabs{audio: []byte("ID3 the call")}
	srv := f.server(t)
	closer := &Closer{
		Store:   store,
		Audio:   &ElevenLabsAudio{BaseURL: srv.URL, APIKey: "k", Client: srv.Client()},
		Backoff: []time.Duration{20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond},
	}
	mux := callMux(t, pool, hookFor(closer))
	go func() { // ElevenLabs finishes processing a moment after the webhook
		time.Sleep(30 * time.Millisecond)
		f.hasAudio.Store(true)
	}()
	postWebhook(t, mux)
	closer.Wait()

	if f.downloads.Load() != 1 || f.details.Load() < 2 {
		t.Fatalf("details %d downloads %d: want retries, then one download", f.details.Load(), f.downloads.Load())
	}
	rec, err := store.Load(context.Background(), testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("ID3 the call"))
	r := rec.Calls[0].Recording
	if r == nil || r.SHA256 != want || r.Size != int64(len("ID3 the call")) || r.ContentType != "audio/mpeg" {
		t.Fatalf("recording = %+v", r)
	}

	link, err := store.Ensure(context.Background(), testIncidentID, 30)
	if err != nil {
		t.Fatal(err)
	}
	page := http.NewServeMux()
	NewHandler(store, Options{}).Register(page)
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		page.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}
	body := html.UnescapeString(get("/i/" + link.Token).Body.String())
	if !strings.Contains(body, `<audio id="audio-1"`) || !strings.Contains(body, `data-player="1"`) || strings.Contains(body, "todavía no entregó la grabación") {
		t.Errorf("page has no player or no hash:\n%s", body)
	}
	if a := get("/i/" + link.Token + "/audio.mp3?llamada=1"); a.Code != http.StatusOK || a.Body.String() != "ID3 the call" {
		t.Errorf("audio = %d %q", a.Code, a.Body.String())
	}
	if a := get("/i/" + link.Token + "/audio.mp3"); a.Code != http.StatusOK || a.Body.String() != "ID3 the call" {
		t.Errorf("audio without llamada = %d %q", a.Code, a.Body.String())
	}

	postWebhook(t, mux)
	closer.Wait()
	if f.downloads.Load() != 1 {
		t.Errorf("repeated webhook downloaded again: %d downloads", f.downloads.Load())
	}
}

// ElevenLabs never gets the audio: the closer gives up after its retries and
// the page says "Audio no disponible" with the transcript.
func TestRecordingThatNeverArrives(t *testing.T) {
	pool := newDB(t)
	store := NewDBStore(pool)
	f := &fakeElevenLabs{}
	srv := f.server(t)
	closer := &Closer{Store: store, Audio: &ElevenLabsAudio{BaseURL: srv.URL, APIKey: "k", Client: srv.Client()}, Backoff: []time.Duration{time.Millisecond, time.Millisecond}}
	postWebhook(t, callMux(t, pool, hookFor(closer)))
	closer.Wait()
	if f.details.Load() != 3 || f.downloads.Load() != 0 {
		t.Errorf("details %d downloads %d, want 3 tries and no download", f.details.Load(), f.downloads.Load())
	}
	if has, err := store.HasRecording(context.Background(), "conv_01"); err != nil || has {
		t.Errorf("HasRecording = %t %v", has, err)
	}
}

func TestFailedCallsAndMissingKeyStoreNothing(t *testing.T) {
	pool := newDB(t)
	store := NewDBStore(pool)
	f := &fakeElevenLabs{audio: []byte("x")}
	f.hasAudio.Store(true)
	srv := f.server(t)
	closer := &Closer{Store: store, Audio: &ElevenLabsAudio{BaseURL: srv.URL, APIKey: "k", Client: srv.Client()}}
	closer.CallFinished(context.Background(), testIncidentID, "conv_01", "failed")
	closer.CallFinished(context.Background(), testIncidentID, "", "done")
	closer.Wait()
	if f.details.Load() != 0 {
		t.Errorf("a failed call or a missing conversation id asked ElevenLabs %d times", f.details.Load())
	}

	noKey := &Closer{Store: store}
	noKey.CallFinished(context.Background(), testIncidentID, "conv_01", "done")
	noKey.Wait()
	if has, _ := store.HasRecording(context.Background(), "conv_01"); has {
		t.Error("a closer without a fetcher stored a recording")
	}
}

func TestSaveRecordingKeepsTheFirst(t *testing.T) {
	store := NewDBStore(newDB(t))
	ctx := context.Background()
	if err := store.SaveRecording(ctx, "conv_01", "audio/mpeg", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRecording(ctx, "conv_01", "audio/mpeg", []byte("second")); err != nil {
		t.Fatal(err)
	}
	rec, _ := store.Load(ctx, testIncidentID)
	if rec.Calls[0].Recording.SHA256 != sha256.Sum256([]byte("first")) {
		t.Error("the second save replaced the first recording")
	}
	if err := store.SaveRecording(ctx, "conv_nope", "audio/mpeg", []byte("x")); err != ErrUnknownCall {
		t.Errorf("SaveRecording(unknown) = %v, want ErrUnknownCall", err)
	}
}

// ElevenLabs can send a "done" webhook for a phone that rang out, and the
// engine then places the retry (call.Engine.await). The first call's close
// must not seal while the retry rings or runs: the seal would miss the
// retry for good, and the page would end its stream mid-call. The retry's
// own webhook seals, with its transcript.
func TestTheRetrySealsNotTheCallThatRangOut(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `INSERT INTO calls (incident_id, conversation_id, status, requested_at)
		VALUES ($1, 'conv_02', 'in_progress', '2026-10-24T13:11:10Z')`, testIncidentID)
	store := NewDBStore(pool)
	closer := &Closer{
		Store: store, Signer: testKey(), VerdictWait: 50 * time.Millisecond, PollEvery: 10 * time.Millisecond,
		Now: func() time.Time { return at("2026-10-24T13:12:00Z") }, // while the retry rings
	}
	mux := callMux(t, pool, hookFor(closer))

	postWebhook(t, mux) // conv_01, the phone that rang out
	closer.Wait()
	if rec, _ := store.Load(context.Background(), testIncidentID); rec.Seal != nil {
		t.Fatal("the first call's close sealed while the retry was in progress")
	}

	postWebhookBody(t, mux, retryWebhookBody)
	closer.Wait()
	rec, err := store.Load(context.Background(), testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	calls := sealedCalls(t, rec)
	if len(calls) != 2 || calls[1].ConversationID != "conv_02" || calls[1].Status != "done" ||
		len(calls[1].Transcript) != 1 || calls[1].Transcript[0].Message != "Mami, ya voy en el metro." {
		t.Errorf("sealed calls = %+v, want the retry's with its transcript", calls)
	}
}

// The retry can also end before the first call's close gets to the seal
// (that close waits up to two minutes for a recording): the first close
// then leaves the seal to the retry's.
func TestOnlyTheNewestFinishedCallSeals(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `INSERT INTO calls (incident_id, conversation_id, status, transcript, duration_secs, requested_at, ended_at)
		VALUES ($1, 'conv_02', 'done', $2, 40, '2026-10-24T13:11:10Z', '2026-10-24T13:12:30Z')`,
		testIncidentID, `[{"role":"user","message":"Mami, ya voy en el metro.","time_in_call_secs":3}]`)
	store := NewDBStore(pool)
	closer := &Closer{Store: store, Signer: testKey(), VerdictWait: 50 * time.Millisecond, PollEvery: 10 * time.Millisecond}

	closer.Close(context.Background(), testIncidentID, "conv_01")
	if rec, _ := store.Load(context.Background(), testIncidentID); rec.Seal != nil {
		t.Fatal("the first call's close sealed after the retry had finished")
	}
	closer.Close(context.Background(), testIncidentID, "conv_02")
	rec, err := store.Load(context.Background(), testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	if calls := sealedCalls(t, rec); len(calls) != 2 || calls[1].ConversationID != "conv_02" || len(calls[1].Transcript) != 1 {
		t.Errorf("sealed calls = %+v, want the retry's with its transcript", calls)
	}
}

// The retry fails too, so no webhook comes to seal: Recover, which every
// page view and the live stream run, seals with the call that finished.
// While the retry is still pending it restarts nothing, which would spend
// that call's one recovery on a seal that cannot happen yet.
func TestAFailedRetryIsSealedByRecover(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `INSERT INTO calls (incident_id, status, requested_at) VALUES ($1, 'requested', '2026-10-24T13:26:00Z')`, testIncidentID)
	store := NewDBStore(pool)
	closer := &Closer{
		Store: store, Signer: testKey(), VerdictWait: time.Second, PollEvery: 10 * time.Millisecond,
		Now: func() time.Time { return at("2026-10-24T13:30:00Z") }, // past RecoverAfter
	}
	recoverNow := func() {
		t.Helper()
		rec, err := store.Load(context.Background(), testIncidentID)
		if err != nil {
			t.Fatal(err)
		}
		closer.Recover(rec)
		closer.Wait()
	}

	closer.Close(context.Background(), testIncidentID, "conv_01") // its webhook's close
	recoverNow()
	if sealed, err := closer.Seal(context.Background(), testIncidentID); sealed || err != nil {
		t.Errorf("Seal while the retry is pending = %t, %v", sealed, err)
	}
	if rec, _ := store.Load(context.Background(), testIncidentID); rec.Seal != nil {
		t.Fatal("sealed while the retry was pending")
	}

	exec(t, pool, `UPDATE calls SET status = 'failed', ended_at = '2026-10-24T13:27:00Z' WHERE status = 'requested'`)
	recoverNow()
	rec, err := store.Load(context.Background(), testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	calls := sealedCalls(t, rec)
	if len(calls) != 2 || calls[0].ConversationID != "conv_01" || calls[0].Status != "done" || calls[1].Status != "failed" {
		t.Errorf("sealed calls = %+v, want conv_01 done and the failed retry", calls)
	}
}

// A retry row that never got a webhook nor a failure (the server died
// between placing it and its outcome) stops blocking the seal once it is
// stuckAfter old: the report seals with the call that connected.
func TestAStuckRetryStopsBlockingTheSeal(t *testing.T) {
	pool := newDB(t)
	exec(t, pool, `INSERT INTO calls (incident_id, status, requested_at) VALUES ($1, 'in_progress', '2026-10-24T13:11:10Z')`, testIncidentID)
	store := NewDBStore(pool)
	closer := &Closer{
		Store: store, Signer: testKey(), VerdictWait: time.Second, PollEvery: 10 * time.Millisecond,
		Now: func() time.Time { return at("2026-10-24T13:30:00Z") }, // the retry is 19 min old
	}
	rec, err := store.Load(context.Background(), testIncidentID)
	if err != nil {
		t.Fatal(err)
	}
	closer.Recover(rec)
	closer.Wait()
	if rec, err = store.Load(context.Background(), testIncidentID); err != nil {
		t.Fatal(err)
	}
	if calls := sealedCalls(t, rec); len(calls) != 2 || calls[0].ConversationID != "conv_01" || calls[0].Status != "done" {
		t.Errorf("sealed calls = %+v, want conv_01 done and the stuck retry", calls)
	}
}

// sealedCalls verifies the incident's seal and returns the calls it covers.
func sealedCalls(t *testing.T, rec Record) []docCall {
	t.Helper()
	if rec.Seal == nil {
		t.Fatal("not sealed")
	}
	if _, err := Verify(rec.Seal.Document, rec.Seal.Signature, testKey().Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("seal: %v", err)
	}
	var d document
	if err := json.Unmarshal(rec.Seal.Document, &d); err != nil {
		t.Fatal(err)
	}
	return d.Calls
}
