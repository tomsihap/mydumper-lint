package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL111", Name: "nul-byte", Family: FamilyLoading,
			Severity: diag.Error, Unsuppressible: true,
			Summary: "A NUL byte makes a line unparsable.",
			Why: "GLib reads each line as a C string, which ends at the first NUL byte. A NUL before the " +
				"`=` of a key/value pair, or inside a `[group]` header, hides the rest of the line: GLib " +
				"sees an incomplete line, rejects the file, and mydumper ignores all of it. NUL bytes " +
				"usually come from a bad copy/paste or a binary file saved with a .cnf extension.",
			Refs: []string{"K17", "cases 56, 67"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseNulByte, func(n int, lc keyfile.LineClass) {
				c := p.File.Content(n)
				i := bytes.IndexByte(c, 0)
				start := p.File.Line(n).Start + i
				p.Report(diag.Diagnostic{
					Span: diag.Span{Start: start, End: start + 1},
					Message: "NUL byte: GLib stops reading the line here and only sees " +
						quote(c[firstSignificant(c):i]) + " (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				})
			})
		},
	})
}
