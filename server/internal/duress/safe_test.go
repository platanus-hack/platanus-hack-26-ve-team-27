package duress

import (
	"context"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

const safePhrase = "ya voy llegando"

var bothPhrases = Phrases{Duress: demoPhrase, Safe: safePhrase}

// TestRulesWithSafePhrase is #106's table: the duress phrase wins, silence is
// duress, the safe phrase is no duress, and neither is unclear because the
// person did not confirm they are fine.
func TestRulesWithSafePhrase(t *testing.T) {
	tests := []struct {
		name       string
		transcript []contract.TranscriptTurn
		want       contract.Verdict
		reason     string
	}{
		{"safe phrase", []contract.TranscriptTurn{
			agent("¿Dónde estás, mi amor?"), user("Tranquila, ma, ya voy llegando."),
		}, contract.VerdictNoDuress, "the user said the safe phrase"},
		{"safe phrase with an ASR slip", []contract.TranscriptTurn{
			agent("¿Vienes?"), user("Sí, ma, voy llegano, voy llegano."),
		}, contract.VerdictNoDuress, "close variant of the safe phrase"},
		{"both phrases: the alert wins", []contract.TranscriptTurn{
			agent("¿Todo bien?"), user("Sí, ya voy llegando. Dile a la tía Carmen que llego tarde."),
		}, contract.VerdictDuress, "the user said the duress phrase"},
		{"both phrases, the duress one as a variant", []contract.TranscriptTurn{
			user("Ya voy llegando, ma."), agent("¿Y la tía?"), user("Dile a mi tía Carmen que llego tardecito... tarde."),
		}, contract.VerdictDuress, "duress phrase"},
		{"neither phrase, having talked", []contract.TranscriptTurn{
			agent("¿Cómo te fue?"), user("Bien, ma, aquí en el trabajo, después te llamo."),
		}, contract.VerdictUnclear, "the user said neither phrase, " + reasonNotConfirmed},
		{"no voy llegando is not the safe phrase", []contract.TranscriptTurn{
			agent("¿Ya vienes?"), user("No voy llegando todavía, ma, hay mucha cola."),
		}, contract.VerdictUnclear, "with a negation"},
		{"a no answering before the safe phrase is not a negation", []contract.TranscriptTurn{
			agent("¿Te pasó algo?"), user("No... ya voy llegando, ma."),
		}, contract.VerdictNoDuress, "the user said the safe phrase"},
		{"vague answer", []contract.TranscriptTurn{
			agent("¿Dónde estás?"), user("No sé, ma."), agent("¿Cómo que no sabes?"), user("No sé."),
		}, contract.VerdictUnclear, reasonNotConfirmed},
		{"silence", []contract.TranscriptTurn{
			agent("¿Aló? ¿Mi amor?"), user("[silencio]"), agent("¿Me escuchas?"), user("[ruido]"),
		}, contract.VerdictDuress, "silent"},
		{"mamá says the safe phrase", []contract.TranscriptTurn{
			agent("¿Ya vas llegando? Dime ya voy llegando."), user("Mm, sí, ma."),
		}, contract.VerdictUnclear, reasonNotConfirmed},
		{"part of the duress phrase and no safe phrase", []contract.TranscriptTurn{
			agent("¿Y la tía?"), user("La tía Carmen está bien."),
		}, contract.VerdictUnclear, reasonNotConfirmed},
		{"negated duress phrase, then the safe phrase", []contract.TranscriptTurn{
			user("No le digas a la tía Carmen que llego tarde, ya voy llegando."),
		}, contract.VerdictNoDuress, "safe phrase"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Rules{}.Detect(context.Background(), tt.transcript, bothPhrases)
			if err != nil {
				t.Fatal(err)
			}
			if got.Verdict != tt.want || !strings.Contains(got.Reason, tt.reason) {
				t.Errorf("got %s (%q), want %s with %q", got.Verdict, got.Reason, tt.want, tt.reason)
			}
		})
	}
}

// TestRulesWithoutSafePhraseUnchanged checks that a device with no safe
// phrase keeps the duress-only rules: an ordinary call is no duress.
func TestRulesWithoutSafePhraseUnchanged(t *testing.T) {
	call := []contract.TranscriptTurn{agent("¿Cómo te fue?"), user("Bien, ma, ya voy llegando.")}
	for _, p := range []Phrases{{Duress: demoPhrase}, {Duress: demoPhrase, Safe: "  "}} {
		got, _ := Rules{}.Detect(context.Background(), call, p)
		if got.Verdict != contract.VerdictNoDuress || got.Reason != "the user did not say the duress phrase" {
			t.Errorf("%+v: got %+v, want the duress-only no_duress", p, got)
		}
	}
}

// TestChainNeverCancelsWithASafePhrase: when the person did not say the safe
// phrase, an LLM no_duress must not silence the alert.
func TestChainNeverCancelsWithASafePhrase(t *testing.T) {
	llm, calls := fakeAPI(t, answer(`{"verdict": "no_duress", "reason": "an ordinary call"}`))
	call := []contract.TranscriptTurn{agent("¿Cómo te fue?"), user("Bien, ma, aquí en el trabajo.")}
	got, err := Chain{LLM: llm}.Detect(context.Background(), call, bothPhrases)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != contract.VerdictUnclear || calls.Load() != 1 || !strings.Contains(got.Reason, "kept unclear") {
		t.Fatalf("got %+v after %d LLM calls, want unclear kept after 1", got, calls.Load())
	}
}

func TestChainLetsTheLLMRaiseToDuressWithASafePhrase(t *testing.T) {
	llm, _ := fakeAPI(t, answer(`{"verdict": "duress", "reason": "a variant of the duress phrase"}`))
	call := []contract.TranscriptTurn{agent("¿Y la tía?"), user("La tía Carmen está bien.")}
	got, _ := Chain{LLM: llm}.Detect(context.Background(), call, bothPhrases)
	if got.Verdict != contract.VerdictDuress {
		t.Fatalf("got %+v, want duress", got)
	}
}

func TestChainSkipsTheLLMOnTheSafePhrase(t *testing.T) {
	llm, calls := fakeAPI(t, answer(`{"verdict": "duress", "reason": "x"}`))
	call := []contract.TranscriptTurn{user("Tranquila, ma, ya voy llegando.")}
	got, _ := Chain{LLM: llm}.Detect(context.Background(), call, bothPhrases)
	if got.Verdict != contract.VerdictNoDuress || calls.Load() != 0 {
		t.Fatalf("got %+v after %d LLM calls, want the rules' no_duress and no LLM call", got, calls.Load())
	}
}

func TestLLMPromptCarriesBothPhrases(t *testing.T) {
	p := userPrompt([]contract.TranscriptTurn{user("hola")}, bothPhrases)
	for _, want := range []string{`Duress phrase: "` + demoPhrase + `"`, `Safe phrase: "ya voy llegando"`} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt %q misses %q", p, want)
		}
	}
	if p := userPrompt(nil, Phrases{Duress: demoPhrase}); !strings.Contains(p, "Safe phrase: (none configured)") {
		t.Errorf("prompt without a safe phrase: %q", p)
	}
}
