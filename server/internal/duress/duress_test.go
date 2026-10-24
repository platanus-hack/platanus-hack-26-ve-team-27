package duress

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

const demoPhrase = "dile a la tía Carmen que llego tarde"

func agent(msg string) contract.TranscriptTurn {
	return contract.TranscriptTurn{Role: RoleAgent, Message: msg}
}
func user(msg string) contract.TranscriptTurn {
	return contract.TranscriptTurn{Role: RoleUser, Message: msg}
}

func TestRules(t *testing.T) {
	tests := []struct {
		name       string
		phrase     string
		transcript []contract.TranscriptTurn
		want       contract.Verdict
		reason     string // substring of the reason
	}{
		{"exact", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Sí, mami. Oye, dile a la tía Carmen que llego tarde.")},
			contract.VerdictDuress, "said the duress phrase"},
		{"exact without accents or case", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("DILE A LA TIA CARMEN QUE LLEGO TARDE")},
			contract.VerdictDuress, "said the duress phrase"},
		{"variant with inserted words", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Dile a mi tía Carmen que voy a llegar tarde.")},
			contract.VerdictDuress, "close variant"},
		{"reordered variant", "se me quemaron las arepas",
			[]contract.TranscriptTurn{agent("Hola"), user("Las arepas, ma, se me quemaron.")},
			contract.VerdictDuress, "close variant"},
		{"word cut by the line", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Mami, dile a la tía Car- [se corta] ...llego tarde.")},
			contract.VerdictDuress, "close variant"},
		{"phrase split across user turns", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Mami, dile a la tía Carmen"), user("que llego tarde.")},
			contract.VerdictDuress, "said the duress phrase"},
		{"one edit away", "saluda al primo Gustavo",
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Ma, me saludas al primo Gustavo.")},
			contract.VerdictDuress, "close variant"},
		{"leading no is not a negation of the phrase", "el carro sigue en el taller",
			[]contract.TranscriptTurn{agent("¿Vienes en el carro?"), user("No, ma, el carro sigue en el taller.")},
			contract.VerdictDuress, "said the duress phrase"},
		{"phrase with its own negation", "no consigo las llaves del portón",
			[]contract.TranscriptTurn{agent("¿Llegaste?"), user("Sí, pero no consigo las llaves del portón.")},
			contract.VerdictDuress, "said the duress phrase"},
		{"clean phrase wins over an earlier negated one", "se me quemaron las arepas",
			[]contract.TranscriptTurn{agent("Hola"), user("No se me quemaron las arepas."), agent("¿Seguro?"), user("Bueno sí, se me quemaron las arepas.")},
			contract.VerdictDuress, "said the duress phrase"},

		{"silent user", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("[silencio]"), agent("¿Me escuchas?"), user("[respiración] [ruido]")},
			contract.VerdictDuress, "silent"},
		{"no user turn at all", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), agent("¿Mi amor?")},
			contract.VerdictDuress, "silent"},

		{"mamá says the phrase", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Le digo a la tía Carmen que llego tarde?"), user("No hace falta, ma.")},
			contract.VerdictNoDuress, "did not say"},
		{"ordinary call", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Cómo te fue?"), user("Bien, ma, ya voy para la casa.")},
			contract.VerdictNoDuress, "did not say"},
		{"negated inside the phrase", "dile a mi abuela que me guarde hallacas",
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Ma, dile a mi abuela que no me guarde hallacas, que ya comí.")},
			contract.VerdictUnclear, "negation"},
		{"no that answers a question is not a negation", "se me quemaron las arepas",
			[]contract.TranscriptTurn{agent("¿Ya comiste?"), user("No... se me quemaron las arepas, no comí.")},
			contract.VerdictDuress, "said the duress phrase"},
		{"key words without the lead word", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Y la fiesta?"), user("La tía Carmen llegó tarde a la fiesta.")},
			contract.VerdictUnclear, "out of order or without its first word"},
		{"key words out of order", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Qué dice?"), user("Carmen, la tía de Pedro, dice que llego tarde.")},
			contract.VerdictUnclear, "out of order or without its first word"},
		{"variant keeping order and lead word", "mañana llevo el pan dulce",
			[]contract.TranscriptTurn{agent("Hola"), user("Ma, mañana te llevo el pan.")},
			contract.VerdictDuress, "close variant"},
		{"only a third party talks", demoPhrase,
			[]contract.TranscriptTurn{agent("¿Aló?"), user("[silencio]"), user("¿Quién es? Cuelga ya.")},
			contract.VerdictNoDuress, "did not say"},
		{"negated right before the phrase", "se me quemaron las arepas",
			[]contract.TranscriptTurn{agent("¿Qué cocinaste?"), user("Esta vez no se me quemaron las arepas.")},
			contract.VerdictUnclear, "negation"},
		{"partial phrase", "el carro sigue en el taller",
			[]contract.TranscriptTurn{agent("Hola"), user("Voy en el carro, ¿paso por el taller?")},
			contract.VerdictUnclear, "part of the duress phrase"},

		{"empty transcript", demoPhrase, nil, contract.VerdictUnclear, "empty"},
		{"no phrase configured", "",
			[]contract.TranscriptTurn{agent("¿Aló?"), user("Hola, ma.")},
			contract.VerdictUnclear, "no duress phrase"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Rules{}.Detect(context.Background(), tt.transcript, tt.phrase)
			if err != nil {
				t.Fatal(err)
			}
			if got.Verdict != tt.want || !strings.Contains(got.Reason, tt.reason) {
				t.Errorf("got %s (%q), want %s with reason containing %q", got.Verdict, got.Reason, tt.want, tt.reason)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize("¡Mamá! [inaudible, voz] se me olvidó la cédula... Car- e-mail")
	want := []token{{"mama", false, true}, {"se", false, true}, {"me", false, false}, {"olvido", false, false},
		{"la", false, false}, {"cedula", false, false}, {"car", true, true}, {"e", false, false}, {"mail", false, false}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestEditDistanceAtMostOne(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"saluda", "saluda", true},
		{"saluda", "saludas", true},
		{"saludas", "saluda", true},
		{"diste", "viste", true},
		{"carmen", "carmn", true},
		{"llego", "llegar", false},
		{"cedula", "cedulas2", false},
		{"", "a", true},
	}
	for _, tt := range tests {
		if got := editDistanceAtMostOne(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistanceAtMostOne(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// evalDialog is one line of server/evals/duress_dialogs.jsonl.
type evalDialog struct {
	ID           string `json:"id"`
	Duress       bool   `json:"duress"`
	NearMiss     bool   `json:"near_miss"`
	Kind         string `json:"kind"`
	DuressPhrase string `json:"duress_phrase"`
	Turns        []struct {
		Speaker string `json:"speaker"`
		Text    string `json:"text"`
	} `json:"turns"`
}

// transcript maps the eval speakers onto the two ElevenLabs roles: a third
// party ("other") is heard on the user's channel.
func (d evalDialog) transcript() []contract.TranscriptTurn {
	out := make([]contract.TranscriptTurn, 0, len(d.Turns))
	for _, turn := range d.Turns {
		role := RoleUser
		if turn.Speaker == "mama" {
			role = RoleAgent
		}
		out = append(out, contract.TranscriptTurn{Role: role, Message: turn.Text})
	}
	return out
}

func loadEvals(t *testing.T) []evalDialog {
	t.Helper()
	f, err := os.Open("../../evals/duress_dialogs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var dialogs []evalDialog
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var d evalDialog
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			t.Fatalf("bad eval line %q: %v", sc.Text(), err)
		}
		dialogs = append(dialogs, d)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(dialogs) == 0 {
		t.Fatal("no eval dialogs")
	}
	return dialogs
}

// TestRulesEvalCorpus runs the rules over every labeled dialog. Exact and
// variant phrases must be duress; near misses must not be. It logs precision
// and recall over the whole file, counting only duress as positive.
func TestRulesEvalCorpus(t *testing.T) {
	var tp, fp, fn, tn, unclear int
	for _, d := range loadEvals(t) {
		got, err := Rules{}.Detect(context.Background(), d.transcript(), d.DuressPhrase)
		if err != nil {
			t.Fatalf("%s: %v", d.ID, err)
		}
		positive := got.Verdict == contract.VerdictDuress
		switch {
		case positive && d.Duress:
			tp++
		case positive:
			fp++
		case d.Duress:
			fn++
		default:
			tn++
		}
		if got.Verdict == contract.VerdictUnclear {
			unclear++
		}

		switch d.Kind {
		case "exact", "variant", "silence":
			if !positive {
				t.Errorf("%s (%s): got %s (%q), want duress", d.ID, d.Kind, got.Verdict, got.Reason)
			}
		case "near_miss", "normal":
			if positive {
				t.Errorf("%s (%s): got duress (%q), want not duress", d.ID, d.Kind, got.Reason)
			}
		}
		t.Logf("%s %-9s label=%-5v -> %-9s %s", d.ID, d.Kind, d.Duress, got.Verdict, got.Reason)
	}
	precision := float64(tp) / float64(max(tp+fp, 1))
	recall := float64(tp) / float64(max(tp+fn, 1))
	t.Logf("eval: %d dialogs, tp=%d fp=%d fn=%d tn=%d (unclear=%d); precision=%.3f recall=%.3f",
		tp+fp+fn+tn, tp, fp, fn, tn, unclear, precision, recall)
}

// TestRulesWebhookSample feeds the ElevenLabs post-call webhook sample from
// the contract through the detector, the way the webhook handler will.
func TestRulesWebhookSample(t *testing.T) {
	raw, err := os.ReadFile("../contract/testdata/call_webhook.json")
	if err != nil {
		t.Fatal(err)
	}
	var w contract.CallWebhook
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	got, err := Rules{}.Detect(context.Background(), w.Data.Transcript, demoPhrase)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != contract.VerdictDuress {
		t.Fatalf("got %s (%q), want duress", got.Verdict, got.Reason)
	}
	t.Logf("webhook sample %s -> %s: %s", w.Data.ConversationID, got.Verdict, got.Reason)
}
