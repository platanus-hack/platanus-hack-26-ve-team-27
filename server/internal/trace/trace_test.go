package trace

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

func TestLineFormat(t *testing.T) {
	got := Line("9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", "call_placed",
		"call", int64(12), "conversation_id", "conv_01", "call_sid", "CA123", "place_ms", 840*time.Millisecond,
		"ok", true, "speed", 1.25, "reason", "the user said the duress phrase", "empty", "", "err", errors.New("boom: x"), "nil", nil, "dangling")
	want := `trace incident=9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f step=call_placed call=12 conversation_id=conv_01 call_sid=CA123 place_ms=840 ok=true speed=1.25 reason="the user said the duress phrase" empty=- err="boom: x" nil=- dangling=-`
	if got != want {
		t.Errorf("Line =\n%s\nwant\n%s", got, want)
	}
	if got := Line("", "verdict"); got != "trace incident=- step=verdict" {
		t.Errorf("no incident: %s", got)
	}
}

// A phone number never reaches a trace line, even one passed by mistake or
// inside an error.
func TestLineMasksPhoneNumbers(t *testing.T) {
	got := Line("x", "notify", "to", "+584141234567", "error", `twilio: "To" +584241112233 is not verified`, "id", "wamid.HBgMNTg0MjQxMTEyMjMzFQ")
	for _, n := range []string{"584141234567", "584241112233", "1234567"} {
		if strings.Contains(got, n) {
			t.Errorf("line carries %s: %s", n, got)
		}
	}
	if !strings.Contains(got, "to=···67") || !strings.Contains(got, "···33 is not verified") || !strings.Contains(got, "id=wamid.HBgMNTg0MjQxMTEyMjMzFQ") {
		t.Errorf("line = %s", got)
	}
}

func TestPhraseTurnsIgnoresCaseAccentsAndPunctuation(t *testing.T) {
	phrase := "Dile a la tía Carmen que llegó tarde"
	turns := []contract.TranscriptTurn{
		{Role: "agent", Message: "¿Le digo a la tía Carmen que llegas tarde?"}, // Mamá never counts
		{Role: "user", Message: "Hola mami, voy saliendo."},
		{Role: "user", Message: "Oye, DILE a la TIA carmen que LLEGO tarde."},
		{Role: "user", Message: "dile a la tia carmen... que [pausa] llegó, tarde"},
		{Role: "user", Message: "dile a carmen que llego tarde"}, // tía missing
	}
	if got := PhraseTurns(turns, phrase); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("PhraseTurns = %v, want [2 3]", got)
	}
	if got := KeyWords(phrase); !reflect.DeepEqual(got, []string{"dile", "tia", "carmen", "llego", "tarde"}) {
		t.Errorf("KeyWords = %v", got)
	}
	if PhraseTurns(turns, "") != nil {
		t.Error("an empty phrase matched")
	}
}

// A detector reason that quotes the person loses the phrase's key words.
func TestScrubDropsThePhrase(t *testing.T) {
	phrase := "dile a la tía Carmen que llego tarde"
	got := Scrub(`LLM: the user said "Dile a la TIA Carmen que llegó tarde" clearly`, phrase)
	for _, w := range []string{"Dile", "TIA", "Carmen", "llegó", "tarde"} {
		if strings.Contains(got, w) {
			t.Errorf("scrubbed reason keeps %q: %s", w, got)
		}
	}
	if !strings.Contains(got, "LLM: the user said") || !strings.Contains(got, "clearly") {
		t.Errorf("scrubbed too much: %s", got)
	}
	if got := Scrub("the user said the duress phrase", phrase); got != "the user said the duress phrase" {
		t.Errorf("a clean reason changed: %s", got)
	}
}

func TestTurns(t *testing.T) {
	u, a := Turns([]contract.TranscriptTurn{{Role: "agent"}, {Role: "user"}, {Role: "user"}})
	if u != 2 || a != 1 {
		t.Errorf("turns %d user, %d agent", u, a)
	}
}
