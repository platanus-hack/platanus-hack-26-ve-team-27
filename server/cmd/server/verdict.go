package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/trace"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/verdict"
)

// verdictTimeout bounds the whole post-call step (load, detector, store,
// notify). The detector's own LLM timeout (duress.DefaultLLMTimeout) is
// shorter, so a slow LLM falls back to the rules well inside it.
const verdictTimeout = 45 * time.Second

// runVerdict runs the post-call step off the webhook request, so a slow LLM
// never holds ElevenLabs' webhook. Tests replace it to run inline.
var runVerdict = func(f func()) { go f() }

// verdictHook is the post-call step of #21, wired first in call.Hooks so the
// report closes after it: judge the transcript with the duress detector,
// store the verdict on the call, then alert the family on duress or unclear.
// A no_duress call alerts nobody. nil without a database.
func verdictHook(cfg config.Config, pool *sql.DB, notifier intake.Notifier) func(context.Context, call.Finished) {
	if pool == nil || notifier == nil {
		return nil
	}
	det := detector(cfg)
	name := detectorName(det)
	store := verdict.DBStore{Pool: pool}
	return func(ctx context.Context, f call.Finished) {
		if f.Status != "done" || f.ConversationID == "" {
			trace.Log(f.IncidentID, "verdict", "skipped", true, "reason", "call_not_done", "call_status", f.Status, "conversation_id", f.ConversationID)
			return
		}
		runVerdict(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), verdictTimeout)
			defer cancel()
			inc, phrase, err := loadVerdictIncident(ctx, pool, f.IncidentID)
			if err != nil {
				log.Printf("verdict: incident %s: load: %v", f.IncidentID, err)
				trace.Log(f.IncidentID, "verdict", "level", "ERROR", "error", err, "conversation_id", f.ConversationID)
				return
			}
			c := verdict.Call{ConversationID: f.ConversationID, Transcript: f.Transcript, DuressPhrase: phrase}
			timed := &timedDetector{Detector: det}
			r, err := verdict.Decide(ctx, timed, store, c, func(ctx context.Context, _ verdict.Call, r duress.Result) {
				traceVerdict(f, name, r, timed.took, phrase)
				if r.Verdict == contract.VerdictNoDuress {
					return
				}
				notifier.Notify(ctx, inc)
			})
			if err != nil {
				log.Printf("verdict: incident %s: %v", f.IncidentID, err)
			}
			log.Printf("verdict: incident %s conversation %s: %s (%s)", f.IncidentID, f.ConversationID, r.Verdict, r.Reason)
		})
	}
}

// timedDetector measures how long the detector took, for the trace.
type timedDetector struct {
	duress.Detector
	took time.Duration
}

func (d *timedDetector) Detect(ctx context.Context, transcript []contract.TranscriptTurn, phrase string) (duress.Result, error) {
	start := time.Now()
	r, err := d.Detector.Detect(ctx, transcript, phrase)
	d.took = time.Since(start)
	return r, err
}

// detectorName names the detector in the trace.
func detectorName(d duress.Detector) string {
	if _, ok := d.(duress.Chain); ok {
		return "rules+llm"
	}
	return "rules"
}

// traceVerdict writes the verdict step: the detector, its verdict and
// reason (with the phrase's words scrubbed), how long it took, and whether
// a user turn holds the phrase, by the trace's display-only matcher
// (phrase_match_diag), with that turn's time in the call. Never the phrase
// or what was said.
func traceVerdict(f call.Finished, detector string, r duress.Result, took time.Duration, phrase string) {
	user, agent := trace.Turns(f.Transcript)
	kv := []any{"conversation_id", f.ConversationID, "detector", detector, "verdict", string(r.Verdict),
		"reason", trace.Scrub(r.Reason, phrase), "latency_ms", took, "phrase_set", phrase != "",
		"user_turns", user, "agent_turns", agent}
	turns := trace.PhraseTurns(f.Transcript, phrase)
	kv = append(kv, "phrase_match_diag", len(turns) > 0)
	if len(turns) > 0 {
		t := f.Transcript[turns[0]]
		kv = append(kv, "phrase_turn", turns[0]+1, "phrase_at_s", t.TimeInCallSecs, "phrase_turns", len(turns))
	}
	trace.Log(f.IncidentID, "verdict", kv...)
}

// detector is the production duress detector: rules, then the LLM on the
// calls the rules mark unclear when ANTHROPIC_API_KEY is set; rules alone
// otherwise.
func detector(cfg config.Config) duress.Detector {
	if cfg.Anthropic.APIKey == "" {
		log.Print("verdict: ANTHROPIC_API_KEY is not set; the duress detector runs on rules only")
		return duress.Rules{}
	}
	return duress.Chain{LLM: duress.LLM{APIKey: cfg.Anthropic.APIKey, Model: cfg.Anthropic.DuressModel}}
}

// loadVerdictIncident reads what the notifier needs about the incident and
// the device's duress phrase. The notifier reads the newest fix and the
// verdict itself.
func loadVerdictIncident(ctx context.Context, pool *sql.DB, incidentID string) (intake.Incident, string, error) {
	inc := intake.Incident{ID: incidentID}
	var branch, phrase string
	var speed sql.NullFloat64
	err := pool.QueryRowContext(ctx, `
		SELECT i.device_id, i.branch, i.speed_mps, i.triggered_at, d.duress_phrase
		FROM incidents i JOIN devices d ON d.id = i.device_id
		WHERE i.id = $1`, incidentID).
		Scan(&inc.DeviceID, &branch, &speed, &inc.TriggeredAt, &phrase)
	if errors.Is(err, sql.ErrNoRows) {
		return inc, "", errors.New("no such incident")
	}
	if err != nil {
		return inc, "", err
	}
	inc.Branch = contract.Branch(branch)
	if speed.Valid {
		s := speed.Float64
		inc.SpeedMPS = &s
	}
	return inc, phrase, nil
}
