package report

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

func postWebhook(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", strings.NewReader(webhookBody))
	req.Header.Set(call.SignatureHeader, "t="+ts+",v0="+hex.EncodeToString(call.Sign(webhookSecret, ts, []byte(webhookBody))))
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
