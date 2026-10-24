package duress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Anthropic Messages API settings for the LLM step.
const (
	DefaultEndpoint = "https://api.anthropic.com/v1/messages"
	// DefaultModel is fast and cheap; the step only classifies one short call.
	DefaultModel = "claude-haiku-5-5"
	apiVersion   = "2026-10-21"
	// maxTokens leaves room for the model's thinking plus a one-line JSON
	// answer. Hitting it leaves no text, which is an error like any other.
	maxTokens = 2048
	// maxBody caps how much of a response is read, so a broken proxy can't
	// make the detector buffer megabytes.
	maxBody = 1 << 20
)

// ErrNoAPIKey means the LLM step has no key (ANTHROPIC_API_KEY unset).
var ErrNoAPIKey = errors.New("no API key")

// LLM asks Claude for the verdict through the Messages API, with the
// standard library only. Any failure (no key, HTTP error, non-2xx, a reply
// that is not the asked JSON) is an error and never a guessed verdict: Chain
// turns errors into the rules verdict.
type LLM struct {
	APIKey   string
	Model    string       // "" = DefaultModel
	Endpoint string       // "" = DefaultEndpoint; tests point it at httptest
	Client   *http.Client // nil = http.DefaultClient; the deadline comes from ctx
}

var _ Detector = LLM{}

func (l LLM) Detect(ctx context.Context, transcript []contract.TranscriptTurn, phrase string) (Result, error) {
	if l.APIKey == "" {
		return Result{}, ErrNoAPIKey
	}
	body, err := json.Marshal(l.request(transcript, phrase))
	if err != nil {
		return Result{}, err
	}
	endpoint := l.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", l.APIKey)
	req.Header.Set("anthropic-version", apiVersion)

	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode/100 != 2 {
		// The status only: an error body can echo the request.
		return Result{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseReply(raw)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model        string         `json:"model"`
	MaxTokens    int            `json:"max_tokens"`
	System       string         `json:"system"`
	Messages     []message      `json:"messages"`
	OutputConfig map[string]any `json:"output_config"`
}

func (l LLM) request(transcript []contract.TranscriptTurn, phrase string) request {
	model := l.Model
	if model == "" {
		model = DefaultModel
	}
	return request{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  []message{{Role: "user", Content: userPrompt(transcript, phrase)}},
		// Low effort: the call is short and the verdict is on the
		// notifier's critical path.
		OutputConfig: map[string]any{"effort": "low"},
	}
}

// systemPrompt restates the labeling rules of server/evals/README.md, the
// same ones Rules implements, so the LLM and the evals agree on what duress
// means.
const systemPrompt = `You judge one phone call for a personal-safety app used in Venezuela.

The person in danger triggered a fake call from "Mamá", an AI voice playing their mother. Before any emergency, the person chose a secret duress phrase. Saying it on the call, in passing, means "I am in danger, alert my family". The transcript is Spanish (Venezuela) from speech recognition, so expect slips, missing accents, cut words ("Car-") and tags such as [silencio], [ruido], [inaudible], [se corta].

Speakers: "MAMÁ" is the AI caller. "USUARIO" is everything heard on the person's line. That channel can also carry a third party (a robber, a taxi driver, a relative), labeled USUARIO too.

Verdict rules:
- duress: the person says the duress phrase, or a close variant with the same meaning and the same key words (reordered, a word swapped or added, a speech-recognition slip, a word cut by noise).
- duress: the person never says a real word the whole call (only tags or silence). The family is alerted when the person stays silent. A different voice heard on the line (for example a man ordering someone to hang up) does not count as the person speaking.
- no_duress: an ordinary call without the phrase.
- no_duress: the key words appear with another meaning, only about half of the phrase is said, or the person negates it ("no se me quemaron las arepas" when the phrase is "se me quemaron las arepas"). A "no" that answers Mamá's question before the phrase ("No... se me quemaron las arepas") is not a negation.
- no_duress: Mamá says the phrase or its words and the person does not say it themselves.
- Someone else speaking is never, by itself, a reason for duress.
- unclear: only when the transcript truly does not let you decide.

Answer with only this JSON object and nothing else:
{"verdict": "duress" | "no_duress" | "unclear", "reason": "<one short English sentence>"}`

func userPrompt(transcript []contract.TranscriptTurn, phrase string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Duress phrase: %q\n\nTranscript:\n", phrase)
	if len(transcript) == 0 {
		b.WriteString("(empty)\n")
	}
	for _, t := range transcript {
		speaker := "USUARIO"
		if t.Role == RoleAgent {
			speaker = "MAMÁ"
		}
		fmt.Fprintf(&b, "%s: %s\n", speaker, t.Message)
	}
	return b.String()
}

// parseReply reads the Messages API response and the JSON verdict in its
// text. It tolerates prose or a code fence around the object, and nothing
// else: an unknown verdict, an empty reply or a refusal is an error.
func parseReply(raw []byte) (Result, error) {
	var resp struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Result{}, fmt.Errorf("bad response: %w", err)
	}
	if resp.StopReason == "refusal" {
		return Result{}, errors.New("the model refused")
	}
	var text strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	s := text.String()
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return Result{}, fmt.Errorf("no JSON verdict in the reply (stop_reason %q)", resp.StopReason)
	}
	var answer struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &answer); err != nil {
		return Result{}, fmt.Errorf("bad JSON verdict: %w", err)
	}
	v := contract.Verdict(strings.TrimSpace(answer.Verdict))
	switch v {
	case contract.VerdictDuress, contract.VerdictNoDuress, contract.VerdictUnclear:
	default:
		return Result{}, fmt.Errorf("unknown verdict %q", answer.Verdict)
	}
	reason := strings.TrimSpace(answer.Reason)
	if reason == "" {
		reason = "the LLM gave no reason"
	}
	return Result{Verdict: v, Reason: reason}, nil
}
