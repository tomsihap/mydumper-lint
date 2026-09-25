package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL109", Name: "unparsable-line", Family: FamilyLoading,
			Severity: diag.Error, Unsuppressible: true,
			Summary: "GLib rejects a line for a reason no more specific rule describes.",
			Why: "mydumper-lint emulates GLib exactly, so every line GLib would reject is reported. " +
				"This rule is the safety net for a rejection that none of MDL101–MDL111 explains; " +
				"please report it as a bug with the file so a specific rule can be added.",
			Refs: []string{"§6.2"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseUnknown, func(n int, lc keyfile.LineClass) {
				p.Report(diag.Diagnostic{
					Span:        lineSpan(p, n),
					Message:     "GLib rejects this line (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				})
			})
		},
	})
}
