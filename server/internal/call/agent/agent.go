// Package agent holds the "Mamá" ElevenLabs agent: its prompt
// (prompt.md), first message (first_message.txt) and TTS settings
// (tts.json), and the PATCH body that go run ./cmd/pushagent sends to the
// agent. See README.md.
//
// Both texts use the dynamic variables {{nombre}}, {{frase_alerta}} and
// {{frase_segura}}, which the call engine sends when it places the call.
package agent

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed prompt.md
var prompt string

//go:embed first_message.txt
var firstMessage string

//go:embed tts.json
var ttsJSON []byte

// Language is the agent language (ASR and TTS).
const Language = "es"

// Prompt is the system prompt of the agent. Line endings are normalized to
// \n, since a Windows checkout may embed \r\n.
func Prompt() string { return strings.ReplaceAll(prompt, "\r\n", "\n") }

// FirstMessage is what the agent says when the call is answered.
func FirstMessage() string { return strings.TrimSpace(firstMessage) }

// TTS is conversation_config.tts without voice_id: the voice stays the
// agent's own, and the call engine overrides it per call. The agent schema
// has no style or use_speaker_boost (see README.md).
type TTS struct {
	ModelID         string  `json:"model_id"`
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Speed           float64 `json:"speed"`
}

// ttsModels are the agent TTS models that speak Spanish (eleven_turbo_v2
// and eleven_flash_v2 are English only).
var ttsModels = map[string]bool{
	"eleven_turbo_v2_5":        true,
	"eleven_flash_v2_5":        true,
	"eleven_multilingual_v2":   true,
	"eleven_v3_conversational": true,
	"eleven_v4":                true,
	"eleven_v4_turbo":          true,
}

// LoadTTS parses tts.json. Unknown keys and out-of-range values are errors,
// so a setting the agent API does not take never goes unnoticed.
func LoadTTS() (TTS, error) { return parseTTS(ttsJSON) }

func parseTTS(raw []byte) (TTS, error) {
	var t TTS
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return TTS{}, fmt.Errorf("agent: tts.json: %w", err)
	}
	switch {
	case !ttsModels[t.ModelID]:
		return TTS{}, fmt.Errorf("agent: tts.json: model_id %q is not a Spanish agent model", t.ModelID)
	case t.Stability < 0 || t.Stability > 1:
		return TTS{}, fmt.Errorf("agent: tts.json: stability %v is outside 0..1", t.Stability)
	case t.SimilarityBoost < 0 || t.SimilarityBoost > 1:
		return TTS{}, fmt.Errorf("agent: tts.json: similarity_boost %v is outside 0..1", t.SimilarityBoost)
	case t.Speed < 0.7 || t.Speed > 1.2:
		return TTS{}, fmt.Errorf("agent: tts.json: speed %v is outside 0.7..1.2", t.Speed)
	}
	return t, nil
}

// Update is the body of PATCH /v1/convai/agents/{agent_id}. Field names
// follow https://elevenlabs.io/docs/api-reference/agents/update and
// https://elevenlabs.io/docs/agents-platform/customization/tools/system-tools/end-call
// (built_in_tools.end_call). Fields left out keep their current value.
type Update struct {
	ConversationConfig ConversationConfig `json:"conversation_config"`
}

type ConversationConfig struct {
	Agent AgentConfig `json:"agent"`
	TTS   TTS         `json:"tts"`
}

type AgentConfig struct {
	FirstMessage string       `json:"first_message"`
	Language     string       `json:"language"`
	Prompt       PromptConfig `json:"prompt"`
}

type PromptConfig struct {
	Prompt       string       `json:"prompt"`
	BuiltInTools BuiltInTools `json:"built_in_tools"`
}

type BuiltInTools struct {
	EndCall SystemTool `json:"end_call"`
}

// SystemTool is an ElevenLabs system tool such as end_call.
type SystemTool struct {
	Type        string           `json:"type"` // always "system"
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"` // left out: default behavior
	Params      SystemToolParams `json:"params"`
}

type SystemToolParams struct {
	SystemToolType string `json:"system_tool_type"`
}

// NewUpdate builds the PATCH body with the prompt, the first message, the
// language, the end_call system tool and the TTS settings.
func NewUpdate() (Update, error) {
	tts, err := LoadTTS()
	if err != nil {
		return Update{}, err
	}
	return Update{ConversationConfig: ConversationConfig{
		Agent: AgentConfig{
			FirstMessage: FirstMessage(),
			Language:     Language,
			Prompt: PromptConfig{
				Prompt: Prompt(),
				BuiltInTools: BuiltInTools{EndCall: SystemTool{
					Type:   "system",
					Name:   "end_call",
					Params: SystemToolParams{SystemToolType: "end_call"},
				}},
			},
		},
		TTS: tts,
	}}, nil
}
