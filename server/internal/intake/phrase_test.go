package intake

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// numbers holds 21 words in 142 characters, so its first 20 and 21 words
// test the word cap while staying under the character cap.
var numbers = strings.Fields("uno dos tres cuatro cinco seis siete ocho nueve diez once doce trece catorce " +
	"quince dieciséis diecisiete dieciocho diecinueve veinte veintiuno")

func firstWords(n int) string { return strings.Join(numbers[:n], " ") }

// distinctWords returns n different words of prefix plus two letters, none a
// stopword, so every one is a key word the detector checks for typos.
func distinctWords(prefix string, n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = prefix + string(rune('a'+i%26)) + string(rune('a'+i/26%26))
	}
	return strings.Join(words, " ")
}

// A phrase at a cap passes; one more character or word is rejected with an
// error that names the field and the caps, never the phrase. Words are runs
// of letters or digits, as the detector splits them, so punctuation does not
// hide them.
func TestValidateSetupCapsThePhrases(t *testing.T) {
	atRunes, overRunes := strings.Repeat("ñ", maxPhraseRunes), strings.Repeat("ñ", maxPhraseRunes+1)
	cases := []struct {
		name, duress, safe string
		field              string // the field the error names; "" when it passes
	}{
		{"duress phrase at the character cap", atRunes, "", ""},
		{"duress phrase over the character cap", overRunes, "", "duress_phrase"},
		{"duress phrase at the word cap", firstWords(maxPhraseWords), "", ""},
		{"duress phrase over the word cap", firstWords(maxPhraseWords + 1), "", "duress_phrase"},
		{"safe phrase at the character cap", samplePhrase, atRunes, ""},
		{"safe phrase over the character cap", samplePhrase, overRunes, "safe_phrase"},
		{"safe phrase at the word cap", samplePhrase, firstWords(maxPhraseWords), ""},
		{"safe phrase over the word cap", samplePhrase, firstWords(maxPhraseWords + 1), "safe_phrase"},
		{"safe phrase over the word cap without spaces", samplePhrase, strings.Repeat("sí.", maxPhraseWords+1), "safe_phrase"},
	}
	for _, c := range cases {
		err := validateSetup(contract.DeviceRegistration{DuressPhrase: c.duress, SafePhrase: c.safe})
		want := ""
		if c.field != "" {
			want = c.field + " must be at most 150 characters and 20 words"
		}
		if got := errText(err); got != want {
			t.Errorf("%s: validateSetup = %q, want %q", c.name, got, want)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Long phrases are a 422 before the detector or any query runs, so a lazy
// pool is enough. Two 500-word phrases used to cost the detector about 2 s
// of CPU per request.
func TestLongPhrasesAre422WithoutDB(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://invalid.invalid/none")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mux := http.NewServeMux()
	New(pool, LogHandoff{}, LogHandoff{}).Register(mux)

	body := func(duressPhrase, safe string) string {
		return `{"device_id":"x","phone_number":"+584141234567","family_numbers":[],"duress_phrase":"` + duressPhrase +
			`","safe_phrase":"` + safe + `"}`
	}
	cases := map[string]string{
		"duress phrase over the word cap": body(firstWords(maxPhraseWords+1), "ya llegué a la casa"),
		"safe phrase over the word cap":   body(samplePhrase, firstWords(maxPhraseWords+1)),
		"both of 500 words":               body(distinctWords("dura", 500), distinctWords("segu", 500)),
	}
	for name, b := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(b)))
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "must be at most") {
			t.Errorf("%s: status = %d %s, want 422 naming the caps", name, rec.Code, rec.Body)
		}
	}
}

// The costliest phrases the caps let through: a duress phrase of 20 six-letter
// key words that share a prefix, and a safe phrase of 20 near misses of them,
// so the detector's typo check walks most of every word and never stops early
// on a match.
var (
	duressAtCaps = distinctWords("xyzw", maxPhraseWords)
	safeAtCaps   = strings.TrimSpace(strings.Repeat("xyzwzz ", maxPhraseWords))
)

// At the caps one setup check takes under a millisecond; uncapped, one took
// seconds. The bound is loose so a slow machine or -race passes.
func TestSetupCheckAtTheCapsIsFast(t *testing.T) {
	if err := validateSetup(contract.DeviceRegistration{DuressPhrase: duressAtCaps, SafePhrase: safeAtCaps}); err != nil {
		t.Fatalf("phrases at the caps rejected: %v", err)
	}
	start := time.Now()
	for range 10 {
		tooCloseToDuress(safeAtCaps, duressAtCaps)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("10 setup checks at the caps took %v, want a few milliseconds", elapsed)
	}
}

// BenchmarkDetectAtTheCaps times the detector on phrases at the caps in both
// places it runs: the setup check, and the verdict on a long call (3,000
// words is about 20 minutes of the person talking).
func BenchmarkDetectAtTheCaps(b *testing.B) {
	b.Run("setup check", func(b *testing.B) {
		for b.Loop() {
			tooCloseToDuress(safeAtCaps, duressAtCaps)
		}
	})
	b.Run("3000-word call", func(b *testing.B) {
		call := []contract.TranscriptTurn{{Role: duress.RoleUser, Message: strings.Repeat("xyzwzz ", 3000)}}
		for b.Loop() {
			_, _ = duress.Rules{}.Detect(context.Background(), call, duress.Phrases{Duress: duressAtCaps})
		}
	})
}
