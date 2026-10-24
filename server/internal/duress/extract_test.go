package duress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

var carCall = []contract.TranscriptTurn{
	agent("¿Dónde estás, mi amor?"),
	user("Voy en un carro con dos panas por la autopista. Dile a la tía Carmen que llego tarde."),
}

func TestLLMExtract(t *testing.T) {
	var body request
	llm, _ := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		answer("```json\n"+`{"lugar": "la autopista", "personas": "dos panas", "vehiculo": "un carro",
			"rumbo": "", "situacion": "Va en un carro con dos panas y dijo la frase de alerta.", "confianza": "Media"}`+"\n```")(w, r)
	})
	got, err := llm.Extract(context.Background(), carCall, bothPhrases)
	if err != nil {
		t.Fatal(err)
	}
	want := contract.CallContext{
		Lugar: "la autopista", Personas: "dos panas", Vehiculo: "un carro", Rumbo: contract.CallContextUnknown,
		Situacion: "Va en un carro con dos panas y dijo la frase de alerta.", Confianza: contract.ConfianzaMedia,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if body.System != extractPrompt || body.Model != DefaultModel {
		t.Errorf("request %+v", body)
	}
	prompt := body.Messages[0].Content
	for _, want := range []string{`"` + demoPhrase + `"`, `"ya voy llegando"`, "USUARIO: Voy en un carro", "</transcript>"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt %q misses %q", prompt, want)
		}
	}
}

func TestParseContext(t *testing.T) {
	got, err := parseContext(`{"lugar": "No se sabe.", "personas": "  ", "situacion": "` + strings.Repeat("á", 500) + `", "confianza": "muy alta"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lugar != contract.CallContextUnknown || got.Personas != contract.CallContextUnknown ||
		got.Vehiculo != contract.CallContextUnknown || got.Confianza != contract.ConfianzaBaja {
		t.Errorf("got %+v", got)
	}
	if n := len([]rune(got.Situacion)); n != maxContextField+len("...") {
		t.Errorf("situacion has %d runes, want it capped at %d", n, maxContextField)
	}
	for _, bad := range []string{"no sé", `{"lugar": 3}`} {
		if _, err := parseContext(bad); err == nil {
			t.Errorf("parseContext(%q) = nil error", bad)
		}
	}
}

func TestContextKnown(t *testing.T) {
	for v, want := range map[string]bool{
		"": false, "no se sabe": false, "No sé sabe": false, "NO SE SABE.": false, "desconocido": false,
		"la autopista": true, "no sé, ma": true,
	} {
		if got := ContextKnown(v); got != want {
			t.Errorf("ContextKnown(%q) = %v, want %v", v, got, want)
		}
	}
}

type fakeExtractor func(ctx context.Context) (contract.CallContext, error)

func (f fakeExtractor) Extract(ctx context.Context, _ []contract.TranscriptTurn, _ Phrases) (contract.CallContext, error) {
	return f(ctx)
}

func TestExtractContextTimesOut(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ignoresCtx := fakeExtractor(func(context.Context) (contract.CallContext, error) {
		<-release
		return contract.CallContext{Lugar: "late"}, nil
	})
	start := time.Now()
	_, err := ExtractContext(context.Background(), ignoresCtx, 50*time.Millisecond, carCall, bothPhrases)
	if err == nil || err.Error() != "timeout" {
		t.Fatalf("err %v, want timeout", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v, want about the 50ms timeout", d)
	}
}

func TestExtractContextFailure(t *testing.T) {
	fails := fakeExtractor(func(context.Context) (contract.CallContext, error) {
		return contract.CallContext{}, errors.New("HTTP 529")
	})
	if _, err := ExtractContext(context.Background(), fails, 0, carCall, bothPhrases); err == nil || err.Error() != "HTTP 529" {
		t.Fatalf("err %v, want HTTP 529", err)
	}
	if _, err := ExtractContext(context.Background(), LLM{}, 0, carCall, bothPhrases); !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("err %v, want no API key", err)
	}
}

func TestExtractKeepsTheCodePhrasesOut(t *testing.T) {
	llm, _ := fakeAPI(t, answer(`{"lugar": "la autopista", "personas": "dos panas", "vehiculo": "no se sabe", "rumbo": "no se sabe",
		"situacion": "Dijo: dile a la tía Carmen que llego tarde.", "confianza": "media"}`))
	got, err := llm.Extract(context.Background(), carCall, bothPhrases)
	if err != nil {
		t.Fatal(err)
	}
	if got.Situacion != contract.CallContextUnknown || got.Lugar != "la autopista" {
		t.Errorf("got %+v, want the quoting situacion dropped and the rest kept", got)
	}
	safe := scrubPhrases(contract.CallContext{Rumbo: "ya voy llegando a la casa", Lugar: "la casa de la tía"}, bothPhrases)
	if safe.Rumbo != contract.CallContextUnknown || safe.Lugar != "la casa de la tía" {
		t.Errorf("scrub %+v", safe)
	}
	if !strings.Contains(extractPrompt, "never repeat them") {
		t.Error("the prompt should forbid repeating the code phrases")
	}
}
