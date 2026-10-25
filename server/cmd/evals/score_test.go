package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

func TestScoreOutcomes(t *testing.T) {
	s := scoreOutcomes([]outcome{
		{ID: "a", Duress: true, Pred: "duress"},     // TP both
		{ID: "b", Duress: true, Pred: "unclear"},    // FN strict, TP alert
		{ID: "c", Duress: true, Pred: "no_duress"},  // FN both
		{ID: "d", Duress: false, Pred: "duress"},    // FP both
		{ID: "e", Duress: false, Pred: "unclear"},   // TN strict, FP alert
		{ID: "f", Duress: false, Pred: "no_duress"}, // TN both
		{ID: "g", Duress: false, Pred: "error"},     // TN both, listed as error
	})

	if got := (rates{TP: 1, FP: 1, FN: 2, TN: 3, Precision: 0.5, Recall: 1.0 / 3}); s.Strict != got {
		t.Errorf("strict = %+v, want %+v", s.Strict, got)
	}
	if got := (rates{TP: 2, FP: 2, FN: 1, TN: 2, Precision: 0.5, Recall: 2.0 / 3}); s.Alert != got {
		t.Errorf("alert = %+v, want %+v", s.Alert, got)
	}
	lists := map[string][2][]string{
		"strict missed": {s.StrictMissed, {"b", "c"}},
		"strict false":  {s.StrictFalse, {"d"}},
		"alert missed":  {s.AlertMissed, {"c"}},
		"alert false":   {s.AlertFalse, {"d", "e"}},
		"unclear":       {s.Unclear, {"b", "e"}},
		"errors":        {s.Errors, {"g"}},
	}
	for name, l := range lists {
		if !reflect.DeepEqual(l[0], l[1]) {
			t.Errorf("%s = %v, want %v", name, l[0], l[1])
		}
	}
	if s.Matrix["duress"]["unclear"] != 1 || s.Matrix["no_duress"]["error"] != 1 || s.Matrix["no_duress"]["no_duress"] != 1 {
		t.Errorf("matrix = %v", s.Matrix)
	}

	md := s.markdown("Fixture")
	for _, want := range []string{"### Fixture", "| duress | 1 | 1 | 1 | 0 |", "| no_duress | 1 | 1 | 1 | 1 |",
		"| strict (alert on duress only) | 1 | 1 | 2 | 3 | 0.500 | 0.333 |", "- Errors: g", "- Missed duress, alert: c"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown misses %q:\n%s", want, md)
		}
	}
}

func TestScoreWithNoPositives(t *testing.T) {
	s := scoreOutcomes([]outcome{{ID: "a", Duress: false, Pred: "no_duress"}})
	if s.Strict.Precision != 1 || s.Strict.Recall != 1 {
		t.Errorf("got %+v, want 1/1 when nothing is positive", s.Strict)
	}
}

func TestFichaMarkdownCountsInventedAndMissed(t *testing.T) {
	unknown := contract.CallContextUnknown
	rows := []fichaRow{
		{dialog: duress.EvalFichaDialog{ID: "f1", Known: []string{"lugar", "personas"}},
			ctx: contract.CallContext{Lugar: "la autopista", Personas: unknown, Vehiculo: "un carro", Rumbo: unknown,
				Situacion: "va en carro", Confianza: "media"}},
		{dialog: duress.EvalFichaDialog{ID: "f2", Known: []string{"vehiculo"}}, err: errors.New("timeout")},
	}
	got := fichaMarkdown(rows)
	for _, want := range []string{
		"| f1 | la autopista | no se sabe | un carro | no se sabe | va en carro | media | 1 (vehiculo) | 1 (personas) |",
		"| f2 | error: timeout |",
		"- Field checks: 8", "- Invented (labeled unknown, extracted anyway): 1", "- Missed (labeled known, extracted as no se sabe): 2",
		"- Extraction errors: 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fichaMarkdown misses %q:\n%s", want, got)
		}
	}
}
