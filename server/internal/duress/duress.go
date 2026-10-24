// Package duress judges a "Mamá" call transcript against the user's duress
// phrase. Detector is the seam: Rules is the first, deterministic step; LLM
// (#22) is the second, behind the same interface; Chain runs Rules and asks
// the LLM only about the calls Rules marks unclear, falling back to the rules
// verdict when the LLM fails. Production uses Chain.
package duress

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Result is a detector's verdict for one call and why, as stored on the call
// (calls.verdict, calls.verdict_reason) and sent in NotifierMessage.
type Result struct {
	Verdict contract.Verdict
	Reason  string
}

// Detector judges one call. transcript is in call order, Role "agent" or
// "user"; any third party heard on the line arrives as "user". phrase is the
// duress phrase from the device's setup.
type Detector interface {
	Detect(ctx context.Context, transcript []contract.TranscriptTurn, phrase string) (Result, error)
}

// Role values in contract.TranscriptTurn.
const (
	RoleAgent = "agent"
	RoleUser  = "user"
)

// Thresholds of the rules, as the share of the phrase's key words the user
// said inside one window.
const (
	// matchShare and up is the phrase or a close variant: duress.
	matchShare = 0.75
	// partialShare and up, with at least two key words, is a partial hit
	// the rules can't settle: unclear.
	partialShare = 0.5
	// windowSlack is how many extra words a window holds beyond the phrase's
	// own length, so inserted words ("mi tía", "todavía") still fit.
	windowSlack = 3
)

// Rules is the rules-only detector. It never errors and never calls out.
//
//   - No user word in the whole call (only tags like [silencio]) is duress:
//     the family is alerted when the person stays silent.
//   - The phrase, or most of its key words close together in what the user
//     said, is duress, unless a negation sits inside or just before it
//     ("no se me quemaron").
//   - Half of the key words, or most of them negated or out of order, is
//     unclear.
//   - Anything else is no duress.
//
// Only user turns count: "Mamá" saying the phrase is not the user saying it.
// A third party is heard on the user's channel, so a call where only the
// robber talks is not silence. Chain keeps that no duress for now (see its
// override policy).
type Rules struct{}

var _ Detector = Rules{}

func (Rules) Detect(_ context.Context, transcript []contract.TranscriptTurn, phrase string) (Result, error) {
	if len(transcript) == 0 {
		return Result{contract.VerdictUnclear, "the transcript is empty"}, nil
	}
	runs := userRuns(transcript)
	if len(runs) == 0 {
		return Result{contract.VerdictDuress, "the user stayed silent the whole call"}, nil
	}

	p := newPattern(phrase)
	if len(p.words) == 0 {
		return Result{contract.VerdictUnclear, "no duress phrase is configured"}, nil
	}

	best := hit{}
	for _, run := range runs {
		h := p.bestHit(run)
		h.exact = containsSeq(run, p.words)
		if p.better(h, best) {
			best = h
		}
	}

	n, total := best.matched, len(p.keys)
	switch p.level(best) {
	case levelExact:
		return Result{contract.VerdictDuress, "the user said the duress phrase"}, nil
	case levelVariant:
		return Result{contract.VerdictDuress, fmt.Sprintf(
			"the user said a close variant of the duress phrase (%d of %d key words)", n, total)}, nil
	case levelNegated:
		how := "with a negation"
		if !best.negated {
			how = "out of order or without its first word"
		}
		return Result{contract.VerdictUnclear, fmt.Sprintf(
			"the user said the duress phrase's words %s (%d of %d key words)", how, n, total)}, nil
	case levelPartial:
		return Result{contract.VerdictUnclear, fmt.Sprintf(
			"the user said part of the duress phrase (%d of %d key words)", n, total)}, nil
	default:
		return Result{contract.VerdictNoDuress, "the user did not say the duress phrase"}, nil
	}
}

