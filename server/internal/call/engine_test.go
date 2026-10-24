package call

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// sampleIncident is the incident_id of the contract's call_webhook.json.
const (
	sampleIncident = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"
	sampleConv     = "conv_01jxample0000000000000000"
	victimPhone    = "+584141234567"
	marianaVoiceID = "SnLK0h3jnPTd3J6pYcO3"
)

// callDB returns a migrated throwaway database with the sample incident on a
// device whose phone is phone.
func callDB(t *testing.T, phone string) *sql.DB {
	t.Helper()
	pool := dbtest.Fresh(t)
	ctx := context.Background()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO devices (id, phone_number, duress_phrase) VALUES ('dev-1', '` + phone + `', 'dile a la tía Carmen que llego tarde')`,
		`INSERT INTO incidents (id, device_id, trigger, branch, triggered_at) VALUES ('` + sampleIncident + `', 'dev-1', 'widget', 'call', now())`,
	} {
		if _, err := pool.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return pool
}

type callRow struct {
	ID             int64
	ConversationID sql.NullString
	CallSID        sql.NullString
	Status         string
	Transcript     []byte
	Duration       sql.NullInt64
	EndedAt        sql.NullTime
}

func callRows(t *testing.T, pool *sql.DB) []callRow {
	t.Helper()
	rows, err := pool.Query(`SELECT id, conversation_id, call_sid, status, transcript, duration_secs, ended_at FROM calls ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []callRow
	for rows.Next() {
		var r callRow
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.CallSID, &r.Status, &r.Transcript, &r.Duration, &r.EndedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// engineRoutes returns the call routes on pool, with the client pointed at
// elevenLabsURL and Mariana's voice configured.
func engineRoutes(t *testing.T, pool *sql.DB, elevenLabsURL string) (*Routes, *http.ServeMux) {
	t.Helper()
	rt := NewRoutes(config.Config{ElevenLabs: config.ElevenLabs{
		APIKey: "key", AgentID: "agent_1", PhoneNumberID: "phnum_1",
		VoiceID: marianaVoiceID, WebhookSecret: "secret",
	}}, NewDBStore(pool))
	rt.client.BaseURL = elevenLabsURL
	rt.engine.OutcomeTimeout = 10 * time.Millisecond // no webhook comes in these tests
	rt.engine.RetryDelay = 0
	mux := http.NewServeMux()
	rt.Register(mux)
	return rt, mux
}

// postSigned posts body to the webhook route, signed now with "secret".
func postSigned(mux *http.ServeMux, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/elevenlabs", bytes.NewReader(body))
	req.Header.Set(SignatureHeader, signed("secret", time.Now(), body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func sampleIncidentInput() intake.Incident {
	return intake.Incident{ID: sampleIncident, DeviceID: "dev-1", Branch: contract.BranchCall}
}

func TestStartCallInsertsTheRowBeforePlacingAndStoresTheConversation(t *testing.T) {
	pool := callDB(t, victimPhone)
	var during []callRow
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		during = callRows(t, pool) // what the table holds while ElevenLabs places the call
		_, _ = io.WriteString(w, `{"success":true,"message":"ok","conversation_id":"`+sampleConv+`","callSid":"CA1"}`)
	}))
	t.Cleanup(srv.Close)
	rt, _ := engineRoutes(t, pool, srv.URL)

	rt.Starter().StartCall(context.Background(), sampleIncidentInput())

	if len(during) != 1 || during[0].Status != StatusRequested || during[0].ConversationID.Valid {
		t.Fatalf("rows while placing = %+v, want one requested row with no conversation_id", during)
	}
	var sent outboundCall
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.ToNumber != victimPhone || sent.ClientData == nil ||
		sent.ClientData.DynamicVariables[contract.DynamicVarIncidentID] != sampleIncident ||
		sent.ClientData.ConfigOverride == nil || sent.ClientData.ConfigOverride.TTS.VoiceID != marianaVoiceID {
		t.Errorf("outbound call = %s", body)
	}
	after := callRows(t, pool)
	if len(after) != 1 || after[0].ID != during[0].ID || after[0].ConversationID.String != sampleConv ||
		after[0].CallSID.String != "CA1" || after[0].Status != StatusInProgress {
		t.Errorf("rows after placing = %+v", after)
	}
}

func TestStartCallMarksTheCallFailed(t *testing.T) {
	cases := []struct {
		name, phone, answer string
		wantRequests        int
	}{
		// Refused twice: the first call and its one retry, then the fallback.
		{"ElevenLabs refuses", victimPhone, `{"success":false,"message":"Twilio refused the number"}`, 2},
		// A retry cannot fix the number: straight to the fallback.
		{"not a +58 number", "+19005551234", `{"success":true,"conversation_id":"c","callSid":"s"}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := callDB(t, tc.phone)
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				_, _ = io.WriteString(w, tc.answer)
			}))
			t.Cleanup(srv.Close)
			rt, _ := engineRoutes(t, pool, srv.URL)

			rt.Starter().StartCall(context.Background(), sampleIncidentInput())

			if requests != tc.wantRequests {
				t.Errorf("ElevenLabs requests = %d, want %d", requests, tc.wantRequests)
			}
			rows := callRows(t, pool)
			if want := max(tc.wantRequests, 1); len(rows) != want {
				t.Errorf("rows = %+v, want %d", rows, want)
			}
			for _, r := range rows {
				if r.Status != StatusFailed || !r.EndedAt.Valid {
					t.Errorf("row %+v, want failed with ended_at", r)
				}
			}
			if p := rt.fallback.pending(); len(p) != 1 || p[0].ID != sampleIncident {
				t.Errorf("fallback pending = %+v, want the incident", p)
			}
		})
	}
}

func TestStartCallForAnUnknownIncidentStoresNothing(t *testing.T) {
	pool := callDB(t, victimPhone)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("ElevenLabs was called for an unknown incident")
	}))
	t.Cleanup(srv.Close)
	rt, _ := engineRoutes(t, pool, srv.URL)

	rt.Starter().StartCall(context.Background(), intake.Incident{ID: "00000000-0000-0000-0000-000000000000"})

	if rows := callRows(t, pool); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

// placed answers the outbound call with the sample conversation.
func placed(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"message":"ok","conversation_id":"`+sampleConv+`","callSid":"CA1"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWebhookStoresTheTranscriptOnceWhenRepeated(t *testing.T) {
	pool := callDB(t, victimPhone)
	rt, mux := engineRoutes(t, pool, placed(t).URL)
	var finished []Finished
	rt.engine.Finished = func(_ context.Context, f Finished) { finished = append(finished, f) }
	rt.Starter().StartCall(context.Background(), sampleIncidentInput())

	body := sampleWebhook(t)
	for i := range 2 {
		if rec := postSigned(mux, body); rec.Code != http.StatusOK {
			t.Fatalf("webhook %d: status %d %s", i+1, rec.Code, rec.Body)
		}
	}

	rows := callRows(t, pool)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want exactly one call", rows)
	}
	r := rows[0]
	var turns []contract.TranscriptTurn
	if err := json.Unmarshal(r.Transcript, &turns); err != nil {
		t.Fatalf("transcript %s: %v", r.Transcript, err)
	}
	if r.Status != StatusDone || r.ConversationID.String != sampleConv || r.Duration.Int64 != 42 ||
		len(turns) != 2 || turns[1].Role != "user" || !strings.Contains(turns[1].Message, "tía Carmen") {
		t.Errorf("row = %+v transcript %+v", r, turns)
	}
	// metadata.start_time_unix_secs 1761297010 + 42 s.
	if !r.EndedAt.Time.Equal(time.Unix(1761297052, 0)) {
		t.Errorf("ended_at = %v", r.EndedAt)
	}
	if len(finished) != 2 || finished[0].IncidentID != sampleIncident || finished[0].ConversationID != sampleConv ||
		finished[0].Status != StatusDone || len(finished[0].Transcript) != 2 {
		t.Errorf("Finished = %+v", finished)
	}
}

func TestWebhookThatBeatsThePlacedResponseUpdatesTheRequestedRow(t *testing.T) {
	pool := callDB(t, victimPhone)
	_, mux := engineRoutes(t, pool, "")
	store := NewDBStore(pool)
	ctx := context.Background()

	callID, _, err := store.Begin(ctx, sampleIncident)
	if err != nil {
		t.Fatal(err)
	}
	if rec := postSigned(mux, sampleWebhook(t)); rec.Code != http.StatusOK {
		t.Fatalf("webhook: status %d", rec.Code)
	}
	// The outbound-call response lands after the webhook.
	if err := store.Placed(ctx, callID, Placed{ConversationID: sampleConv, CallSID: "CA1"}); err != nil {
		t.Fatal(err)
	}

	rows := callRows(t, pool)
	if len(rows) != 1 || rows[0].ID != callID || rows[0].ConversationID.String != sampleConv ||
		rows[0].Status != StatusDone || rows[0].CallSID.String != "CA1" || len(rows[0].Transcript) == 0 {
		t.Errorf("rows = %+v, want the requested row finished", rows)
	}
}

func TestWebhookWithNoCallRowInsertsOne(t *testing.T) {
	pool := callDB(t, victimPhone)
	_, mux := engineRoutes(t, pool, "")
	for range 2 {
		if rec := postSigned(mux, sampleWebhook(t)); rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	if rows := callRows(t, pool); len(rows) != 1 || rows[0].ConversationID.String != sampleConv || rows[0].Status != StatusDone {
		t.Errorf("rows = %+v, want one inserted call", rows)
	}
}

func TestWebhookForAnUnknownIncidentIsDropped(t *testing.T) {
	pool := callDB(t, victimPhone)
	_, mux := engineRoutes(t, pool, "")
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		body := []byte(strings.Replace(string(sampleWebhook(t)), sampleIncident, id, 1))
		if rec := postSigned(mux, body); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200 so ElevenLabs does not retry", id, rec.Code)
		}
	}
	if rows := callRows(t, pool); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

func TestInitiationFailureMarksTheCallFailed(t *testing.T) {
	pool := callDB(t, victimPhone)
	rt, mux := engineRoutes(t, pool, placed(t).URL)
	rt.Starter().StartCall(context.Background(), sampleIncidentInput())

	body := []byte(`{"type":"call_initiation_failure","data":{"conversation_id":"` + sampleConv + `","failure_reason":"busy"}}`)
	if rec := postSigned(mux, body); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rows := callRows(t, pool); len(rows) != 1 || rows[0].Status != StatusFailed {
		t.Errorf("rows = %+v, want the call failed", rows)
	}
}

// TestAlertToOutboundCall posts a call-branch alert through intake and times
// it until the outbound-call request reaches ElevenLabs. Ringing follows that
// request by 2.3–6.2 s (spike #11), inside the 15 s budget.
func TestAlertToOutboundCall(t *testing.T) {
	pool := callDB(t, victimPhone)
	got := make(chan time.Time, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got <- time.Now()
		_, _ = io.WriteString(w, `{"success":true,"message":"ok","conversation_id":"conv_alert","callSid":"CA1"}`)
	}))
	t.Cleanup(srv.Close)
	rt, mux := engineRoutes(t, pool, srv.URL)
	intake.New(pool, rt.Starter(), intake.LogHandoff{}).Register(mux)

	alert := `{"incident_id":"0b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f","device_id":"dev-1","trigger":"widget",` +
		`"triggered_at":"2026-10-24T09:10:00Z","points":[{"lat":10.4961,"lng":-66.8531,"speed_mps":2,"recorded_at":"2026-10-24T09:10:00Z"}],"has_audio":false}`
	start := time.Now()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/incidents", strings.NewReader(alert)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /incidents = %d %s", rec.Code, rec.Body)
	}
	select {
	case at := <-got:
		t.Logf("alert to outbound-call request: %v", at.Sub(start))
	case <-time.After(placeTimeout):
		t.Fatal("no outbound call within 15 s of the alert")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := callRows(t, pool)
		if len(rows) == 1 && rows[0].ConversationID.String == "conv_alert" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rows = %+v, want the placed call stored", rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// failingStore fails every write, for the webhook's retry path.
type failingStore struct{ Store }

func (failingStore) SaveTranscript(context.Context, contract.CallWebhook) error {
	return errors.New("database down")
}

func TestWebhookAnswers500WhenStoringFails(t *testing.T) {
	e := &Engine{Store: failingStore{}}
	_, post := signedWebhook(e.Received)
	if rec := post(sampleWebhook(t)); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 so ElevenLabs retries", rec.Code)
	}
}

func TestStarterWithoutADatabaseOnlyLogs(t *testing.T) {
	if _, ok := NewRoutes(config.Config{}, nil).Starter().(intake.LogHandoff); !ok {
		t.Error("Starter without a store is not intake.LogHandoff")
	}
}

func TestConcurrentCopiesOfTheWebhookLeaveOneRow(t *testing.T) {
	for _, withRow := range []bool{false, true} {
		pool := callDB(t, victimPhone)
		_, mux := engineRoutes(t, pool, "")
		if withRow {
			if _, _, err := NewDBStore(pool).Begin(context.Background(), sampleIncident); err != nil {
				t.Fatal(err)
			}
		}
		body := sampleWebhook(t)
		codes := make(chan int, 5)
		for range cap(codes) {
			go func() { codes <- postSigned(mux, body).Code }()
		}
		for range cap(codes) {
			if code := <-codes; code != http.StatusOK {
				t.Errorf("with requested row %v: status %d", withRow, code)
			}
		}
		if rows := callRows(t, pool); len(rows) != 1 || rows[0].Status != StatusDone {
			t.Errorf("with requested row %v: rows = %+v, want one done call", withRow, rows)
		}
	}
}

func TestInitiationFailureThatBeatsThePlacedResponseStaysFailed(t *testing.T) {
	pool := callDB(t, victimPhone)
	_, mux := engineRoutes(t, pool, "")
	store := NewDBStore(pool)
	ctx := context.Background()
	callID, _, err := store.Begin(ctx, sampleIncident)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"type":"call_initiation_failure","data":{"conversation_id":"` + sampleConv + `","failure_reason":"busy",` +
		`"conversation_initiation_client_data":{"dynamic_variables":{"incident_id":"` + sampleIncident + `"}}}}`)
	if rec := postSigned(mux, body); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if err := store.Placed(ctx, callID, Placed{ConversationID: sampleConv, CallSID: "CA1"}); err != nil {
		t.Fatal(err)
	}
	if rows := callRows(t, pool); len(rows) != 1 || rows[0].Status != StatusFailed || rows[0].ConversationID.String != sampleConv {
		t.Errorf("rows = %+v, want the requested row failed", rows)
	}
}

func TestWebhookWithoutAConversationIDIsDropped(t *testing.T) {
	pool := callDB(t, victimPhone)
	_, mux := engineRoutes(t, pool, "")
	if _, _, err := NewDBStore(pool).Begin(context.Background(), sampleIncident); err != nil {
		t.Fatal(err)
	}
	body := []byte(strings.Replace(string(sampleWebhook(t)), sampleConv, "", 1))
	if rec := postSigned(mux, body); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rows := callRows(t, pool); len(rows) != 1 || rows[0].Status != StatusRequested || rows[0].ConversationID.Valid {
		t.Errorf("rows = %+v, want the requested row untouched", rows)
	}
}
