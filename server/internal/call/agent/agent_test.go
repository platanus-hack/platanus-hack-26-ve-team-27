package agent

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// forbidden are the words and questions Mamá never says in the call.
var forbidden = []string{"policia", "emergencia", "alerta", "peligro", "ayuda", "estas bien", "te estan obligando"}

// fold lowercases s and drops Spanish accents so "Policía" matches "policia".
func fold(s string) string {
	return strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u").Replace(strings.ToLower(s))
}

var quoted = regexp.MustCompile(`"([^"]+)"`)

// mamaLines are the lines the prompt gives Mamá to say: every quoted text,
// except the line that lists what she never says and the secret phrases.
func mamaLines(t *testing.T) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(Prompt(), "\n") {
		if strings.Contains(line, "nunca dice") {
			continue
		}
		for _, m := range quoted.FindAllStringSubmatch(line, -1) {
			if strings.Contains(m[1], "{{frase_") {
				continue
			}
			lines = append(lines, m[1])
		}
	}
	if len(lines) < 10 {
		t.Fatalf("found only %d example lines in prompt.md: %q", len(lines), lines)
	}
	return lines
}

func TestPromptUsesDynamicVariables(t *testing.T) {
	for _, v := range []string{"{{nombre}}", "{{frase_alerta}}", "{{frase_segura}}"} {
		if !strings.Contains(Prompt(), v) {
			t.Errorf("prompt.md does not use %s", v)
		}
	}
	if got, want := FirstMessage(), "¡Hola, {{nombre}}! ¿Cómo vas, mi amor?"; got != want {
		t.Errorf("first message = %q, want %q", got, want)
	}
}

var variable = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)

// Every variable the texts use has a default, so the agent can start a
// conversation even when the caller sends none (before #104).
func TestEveryVariableHasPlaceholder(t *testing.T) {
	for _, m := range variable.FindAllStringSubmatch(Prompt()+FirstMessage(), -1) {
		if _, ok := Placeholders[m[1]]; !ok {
			t.Errorf("{{%s}} has no default in Placeholders", m[1])
		}
	}
	if Placeholders["frase_alerta"] != "" || Placeholders["frase_segura"] != "" {
		t.Error("default phrases must be empty: a default phrase could fire by chance")
	}
}

func TestMamaLinesAvoidForbiddenWords(t *testing.T) {
	lines := append(mamaLines(t), FirstMessage())
	for _, line := range lines {
		for _, w := range forbidden {
			if strings.Contains(fold(line), w) {
				t.Errorf("example line %q says %q", line, w)
			}
		}
	}
}

func TestPromptListsForbiddenWords(t *testing.T) {
	// The prompt itself must tell Mamá not to say them.
	for _, w := range forbidden {
		if !strings.Contains(fold(Prompt()), w) {
			t.Errorf("prompt.md does not forbid %q", w)
		}
	}
}

func TestUpdateBody(t *testing.T) {
	u, err := NewUpdate()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ConversationConfig struct {
			Agent struct {
				FirstMessage     string `json:"first_message"`
				Language         string `json:"language"`
				DynamicVariables struct {
					Placeholders map[string]string `json:"dynamic_variable_placeholders"`
				} `json:"dynamic_variables"`
				Prompt       struct {
					Prompt       string `json:"prompt"`
					BuiltInTools struct {
						EndCall struct {
							Type   string `json:"type"`
							Name   string `json:"name"`
							Params struct {
								SystemToolType string `json:"system_tool_type"`
							} `json:"params"`
						} `json:"end_call"`
					} `json:"built_in_tools"`
				} `json:"prompt"`
			} `json:"agent"`
			TTS map[string]any `json:"tts"`
		} `json:"conversation_config"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	a := got.ConversationConfig.Agent
	if a.FirstMessage != FirstMessage() || a.Language != "es" || a.Prompt.Prompt != Prompt() {
		t.Errorf("agent config = %+v", a)
	}
	if ph := a.DynamicVariables.Placeholders; len(ph) != 3 || ph["nombre"] == "" {
		t.Errorf("dynamic_variable_placeholders = %v", ph)
	}
	ec := a.Prompt.BuiltInTools.EndCall
	if ec.Type != "system" || ec.Name != "end_call" || ec.Params.SystemToolType != "end_call" {
		t.Errorf("end_call = %+v", ec)
	}
	wantTTS := map[string]any{"model_id": "eleven_flash_v2_5", "stability": 0.45, "similarity_boost": 0.85, "speed": 1.0}
	if len(got.ConversationConfig.TTS) != len(wantTTS) {
		t.Errorf("tts = %v, want exactly %v (no voice_id)", got.ConversationConfig.TTS, wantTTS)
	}
	for k, v := range wantTTS {
		if got.ConversationConfig.TTS[k] != v {
			t.Errorf("tts.%s = %v, want %v", k, got.ConversationConfig.TTS[k], v)
		}
	}
}

func TestParseTTSRejects(t *testing.T) {
	for name, raw := range map[string]string{
		"style (not in the agent API)": `{"model_id":"eleven_flash_v2_5","stability":0.45,"similarity_boost":0.85,"speed":1,"style":0}`,
		"voice_id":                     `{"model_id":"eleven_flash_v2_5","stability":0.45,"similarity_boost":0.85,"speed":1,"voice_id":"v"}`,
		"English-only model":           `{"model_id":"eleven_flash_v2","stability":0.45,"similarity_boost":0.85,"speed":1}`,
		"stability > 1":                `{"model_id":"eleven_flash_v2_5","stability":1.5,"similarity_boost":0.85,"speed":1}`,
		"similarity_boost < 0":         `{"model_id":"eleven_flash_v2_5","stability":0.45,"similarity_boost":-0.1,"speed":1}`,
		"speed too fast":               `{"model_id":"eleven_flash_v2_5","stability":0.45,"similarity_boost":0.85,"speed":2}`,
	} {
		if _, err := parseTTS([]byte(raw)); err == nil {
			t.Errorf("%s: parseTTS accepted %s", name, raw)
		}
	}
}
