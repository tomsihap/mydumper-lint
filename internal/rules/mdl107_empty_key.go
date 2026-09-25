package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL107", Name: "empty-key", Family: FamilyLoading,
			Severity: diag.Error, Unsuppressible: true,
			Summary: "A line starts with `=`: the key is empty.",
			Why: "GLib treats a line as a key/value pair only if there is a key before the `=`. A line " +
				"such as `=1` is rejected, and mydumper ignores the whole file.",
			Refs: []string{"K7", "case 23"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseEmptyKey, func(n int, lc keyfile.LineClass) {
				p.Report(diag.Diagnostic{
					Span:        lineSpan(p, n),
					Message:     "line starts with `=`: there is no key before it (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				})
			})
		},
	})
}
