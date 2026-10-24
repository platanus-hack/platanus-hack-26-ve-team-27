package main

import (
	"fmt"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// predError is the prediction column for a detector call that errored
// (only the LLM-only run can: Rules and Chain never error).
const predError = "error"

// columns are the prediction columns of the confusion matrix, in order.
var columns = []string{string(contract.VerdictDuress), string(contract.VerdictUnclear), string(contract.VerdictNoDuress), predError}

// outcome is one dialog's label and what a detector said.
type outcome struct {
	ID     string
	Duress bool   // label
	Pred   string // a contract.Verdict or predError
	Reason string
}

// rates is precision and recall, positive class = duress.
type rates struct {
	TP, FP, FN, TN    int
	Precision, Recall float64
}

// score is a detector's whole report.
type score struct {
	// Matrix[label][pred] counts dialogs; label is "duress" or "no_duress".
	Matrix map[string]map[string]int
	// Strict alerts only on duress. Alert also alerts on unclear, which is
	// what the family would see if the notifier forwards unclear calls.
	// An error never alerts.
	Strict, Alert rates
	// Missed (label duress, not alerted) and False (label no duress,
	// alerted) list ids under Strict and under Alert.
	StrictMissed, StrictFalse, AlertMissed, AlertFalse []string
	Unclear, Errors                                    []string
}

func label(duress bool) string {
	if duress {
		return string(contract.VerdictDuress)
	}
	return string(contract.VerdictNoDuress)
}

// scoreOutcomes is pure, so it is unit-tested with a small fixture.
func scoreOutcomes(outs []outcome) score {
	s := score{Matrix: map[string]map[string]int{
		string(contract.VerdictDuress):   {},
		string(contract.VerdictNoDuress): {},
	}}
	for _, o := range outs {
		s.Matrix[label(o.Duress)][o.Pred]++
		switch o.Pred {
		case string(contract.VerdictUnclear):
			s.Unclear = append(s.Unclear, o.ID)
		case predError:
			s.Errors = append(s.Errors, o.ID)
		}
		strict := o.Pred == string(contract.VerdictDuress)
		alert := strict || o.Pred == string(contract.VerdictUnclear)
		s.Strict.add(o.Duress, strict, o.ID, &s.StrictMissed, &s.StrictFalse)
		s.Alert.add(o.Duress, alert, o.ID, &s.AlertMissed, &s.AlertFalse)
	}
	s.Strict.finish()
	s.Alert.finish()
	return s
}

func (r *rates) add(truth, alerted bool, id string, missed, falseAlarm *[]string) {
	switch {
	case truth && alerted:
		r.TP++
	case alerted:
		r.FP++
		*falseAlarm = append(*falseAlarm, id)
	case truth:
		r.FN++
		*missed = append(*missed, id)
	default:
		r.TN++
	}
}

// finish computes the ratios. With no predicted positives precision is 1
// (no false alarm was raised); with no labeled positives recall is 1.
func (r *rates) finish() {
	r.Precision, r.Recall = 1, 1
	if r.TP+r.FP > 0 {
		r.Precision = float64(r.TP) / float64(r.TP+r.FP)
	}
	if r.TP+r.FN > 0 {
		r.Recall = float64(r.TP) / float64(r.TP+r.FN)
	}
}

// markdown renders s as the section of results.md for one detector.
func (s score) markdown(name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", name)
	b.WriteString("| label \\ verdict | " + strings.Join(columns, " | ") + " |\n")
	b.WriteString("| --- |" + strings.Repeat(" ---: |", len(columns)) + "\n")
	for _, l := range []string{string(contract.VerdictDuress), string(contract.VerdictNoDuress)} {
		fmt.Fprintf(&b, "| %s |", l)
		for _, c := range columns {
			fmt.Fprintf(&b, " %d |", s.Matrix[l][c])
		}
		b.WriteString("\n")
	}
	b.WriteString("\n| counting | TP | FP | FN | TN | precision | recall |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, row := range []struct {
		name string
		r    rates
	}{{"strict (alert on duress only)", s.Strict}, {"alert (alert on duress or unclear)", s.Alert}} {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %.3f | %.3f |\n",
			row.name, row.r.TP, row.r.FP, row.r.FN, row.r.TN, row.r.Precision, row.r.Recall)
	}
	b.WriteString("\n")
	for _, l := range []struct {
		name string
		ids  []string
	}{
		{"Missed duress, strict", s.StrictMissed},
		{"False alarms, strict", s.StrictFalse},
		{"Missed duress, alert", s.AlertMissed},
		{"False alarms, alert", s.AlertFalse},
		{"Unclear", s.Unclear},
		{"Errors", s.Errors},
	} {
		ids := "none"
		if len(l.ids) > 0 {
			ids = strings.Join(l.ids, ", ")
		}
		fmt.Fprintf(&b, "- %s: %s\n", l.name, ids)
	}
	return b.String()
}

// fichaFields are the call-context fields the ficha eval checks: the facts.
// situacion and confianza are shown, not scored.
var fichaFields = []string{"lugar", "personas", "vehiculo", "rumbo"}

// fichaRow is one call-context dialog and what the extraction returned.
type fichaRow struct {
	dialog duress.EvalFichaDialog
	ctx    contract.CallContext
	err    error
}

func fichaField(c contract.CallContext, name string) string {
	switch name {
	case "lugar":
		return c.Lugar
	case "personas":
		return c.Personas
	case "vehiculo":
		return c.Vehiculo
	case "rumbo":
		return c.Rumbo
	}
	return ""
}

// fichaMarkdown renders the call-context section: per dialog, the extracted
// fields, and over all dialogs how many labeled-unknown fields were filled
// anyway (invented) and how many labeled-known fields were missed. An
// extraction error counts as every field unknown.
func fichaMarkdown(rows []fichaRow) string {
	var b strings.Builder
	b.WriteString("| id | lugar | personas | vehiculo | rumbo | situacion | confianza | invented | missed |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | ---: |\n")
	invented, missed, checks, errs := 0, 0, 0, 0
	for _, r := range rows {
		known := map[string]bool{}
		for _, k := range r.dialog.Known {
			known[k] = true
		}
		var inv, mis []string
		for _, f := range fichaFields {
			checks++
			got := r.err == nil && duress.ContextKnown(fichaField(r.ctx, f))
			switch {
			case got && !known[f]:
				inv = append(inv, f)
			case !got && known[f]:
				mis = append(mis, f)
			}
		}
		invented += len(inv)
		missed += len(mis)
		cell := func(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
		if r.err != nil {
			errs++
			fmt.Fprintf(&b, "| %s | error: %s | | | | | | %d | %d |\n", r.dialog.ID, cell(r.err.Error()), len(inv), len(mis))
			continue
		}
		c := r.ctx
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", r.dialog.ID,
			cell(c.Lugar), cell(c.Personas), cell(c.Vehiculo), cell(c.Rumbo), cell(c.Situacion), c.Confianza,
			countList(inv), countList(mis))
	}
	fmt.Fprintf(&b, "\n- Field checks: %d (%s over %d dialogs)\n", checks, strings.Join(fichaFields, ", "), len(rows))
	fmt.Fprintf(&b, "- Invented (labeled unknown, extracted anyway): %d\n", invented)
	fmt.Fprintf(&b, "- Missed (labeled known, extracted as no se sabe): %d\n", missed)
	fmt.Fprintf(&b, "- Extraction errors: %d\n", errs)
	return b.String()
}

func countList(fields []string) string {
	if len(fields) == 0 {
		return "0"
	}
	return fmt.Sprintf("%d (%s)", len(fields), strings.Join(fields, ", "))
}
