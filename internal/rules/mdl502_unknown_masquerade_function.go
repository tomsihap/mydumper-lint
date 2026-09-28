package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL502", Name: "unknown-masquerade-function", Family: FamilyTables,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A masked column's value names no masking function of the version.",
			Why: "mydumper selects the masking function by the start of the value (`random_string`, " +
				"`constant`, …; `null` only from v0.21.4-1). When none matches, it logs " +
				"`Function not found: Using default`, visible only at `--verbose 3`, and uses its " +
				"identity function: up to v0.21.2-4 the column is dumped unmasked, in plaintext; from " +
				"v0.21.3-1 it is dumped empty. Either way the dump succeeds and looks normal.",
			Refs: []string{"F10"},
			E2E:  []string{"mdl502-unknown-masquerade-function-plaintext", "mdl502-null-function-by-version"},
		},
		Check: func(p *Pass) {
			if p.Target == nil {
				return
			}
			functions := p.Target.MasqueradeFunctions()
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || !model.IsTableGroup(g.Name) || !model.IsMaskedColumn(e.Key) ||
					model.MasqueradeFunction(e.Value, functions) != "" {
					continue
				}
				first, _, _ := strings.Cut(e.Value, " ")
				d := diag.Diagnostic{Span: e.ValueSpan, Consequence: identityConsequence(p)}
				if e.Value == "" {
					d.Span = e.KeySpan
					d.Message = fmt.Sprintf("the masked column %s has no masking function", e.Key)
				} else {
					d.Message = fmt.Sprintf("`%s` is not a masking function", first)
					if p.Version != "" {
						d.Message += " in " + p.Version
					}
					best, bestD, ties := "", 3, 0
					for _, f := range functions {
						switch dist := Levenshtein(first, f); {
						case dist < bestD:
							best, bestD, ties = f, dist, 1
						case dist == bestD:
							ties++
						}
					}
					if best != "" {
						d.Message += fmt.Sprintf(": did you mean `%s`?", best)
						if ties == 1 {
							d.Fix = &diag.Fix{
								Applicability: diag.Unsafe,
								Description:   "Use `" + best + "`",
								Edits:         []diag.Edit{{Start: e.ValueSpan.Start, End: e.ValueSpan.Start + len(first), New: best}},
							}
						}
					}
				}
				p.Report(d)
			}
		},
	})
}
