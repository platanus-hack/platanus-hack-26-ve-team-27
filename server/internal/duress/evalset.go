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
	// SafePhrase is the "todo bien" phrase (#106); "" for the dialogs from
	// before it, which keep the duress-only rules.
	SafePhrase string `json:"safe_phrase,omitempty"`
	// Expect is the verdict the rules must give, when set. The safe-phrase
	// dialogs set it, since "said neither phrase" is unclear by design.
	Expect contract.Verdict `json:"expect,omitempty"`
	Turns  []EvalTurn       `json:"turns"`
}

// EvalTurn is one turn of an eval dialog: speaker "mama", "user" or "other".
type EvalTurn struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

// Phrases are the dialog's configured phrases.
func (d EvalDialog) Phrases() Phrases {
	return Phrases{Duress: d.DuressPhrase, Safe: d.SafePhrase}
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

// EvalFichaDialog is one line of server/evals/ficha_dialogs.jsonl: a call and
// the call-context fields it lets a reader know (schema in
// server/evals/README.md).
type EvalFichaDialog struct {
	ID           string     `json:"id"`
	DuressPhrase string     `json:"duress_phrase"`
	SafePhrase   string     `json:"safe_phrase"`
	Known        []string   `json:"known"` // lugar, personas, vehiculo, rumbo, situacion
	Turns        []EvalTurn `json:"turns"`
}

// Dialog is d as an EvalDialog, for its Transcript and Phrases.
func (d EvalFichaDialog) Dialog() EvalDialog {
	return EvalDialog{ID: d.ID, DuressPhrase: d.DuressPhrase, SafePhrase: d.SafePhrase, Turns: d.Turns}
}

// LoadEvalDialogs reads a JSONL file of EvalDialog, skipping blank lines. An
// empty file is an error: an eval over nothing would print perfect scores.
func LoadEvalDialogs(path string) ([]EvalDialog, error) { return loadJSONL[EvalDialog](path) }

// LoadEvalFichaDialogs reads a JSONL file of EvalFichaDialog, like
// LoadEvalDialogs.
func LoadEvalFichaDialogs(path string) ([]EvalFichaDialog, error) {
	return loadJSONL[EvalFichaDialog](path)
}

func loadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New(path + ": no eval dialogs")
	}
	return out, nil
}
