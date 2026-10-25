package main

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

type recordNotifier struct{ got []intake.Incident }

func (r *recordNotifier) Notify(_ context.Context, inc intake.Incident) { r.got = append(r.got, inc) }

// The post-call step of #21: the detector judges the transcript, the verdict
// is stored on the call, and only duress or unclear alerts the family.
func TestVerdictHookStoresVerdictAndAlerts(t *testing.T) {
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := runVerdict
	runVerdict = func(f func()) { f() }
	t.Cleanup(func() { runVerdict = prev })

	mustExec(t, pool, `INSERT INTO devices (id, phone_number, family_numbers, duress_phrase)
		VALUES ('dev-1', '+584141234567', '{+584241112233}', 'dile a la tía Carmen que llego tarde')`)
	cases := []struct {
		incident, conversation, said string
		want                         contract.Verdict
		alerts                       bool
	}{
		{"00000000-0000-4000-8000-000000000001", "conv_a", "Sí, mami. Oye, dile a la tía Carmen que llego tarde.", contract.VerdictDuress, true},
		{"00000000-0000-4000-8000-000000000002", "conv_b", "Todo bien, mami, ya voy llegando a la casa.", contract.VerdictNoDuress, false},
	}
	notifier := &recordNotifier{}
	hook := verdictHook(config.Config{}, pool, notifier)
	for _, c := range cases {
		mustExec(t, pool, `INSERT INTO incidents (id, device_id, trigger, branch, speed_mps, triggered_at)
			VALUES ($1, 'dev-1', 'volume_pattern', 'call', 1.2, now())`, c.incident)
		mustExec(t, pool, `INSERT INTO calls (incident_id, conversation_id, status) VALUES ($1, $2, 'done')`, c.incident, c.conversation)
		hook(context.Background(), call.Finished{
			IncidentID:     c.incident,
			ConversationID: c.conversation,
			Status:         "done",
			Transcript: []contract.TranscriptTurn{
				{Role: "agent", Message: "¿Aló? Mi amor, ¿dónde estás?", TimeInCallSecs: 0},
				{Role: "user", Message: c.said, TimeInCallSecs: 4},
			},
		})
		var stored string
		if err := pool.QueryRow(`SELECT verdict FROM calls WHERE conversation_id = $1`, c.conversation).Scan(&stored); err != nil || stored != string(c.want) {
			t.Fatalf("%s: stored verdict = %q (err %v), want %s", c.conversation, stored, err, c.want)
		}
	}
	if len(notifier.got) != 1 || notifier.got[0].ID != cases[0].incident || notifier.got[0].Branch != contract.BranchCall || notifier.got[0].DeviceID != "dev-1" {
		t.Fatalf("notified = %+v, want only the duress incident", notifier.got)
	}
}

// A failed call, or one without a conversation, runs no detector and alerts
// nobody from this hook.
func TestVerdictHookSkipsFailedCalls(t *testing.T) {
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := runVerdict
	ran := false
	runVerdict = func(f func()) { ran = true; f() }
	t.Cleanup(func() { runVerdict = prev })

	notifier := &recordNotifier{}
	hook := verdictHook(config.Config{}, pool, notifier)
	hook(context.Background(), call.Finished{IncidentID: "x", ConversationID: "conv_x", Status: "failed"})
	hook(context.Background(), call.Finished{IncidentID: "x", Status: "done"})
	if ran || len(notifier.got) != 0 {
		t.Fatalf("ran = %v, notified = %+v; want nothing for failed or conversation-less calls", ran, notifier.got)
	}
	if verdictHook(config.Config{}, nil, notifier) != nil {
		t.Fatal("verdictHook without a database should be nil")
	}
}

