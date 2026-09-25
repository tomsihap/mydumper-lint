package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL104", Name: "missing-final-newline", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "The file does not end with a newline.",
			Why: "mydumper's pre-processor only appends `= 1` to a line when it meets the `\\n` that ends " +
				"it. A flag such as `routines` on the last line of a file without a final newline stays " +
				"`routines`, which GLib rejects, so mydumper ignores the whole file.\n\nEven when the " +
				"last line is a key/value pair, a missing final newline is a latent risk: anything a " +
				"script appends to the file is glued to that line.",
			Refs: []string{"§3.2", "cases 02, 18"},
		},
		Check: func(p *Pass) {
			b := p.File.Bytes
			if len(b) == 0 || b[len(b)-1] == '\n' {
				return
			}
			n := len(p.File.Lines)
			d := diag.Diagnostic{Span: diag.Span{Start: len(b), End: len(b)}}
			if lc := p.KF.Lines[n-1]; lc.Kind == keyfile.KindRejected && lc.Cause == keyfile.CauseMissingFinalNewline {
				d.Message = "the last line has no newline, so mydumper's pre-processor does not append `= 1` to " +
					quote(content(p, n)) + " (" + glibSays(lc.Message) + ")"
				d.Consequence = rejectionConsequence(p)
			} else {
				d.Message = "file does not end with a newline"
				d.Consequence = "Anything appended to this file (for example a [client] section added by a " +
					"script) is glued to its last line."
			}
			// Adding a newline after a line of whitespace would turn it into "  = 1"
			// (MDL102 and MDL103 fix those lines instead), and after a trailing '\r'
			// it would make GLib strip that '\r' and change the line.
			if last := p.File.Content(n); !isBlankish(last) && last[len(last)-1] != '\r' {
				d.Fix = &diag.Fix{
					Applicability: diag.Safe,
					Description:   "Add a final newline",
					Edits:         []diag.Edit{{Start: len(b), End: len(b), New: "\n"}},
				}
			}
			p.Report(d)
		},
	})
}
