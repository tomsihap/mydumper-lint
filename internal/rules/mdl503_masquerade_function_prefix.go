package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL503", Name: "masquerade-function-prefix", Family: FamilyTables,
			Severity: diag.Warning,
			Summary:  "A masking function is selected by a prefix of a longer word.",
			Why: "mydumper compares only the start of the value: `constantly 'x'` selects `constant`, " +
				"`random_intx` selects `random_int`. mydumper then reads the arguments at a fixed " +
				"offset, so the extra letters become part of them.",
			Refs: []string{"F10"},
		},
		Check: func(p *Pass) {
			if p.Target == nil {
				return
			}
			functions := p.Target.MasqueradeFunctions()
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || !model.IsTableGroup(g.Name) || !model.IsMaskedColumn(e.Key) {
					continue
				}
				fn := model.MasqueradeFunction(e.Value, functions)
				if fn == "" || len(e.Value) == len(fn) || e.Value[len(fn)] == ' ' {
					continue
				}
				end := len(fn)
				for end < len(e.Value) && e.Value[end] != ' ' {
					end++
				}
				p.Report(diag.Diagnostic{
					Span:        diag.Span{Start: e.ValueSpan.Start, End: e.ValueSpan.Start + end},
					Message:     fmt.Sprintf("`%s` selects the masking function `%s`: mydumper only compares the start of the value", e.Value[:end], fn),
					Consequence: fmt.Sprintf("The masking uses `%s`; the rest of the word is read as its arguments.", fn),
				})
			}
		},
	})
}
