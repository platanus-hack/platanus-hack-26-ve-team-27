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
	raw, err := json.Marshal(NewUpdate())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ConversationConfig struct {
			Agent struct {
				FirstMessage string `json:"first_message"`
				Language     string `json:"language"`
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
		} `json:"conversation_config"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	a := got.ConversationConfig.Agent
	if a.FirstMessage != FirstMessage() || a.Language != "es" || a.Prompt.Prompt != Prompt() {
		t.Errorf("agent config = %+v", a)
	}
	ec := a.Prompt.BuiltInTools.EndCall
	if ec.Type != "system" || ec.Name != "end_call" || ec.Params.SystemToolType != "end_call" {
		t.Errorf("end_call = %+v", ec)
	}
}