// #106: with a safe phrase, saying it notifies nobody, and saying neither
// phrase is unclear and alerts the family.
func TestVerdictHookWithASafePhrase(t *testing.T) {
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := runVerdict
	runVerdict = func(f func()) { f() }
	t.Cleanup(func() { runVerdict = prev })

	mustExec(t, pool, `INSERT INTO devices (id, phone_number, family_numbers, duress_phrase, safe_phrase)
		VALUES ('dev-1', '+584141234567', '{+584241112233}', 'dile a la tía Carmen que llego tarde', 'ya voy llegando')`)
	cases := []struct {
		incident, conversation, said string
		want                         contract.Verdict
	}{
		{"00000000-0000-4000-8000-000000000011", "conv_safe", "Tranquila, mami, ya voy llegando.", contract.VerdictNoDuress},
		{"00000000-0000-4000-8000-000000000012", "conv_neither", "Bien, mami, aquí en el trabajo.", contract.VerdictUnclear},
		{"00000000-0000-4000-8000-000000000013", "conv_negated", "No voy llegando todavía, hay cola.", contract.VerdictUnclear},
	}
	notifier := &recordNotifier{}
	hook := verdictHook(config.Config{}, pool, notifier)
	for _, c := range cases {
		mustExec(t, pool, `INSERT INTO incidents (id, device_id, trigger, branch, triggered_at)
			VALUES ($1, 'dev-1', 'volume_pattern', 'call', now())`, c.incident)
		mustExec(t, pool, `INSERT INTO calls (incident_id, conversation_id, status) VALUES ($1, $2, 'done')`, c.incident, c.conversation)
		hook(context.Background(), call.Finished{IncidentID: c.incident, ConversationID: c.conversation, Status: "done",
			Transcript: []contract.TranscriptTurn{{Role: "agent", Message: "¿Dónde estás?"}, {Role: "user", Message: c.said}}})
		var stored string
		if err := pool.QueryRow(`SELECT verdict FROM calls WHERE conversation_id = $1`, c.conversation).Scan(&stored); err != nil || stored != string(c.want) {
			t.Fatalf("%s: stored verdict = %q (err %v), want %s", c.conversation, stored, err, c.want)
		}
	}
	if len(notifier.got) != 2 || notifier.got[0].ID != cases[1].incident || notifier.got[1].ID != cases[2].incident {
		t.Fatalf("notified = %+v, want the two unclear incidents only", notifier.got)
	}
}

func mustExec(t *testing.T, pool *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// The verdict's trace line says whether and where the phrase was heard,
// with the detector, its verdict and latency, and never the phrase, what
// was said or a phone number, even when a reason quotes the person.
func TestTraceVerdictNeverLogsThePhrase(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	phrase := "Dile a la tía Carmen que llegó tarde"
	f := call.Finished{IncidentID: "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", ConversationID: "conv_7", Status: "done",
		Transcript: []contract.TranscriptTurn{
			{Role: "agent", Message: "¿Aló? ¿Dónde estás?", TimeInCallSecs: 0},
			{Role: "user", Message: "Voy en el carro, mi número es +584141234567", TimeInCallSecs: 4},
			{Role: "user", Message: "Oye, DILE a la TIA Carmen que LLEGO tarde", TimeInCallSecs: 15.5},
		}}
	traceVerdict(f, "rules+llm", duress.Result{Verdict: contract.VerdictDuress, Reason: `rules: the user said the duress phrase; LLM: she said "dile a la tía Carmen que llegó tarde"`}, 1234*time.Millisecond, duress.Phrases{Duress: phrase, Safe: "ya voy llegando"})
	line := buf.String()
	for _, want := range []string{"trace incident=9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f step=verdict", "conversation_id=conv_7", "detector=rules+llm",
		"verdict=duress", "latency_ms=1234", "phrase_set=true", "user_turns=2", "agent_turns=1", "phrase_match_diag=true", "phrase_turn=3", "phrase_at_s=15.5"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
	low := strings.ToLower(line)
	for _, never := range []string{"carmen", "tía", "tia ", "llegó", "llego", "tarde", "dile", "584141234567", "voy en el carro"} {
		if strings.Contains(low, never) {
			t.Errorf("line carries %q: %s", never, line)
		}
	}

	buf.Reset()
	f.Transcript = f.Transcript[:2]
	traceVerdict(f, "rules", duress.Result{Verdict: contract.VerdictUnclear, Reason: "the user did not say the duress phrase"}, 3*time.Millisecond, duress.Phrases{Duress: phrase})
	if line := buf.String(); !strings.Contains(line, "phrase_match_diag=false") || strings.Contains(line, "phrase_turn=") {
		t.Errorf("no phrase: %s", line)
	}
}
