// Package agent holds the "Mamá" ElevenLabs agent: its prompt
// (prompt.md) and first message (first_message.txt), and the PATCH body that
// go run ./cmd/pushagent sends to the agent. See README.md.
//
// Both texts use the dynamic variables {{nombre}}, {{frase_alerta}} and
// {{frase_segura}}, which the call engine sends when it places the call.
package agent

import (
	_ "embed"
	"strings"
)

//go:embed prompt.md
var prompt string

//go:embed first_message.txt
var firstMessage string

// Language is the agent language (ASR and TTS).
const Language = "es"

// Prompt is the system prompt of the agent. Line endings are normalized to
// \n, since a Windows checkout may embed \r\n.
func Prompt() string { return strings.ReplaceAll(prompt, "\r\n", "\n") }

// FirstMessage is what the agent says when the call is answered.
func FirstMessage() string { return strings.TrimSpace(firstMessage) }

// Update is the body of PATCH /v1/convai/agents/{agent_id}. Field names
// follow https://elevenlabs.io/docs/api-reference/agents/update and
// https://elevenlabs.io/docs/agents-platform/customization/tools/system-tools/end-call
// (built_in_tools.end_call). Fields left out keep their current value.
type Update struct {
	ConversationConfig ConversationConfig `json:"conversation_config"`
}

type ConversationConfig struct {
	Agent AgentConfig `json:"agent"`
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
	Description string           `json:"description"` // empty keeps the default behavior
	Params      SystemToolParams `json:"params"`
}

type SystemToolParams struct {
	SystemToolType string `json:"system_tool_type"`
}

// NewUpdate builds the PATCH body with the prompt, the first message, the
// language and the end_call system tool.
func NewUpdate() Update {
	return Update{ConversationConfig: ConversationConfig{Agent: AgentConfig{
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
	}}}
}