// token is one normalized word. cut marks a word the line cut off ("Car-"),
// which matches any key word it starts. pause marks a word that starts a
// turn or follows punctuation or a tag: "No... se me quemaron" answers a
// question, "no se me quemaron" negates the phrase.
type token struct {
	text  string
	cut   bool
	pause bool
}

// userRuns returns the words of each run of consecutive user turns, merged
// because a user utterance can be split across turns. Runs with no words
// (only tags) are dropped.
func userRuns(transcript []contract.TranscriptTurn) [][]token {
	var runs [][]token
	var cur []token
	for _, t := range transcript {
		if t.Role != RoleUser {
			if len(cur) > 0 {
				runs = append(runs, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, tokenize(t.Message)...)
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs
}

// tokenize lowercases s, folds accents, drops [bracketed tags] and splits on
// anything that is not a letter or digit.
func tokenize(s string) []token {
	var toks []token
	var b strings.Builder
	pause := true
	flush := func(cut bool) {
		if b.Len() > 0 {
			toks = append(toks, token{text: b.String(), cut: cut, pause: pause})
			b.Reset()
			pause = false
		}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '[':
			flush(false)
			for i < len(rs) && rs[i] != ']' {
				i++
			}
			pause = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(fold(r))
		case r == '-' && (i+1 == len(rs) || !unicode.IsLetter(rs[i+1])):
			flush(true)
		case unicode.IsSpace(r) || r == '-':
			flush(false)
		default: // punctuation
			flush(false)
			pause = true
		}
	}
	flush(false)
	return toks
}

// fold lowercases r and strips Spanish accents, so "olvidó", "olvido" and an
// ASR "olvido" compare equal.
func fold(r rune) rune {
	r = unicode.ToLower(r)
	switch r {
	case 'á', 'à', 'ä':
		return 'a'
	case 'é', 'è', 'ë':
		return 'e'
	case 'í', 'ì', 'ï':
		return 'i'
	case 'ó', 'ò', 'ö':
		return 'o'
	case 'ú', 'ù', 'ü':
		return 'u'
	case 'ñ':
		return 'n'
	}
	return r
}

// stopwords are Spanish function words. They never count as key words, so
// "la abuela" and "mi abuela" match the same way.
var stopwords = set("a", "al", "con", "de", "del", "el", "en", "la", "las", "le", "les", "lo", "los",
	"me", "mi", "mis", "o", "para", "por", "que", "se", "su", "te", "tu", "un", "una", "unas", "unos", "y", "ya", "yo")

// negations flip a phrase's meaning when they appear around it and the
// phrase itself has none ("no se me olvidó la cédula").
var negations = set("no", "nunca", "jamas", "tampoco", "ni")

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// pattern is a duress phrase ready to match.
type pattern struct {
	words   []string        // every normalized word, in order
	keys    []string        // distinct non-stopwords; all words if none
	inWords map[string]bool // words, for the negation scan
	negates bool            // the phrase itself has a negation ("no consigo...")
}

func newPattern(phrase string) pattern {
	p := pattern{inWords: map[string]bool{}}
	for _, t := range tokenize(phrase) {
		p.words = append(p.words, t.text)
		p.inWords[t.text] = true
		if negations[t.text] {
			p.negates = true
		}
	}
	seen := map[string]bool{}
	for _, w := range p.words {
		if !stopwords[w] && !seen[w] {
			seen[w] = true
			p.keys = append(p.keys, w)
		}
	}
	if len(p.keys) == 0 {
		p.keys = p.words
	}
	return p
}

// hit is the best window found in one run. exact marks a run that holds the
// whole phrase word for word.
// ordered marks a window where the matched key words keep the phrase's
// order and include its first key word.
type hit struct {
	matched int
	negated bool
	exact   bool
	ordered bool
}

// level ranks a hit, most alarming last.
type level int

const (
	levelNone    level = iota
	levelPartial       // unclear: half the key words
	levelNegated       // unclear: most key words, negated or scrambled
	levelVariant       // duress: most key words
	levelExact         // duress: the phrase word for word
)

func (p pattern) level(h hit) level {
	share := float64(h.matched) / float64(len(p.keys))
	switch {
	case h.exact && !h.negated:
		return levelExact
	case share >= matchShare && !h.negated && (h.matched == len(p.keys) || h.ordered):
		// Missing a key word is only a variant when the rest keep the
		// phrase's order and its lead word, so "la tía Carmen llegó tarde"
		// is not "dile a la tía Carmen que llego tarde".
		return levelVariant
	case share >= matchShare:
		return levelNegated
	case share >= partialShare && h.matched >= 2:
		return levelPartial
	default:
		return levelNone
	}
}

// better prefers the more alarming hit, then more key words, so a negated
// mention never hides a clean one elsewhere in the call.
func (p pattern) better(h, o hit) bool {
	if lh, lo := p.level(h), p.level(o); lh != lo {
		return lh > lo
	}
	return h.matched > o.matched
}

// bestHit slides a window of len(words)+windowSlack over run and returns the
// window with the most key words.
func (p pattern) bestHit(run []token) hit {
	size := len(p.words) + windowSlack
	best := hit{}
	for start := range run {
		end := min(start+size, len(run))
		h := p.window(run, start, end)
		if p.better(h, best) {
			best = h
		}
	}
	return best
}

// window counts the key words in run[start:end], each key and each token used
// once, and checks for a negation inside the matched span or just before it.
func (p pattern) window(run []token, start, end int) hit {
	used := make([]bool, end-start)
	first, last, matched, prev := -1, -1, 0, -1
	ordered := true
	for ki, k := range p.keys {
		found := false
		for i := start; i < end; i++ {
			if used[i-start] || !wordMatches(k, run[i]) {
				continue
			}
			used[i-start] = true
			matched++
			found = true
			if first < 0 || i < first {
				first = i
			}
			last = max(last, i)
			ordered = ordered && i > prev
			prev = i
			break
		}
		if ki == 0 && !found {
			ordered = false
		}
	}
	return hit{
		matched: matched,
		negated: matched > 0 && p.negatedAt(run, first, last, used, start),
		ordered: ordered,
	}
}

// negatedAt reports a negation inside run[first..last] that is not one of the
// matched key words, or right before first with only phrase words and no
// pause between ("no se me quemaron", not "No... se me quemaron"). A phrase
// that has its own negation is never negated.
func (p pattern) negatedAt(run []token, first, last int, used []bool, start int) bool {
	if p.negates {
		return false
	}
	for i := first; i <= last; i++ {
		if !used[i-start] && negations[run[i].text] {
			return true
		}
	}
	for i := first; i > 0 && !run[i].pause; i-- {
		if negations[run[i-1].text] {
			return true
		}
		if !p.inWords[run[i-1].text] {
			return false
		}
	}
	return false
}

// wordMatches is equality, a cut word that starts key ("car-" for "carmen"),
// or one edit away for words of five letters or more ("saludas", "saluda").
func wordMatches(key string, t token) bool {
	if t.text == key {
		return true
	}
	if t.cut && len(t.text) >= 3 && strings.HasPrefix(key, t.text) {
		return true
	}
	return len([]rune(key)) >= 5 && editDistanceAtMostOne(key, t.text)
}

func editDistanceAtMostOne(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}
	if len(ra)-len(rb) > 1 {
		return false
	}
	i, j, edits := 0, 0, 0
	for i < len(ra) && j < len(rb) {
		if ra[i] == rb[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		if len(ra) == len(rb) {
			j++
		}
		i++
	}
	return edits+(len(ra)-i) <= 1
}

// containsSeq reports whether words appear in run back to back.
func containsSeq(run []token, words []string) bool {
	for start := 0; start+len(words) <= len(run); start++ {
		ok := true
		for k, w := range words {
			if run[start+k].text != w {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
