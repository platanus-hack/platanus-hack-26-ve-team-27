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
// report closes after it: judge the transcript with the duress detector
// against both phrases, take the call's context on duress or unclear (#106),
// store both on the call, then alert the family on duress or unclear. A
// no_duress call (the safe phrase) alerts nobody. nil without a database.
func verdictHook(cfg config.Config, pool *sql.DB, notifier intake.Notifier) func(context.Context, call.Finished) {
	if pool == nil || notifier == nil {
		return nil
	}
	decider := verdict.Decider{Detector: detector(cfg), Extractor: extractor(cfg), Store: verdict.DBStore{Pool: pool}}
	return func(ctx context.Context, f call.Finished) {
		if f.Status != "done" || f.ConversationID == "" {
			return
		}
		runVerdict(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), verdictTimeout)
			defer cancel()
			inc, phrases, err := loadVerdictIncident(ctx, pool, f.IncidentID)
			if err != nil {
				log.Printf("verdict: incident %s: load: %v", f.IncidentID, err)
				return
			}
			c := verdict.Call{ConversationID: f.ConversationID, Transcript: f.Transcript,
				DuressPhrase: phrases.Duress, SafePhrase: phrases.Safe}
			r, err := decider.Decide(ctx, c, func(ctx context.Context, _ verdict.Call, r verdict.Decision) {
				if r.Verdict == contract.VerdictNoDuress {
					return
				}
				notifier.Notify(ctx, inc)
			})
			if err != nil {
				log.Printf("verdict: incident %s: %v", f.IncidentID, err)
			}
			log.Printf("verdict: incident %s conversation %s: %s (%s), call context: %v",
				f.IncidentID, f.ConversationID, r.Verdict, r.Reason, r.Ficha != nil)
		})
	}
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

// extractor takes the call's context with the LLM when ANTHROPIC_API_KEY
// is set; nil (no context) otherwise.
func extractor(cfg config.Config) duress.Extractor {
	if cfg.Anthropic.APIKey == "" {
		return nil
	}
	return duress.LLM{APIKey: cfg.Anthropic.APIKey, Model: cfg.Anthropic.DuressModel}
}

// loadVerdictIncident reads what the notifier needs about the incident and
// the device's two phrases. The notifier reads the newest fix, the verdict
// and the call's context itself.
func loadVerdictIncident(ctx context.Context, pool *sql.DB, incidentID string) (intake.Incident, duress.Phrases, error) {
	inc := intake.Incident{ID: incidentID}
	var branch string
	var phrases duress.Phrases
	var speed sql.NullFloat64
	err := pool.QueryRowContext(ctx, `
		SELECT i.device_id, i.branch, i.speed_mps, i.triggered_at, d.duress_phrase, COALESCE(d.safe_phrase, '')
		FROM incidents i JOIN devices d ON d.id = i.device_id
		WHERE i.id = $1`, incidentID).
		Scan(&inc.DeviceID, &branch, &speed, &inc.TriggeredAt, &phrases.Duress, &phrases.Safe)
	if errors.Is(err, sql.ErrNoRows) {
		return inc, phrases, errors.New("no such incident")
	}
	if err != nil {
		return inc, phrases, err
	}
	inc.Branch = contract.Branch(branch)
	if speed.Valid {
		s := speed.Float64
		inc.SpeedMPS = &s
	}
	return inc, phrases, nil
}
