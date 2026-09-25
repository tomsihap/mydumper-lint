package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL102", Name: "whitespace-only-line", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "A line contains only spaces or tabs.",
			Why: "Before handing the file to GLib, mydumper appends `= 1` to every line without `=`, so " +
				"that valueless flags such as `routines` get a value. A line of spaces becomes `  = 1`: " +
				"a key/value pair with an empty key. GLib rejects it, and mydumper silently ignores the " +
				"whole file.\n\nThe same line is harmless as the very last line of a file without a final " +
				"newline, because the rewrite only happens before a `\\n`. That is a trap: the file passes " +
				"every test on its own and breaks as soon as a script appends a block to it.",
			Refs: []string{"P0", "cases 01-04"},
			E2E:  []string{"mdl102-whitespace-line-drops-masking"},
		},
		Check: func(p *Pass) {
			for i, l := range p.File.Lines {
				n := i + 1
				c := p.File.Content(n)
				if len(c) == 0 || !isBlankish(c) || len(bytes.Trim(c, "\r")) == 0 {
					continue // empty, not whitespace, or carriage returns only (MDL103)
				}
				lc := p.KF.Lines[i]
				if lc.Kind != keyfile.KindRejected && !p.Preprocessor {
					continue // a comment for GLib when nothing appends "= 1" to it
				}
				d := diag.Diagnostic{
					Span: lineSpan(p, n),
					Fix: &diag.Fix{
						Applicability: diag.Safe,
						Description:   "Remove the whitespace",
						Edits:         []diag.Edit{{Start: l.Start, End: l.End}},
					},
				}
				switch {
				case lc.Kind == keyfile.KindRejected:
					d.Message = "line contains only whitespace: mydumper's pre-processor turns it into " +
						quote(append(append([]byte{}, c...), "= 1"...)) + ", a key/value pair with an empty key (" +
						glibSays(lc.Message) + ")"
					d.Consequence = rejectionConsequence(p)
				case !l.HasNewline:
					d.Message = "last line contains only whitespace and has no newline: harmless now, fatal as soon as anything is appended"
					d.Consequence = "If a newline or another block is appended to this file (for example a [client] " +
						"section added by a script), mydumper will ignore the entire file."
				default:
					d.Message = "line contains only whitespace: harmless here only because the line above keeps " +
						"mydumper's pre-processor from appending `= 1` to it"
					d.Consequence = "Any change to the line above can make mydumper ignore the entire file."
				}
				p.Report(d)
			}
		},
	})
}
