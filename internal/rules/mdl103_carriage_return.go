package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL103", Name: "carriage-return", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "The file contains carriage returns (Windows CRLF line endings).",
			Why: "GLib removes a `\\r` right before `\\n`, so key/value lines with CRLF endings are read " +
				"correctly. mydumper's pre-processor does not: it counts the `\\r` as content. An empty " +
				"line (`\\r\\n`) therefore becomes `\\r= 1`, a key/value pair with an empty key, and GLib " +
				"rejects the whole file. A CRLF file is one empty line away from being ignored.\n\nA " +
				"carriage return that is not part of a line ending stays in the text GLib returns, for " +
				"example at the end of a value.",
			Refs: []string{"K1", "cases 05, 36, 45, 47"},
		},
		Check: func(p *Pass) {
			var crlf []int
			rejected := false
			for i, l := range p.File.Lines {
				n := i + 1
				c := p.File.Content(n)
				stray := false
				for j, ch := range c {
					if ch != '\r' {
						continue
					}
					if j == len(c)-1 && l.HasNewline {
						// GLib strips exactly one '\r' before '\n': removing it is
						// safe only when it is the only trailing '\r'.
						if j == 0 || c[j-1] != '\r' {
							crlf = append(crlf, l.Start+j)
						}
						continue
					}
					if stray {
						continue // one report per line
					}
					stray = true
					p.Report(diag.Diagnostic{
						Severity: diag.Warning,
						Span:     diag.Span{Start: l.Start + j, End: l.Start + j + 1},
						Message:  "carriage return that is not part of a line ending: GLib keeps it in the text",
						Consequence: "The carriage return becomes part of the key, value or name on this line, " +
							"which then no longer matches what you wrote.",
					})
				}
				if lc := p.KF.Lines[i]; lc.Kind == keyfile.KindRejected && lc.Cause == keyfile.CauseCarriageReturn {
					rejected = true
					p.Report(diag.Diagnostic{
						Span: diag.Span{Start: l.Start, End: l.End},
						Message: "empty line with a Windows (CRLF) line ending: mydumper's pre-processor turns it " +
							"into `\\r= 1`, a key/value pair with an empty key (" + glibSays(lc.Message) + ")",
						Consequence: rejectionConsequence(p),
						Fix: &diag.Fix{
							Applicability: diag.Safe,
							Description:   "Remove the carriage returns of this empty line",
							Edits:         []diag.Edit{{Start: l.Start, End: l.End}},
						},
					})
				}
			}
			if len(crlf) == 0 {
				return
			}
			edits := make([]diag.Edit, len(crlf))
			for i, off := range crlf {
				edits[i] = diag.Edit{Start: off, End: off + 1}
			}
			consequence := "GLib strips the `\\r` before `\\n`, so the values are read correctly today, but a " +
				"single empty line added to this file would make mydumper ignore all of it."
			if rejected {
				consequence = rejectionConsequence(p)
			}
			p.Report(diag.Diagnostic{
				Span:        diag.Span{Start: crlf[0], End: crlf[0] + 1},
				Message:     fmt.Sprintf("file uses Windows (CRLF) line endings (%d %s)", len(crlf), plural(len(crlf), "line", "lines")),
				Consequence: consequence,
				Fix: &diag.Fix{
					Applicability: diag.Safe,
					Description:   "Convert the line endings to LF",
					Edits:         edits,
				},
			})
		},
	})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
