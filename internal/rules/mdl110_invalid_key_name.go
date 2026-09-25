package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL110", Name: "invalid-key-name", Family: FamilyLoading,
			Severity: diag.Error, Unsuppressible: true,
			Summary: "A key contains `]`, or a `[` that is not a valid locale suffix.",
			Why: "GLib validates key names: `]` is forbidden, and `[` is only allowed to start a final " +
				"locale suffix such as `name[fr]`, made of letters, digits, `-`, `_`, `.` and `@`. Keys " +
				"such as `foo]`, `foo[bar baz]` or a masked column named `` `c[0]` `` make GLib reject " +
				"the file (\"Invalid key name\"), and mydumper ignores all of it.",
			Refs: []string{"K19", "cases 50, 51, 61"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseInvalidKeyName, func(n int, lc keyfile.LineClass) {
				c := content(p, n)
				key := c[firstSignificant(c):]
				if i := bytes.IndexByte(key, '='); i >= 0 {
					key = bytes.TrimRight(key[:i], " \t\f\r")
				}
				p.Report(diag.Diagnostic{
					Span: lineSpan(p, n),
					Message: "invalid key name " + quote(key) + ": `]` is not allowed, and `[` may only start a " +
						"final locale suffix made of letters, digits, `-`, `_`, `.` and `@` (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				})
			})
		},
	})
}
