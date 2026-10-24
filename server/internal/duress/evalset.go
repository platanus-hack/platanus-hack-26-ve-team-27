package duress

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// EvalDialog is one line of server/evals/duress_dialogs.jsonl (schema in
// server/evals/README.md). It lives here, not in a test file, so the rules
// tests and the eval runner (cmd/evals) read the corpus the same way.
type EvalDialog struct {
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

// Transcript maps the eval speakers onto the two ElevenLabs roles: a third
// party ("other") is heard on the user's channel.
func (d EvalDialog) Transcript() []contract.TranscriptTurn {
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

// LoadEvalDialogs reads a JSONL file of EvalDialog, skipping blank lines. An
// empty file is an error: an eval over nothing would print perfect scores.
func LoadEvalDialogs(path string) ([]EvalDialog, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var dialogs []EvalDialog
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var d EvalDialog
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		dialogs = append(dialogs, d)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(dialogs) == 0 {
		return nil, errors.New(path + ": no eval dialogs")
	}
	return dialogs, nil
}
