package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL404", Name: "empty-option-value", Family: FamilyOptions,
			Severity: diag.Warning,
			Summary:  "An option that expects a value is given an empty one.",
			Why: "`key=` passes an empty value. For a numeric option GLib rejects it and mydumper " +
				"aborts at startup (error). For other options the value is set to the empty string, " +
				"which is rarely what was meant: remove the line to keep the default (warning).",
			Refs: []string{"G7"},
			E2E:  []string{"mdl404-empty-integer-fatal"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				if k.entry == nil || k.entry.NoArg() || !k.effective() || k.ke.Value != "" {
					continue
				}
				d := diag.Diagnostic{
					Span:    k.ke.KeySpan,
					Message: fmt.Sprintf("`%s` expects a value but is given an empty one", k.e.Key),
					Consequence: fmt.Sprintf("%s gets an empty value instead of its default.", k.entry.Long) +
						productNote(k.g),
				}
				if msg := k.ctx.Check(k.ref, k.name(), ""); msg != "" {
					d.Severity = diag.Error
					d.Consequence = fatalConsequence(&k, msg)
				}
				p.Report(d)
			}
		},
	})
}
