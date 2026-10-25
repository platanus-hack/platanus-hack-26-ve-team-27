// Package trace writes one structured log line per step of an incident, so
// Render's logs read as a trace of a real call:
//
//	trace incident=<id> step=<step> k=v k=v …
//
// Steps: alert_received, call_requested, call_placed, call_failed,
// call_retry, call_outcome, twilio_status, webhook_received,
// webhook_rejected, verdict, notify, kapso_status, recording_stored, sealed.
//
// A trace line never carries what was said, the duress or safe phrase, or a
// phone number: callers pass counts, ids, masked numbers and flags, and
// Line masks any E.164 number that slips through anyway. PhraseTurns is a
// display-only matcher for "the phrase was found in turn N"; the verdict is
// still the duress detector's.
package trace

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Log writes Line(incident, step, kv...) to the standard logger.
func Log(incident, step string, kv ...any) { log.Print(Line(incident, step, kv...)) }

// Line is one trace line. kv alternates keys and values; a value is printed
// bare when it is a plain token, quoted otherwise, "-" when empty or nil,
// bools as true/false, durations in whole milliseconds. Any E.164 number in
// a value is masked to its last two digits.
func Line(incident, step string, kv ...any) string {
	var b strings.Builder
	b.WriteString("trace incident=")
	b.WriteString(value(incident))
	b.WriteString(" step=")
	b.WriteString(value(step))
	for i := 0; i < len(kv); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(kv[i]))
		b.WriteByte('=')
		if i+1 < len(kv) {
			b.WriteString(value(kv[i+1]))
		} else {
			b.WriteString("-")
		}
	}
	return b.String()
}

// e164 is an E.164 phone number: + and 7 to 15 digits.
var e164 = regexp.MustCompile(`\+\d{7,15}`)

// bare is a value that needs no quotes.
var bare = regexp.MustCompile(`^[A-Za-z0-9_.:/@+·*\-]+$`)

func value(v any) string {
	var s string
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		s = t
	case error:
		if t == nil {
			return "-"
		}
		s = t.Error()
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case *float64:
		if t == nil {
			return "-"
		}
		return strconv.FormatFloat(*t, 'f', 2, 64)
	case time.Duration:
		return strconv.FormatInt(t.Milliseconds(), 10)
	case time.Time:
		if t.IsZero() {
			return "-"
		}
		return t.UTC().Format(time.RFC3339Nano)
	default:
		s = fmt.Sprint(t)
	}
	s = e164.ReplaceAllStringFunc(s, func(m string) string { return "···" + m[len(m)-2:] })
	if s == "" {
		return "-"
	}
	if bare.MatchString(s) {
		return s
	}
	return strconv.Quote(s)
}

// ---------------------------------------------------------------- the phrase, for display only

// Words are a text's words, lowercased, with Spanish accents folded and
// punctuation and [bracketed tags] dropped: "¿Olvidó?" is "olvido".
func Words(s string) []string {
	var out []string
	var w strings.Builder
	depth := 0
	flush := func() {
		if w.Len() > 0 {
			out = append(out, w.String())
			w.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '[':
			depth++
			flush()
		case r == ']' && depth > 0:
			depth--
		case depth > 0:
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			w.WriteRune(fold(r))
		default:
			flush()
		}
	}
	flush()
	return out
}

func fold(r rune) rune {
	r = unicode.ToLower(r)
	switch r {
	case 'á', 'à', 'ä', 'â':
		return 'a'
	case 'é', 'è', 'ë', 'ê':
		return 'e'
	case 'í', 'ì', 'ï', 'î':
		return 'i'
	case 'ó', 'ò', 'ö', 'ô':
		return 'o'
	case 'ú', 'ù', 'ü', 'û':
		return 'u'
	}
	return r
}

// stopwords are Spanish function words: they never count as key words.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("a al con de del el en la las le les lo los me mi mis o para por que se su sus te tu tus un una unos unas y ya es") {
		stopwords[w] = true
	}
}

// KeyWords are a phrase's distinct words that are not stopwords, in order;
// all its words when every one is a stopword.
func KeyWords(phrase string) []string {
	all := Words(phrase)
	var keys []string
	seen := map[string]bool{}
	for _, w := range all {
		if !stopwords[w] && !seen[w] {
			keys = append(keys, w)
			seen[w] = true
		}
	}
	if len(keys) == 0 {
		return all
	}
	return keys
}

// PhraseTurns are the indexes of the user turns that hold every key word of
// phrase, in any case, with or without accents. It is a diagnostic for the
// trace and the trace page, not the detector: the rules also weigh word
// order, near misses and negation, and the verdict is theirs.
func PhraseTurns(transcript []contract.TranscriptTurn, phrase string) []int {
	keys := KeyWords(phrase)
	if len(keys) == 0 {
		return nil
	}
	var out []int
	for i, t := range transcript {
		if t.Role != "user" {
			continue
		}
		have := map[string]bool{}
		for _, w := range Words(t.Message) {
			have[w] = true
		}
		all := true
		for _, k := range keys {
			if !have[k] {
				all = false
				break
			}
		}
		if all {
			out = append(out, i)
		}
	}
	return out
}

// Scrub replaces each of the phrase's key words (three letters or more) in
// s with "…", in any case and with or without accents, so a detector's
// reason that quotes the person can go in a log line.
func Scrub(s, phrase string) string {
	keys := map[string]bool{}
	for _, k := range KeyWords(phrase) {
		if len([]rune(k)) >= 3 {
			keys[k] = true
		}
	}
	if len(keys) == 0 {
		return s
	}
	var b, w strings.Builder
	flush := func() {
		if w.Len() == 0 {
			return
		}
		word := w.String()
		if ws := Words(word); len(ws) == 1 && keys[ws[0]] {
			b.WriteString("…")
		} else {
			b.WriteString(word)
		}
		w.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			w.WriteRune(r)
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

// Turns counts a transcript's user and agent turns.
func Turns(transcript []contract.TranscriptTurn) (user, agent int) {
	for _, t := range transcript {
		if t.Role == "agent" {
			agent++
		} else {
			user++
		}
	}
	return user, agent
}
