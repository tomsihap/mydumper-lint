package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL105", Name: "key-before-group", Family: FamilyLoading,
			Severity: diag.Error, Unsuppressible: true,
			Summary: "A key/value pair appears before the first [group] header.",
			Why: "Every key must belong to a group such as `[mydumper]` or `[myloader]`. GLib rejects a key " +
				"before the first header (\"Key file does not start with a group\"), and mydumper then " +
				"ignores the whole file.",
			Refs: []string{"K6", "case 08"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseKeyBeforeGroup, func(n int, lc keyfile.LineClass) {
				c := content(p, n)
				key := c[firstSignificant(c):]
				if i := bytes.IndexByte(key, '='); i >= 0 {
					key = bytes.TrimRight(key[:i], " \t")
				}
				p.Report(diag.Diagnostic{
					Span: lineSpan(p, n),
					Message: quote(key) + " is defined before any [group] header; move it under [mydumper] " +
						"or [myloader] (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				})
			})
		},
	})
}
