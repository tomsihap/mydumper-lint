package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL309", Name: "trailing-whitespace", Family: FamilyValues,
			Severity: diag.Warning, Fix: diag.Safe,
			Summary: "A comment or group header ends with spaces or tabs.",
			Why: "GLib tolerates spaces after a group header and inside comments, but invisible " +
				"trailing whitespace is how whitespace-only lines (MDL102) sneak into a file, and it hides " +
				"real differences in reviews. Trailing whitespace in a value is a separate, more serious " +
				"problem: see MDL302.",
			Refs: []string{"K4", "case 40"},
		},
		Check: func(p *Pass) {
			for i, l := range p.File.Lines {
				n := i + 1
				c := content(p, n)
				lc := p.KF.Lines[i]
				s := firstSignificant(c)
				isComment := lc.Kind == keyfile.KindComment && s < len(c) && c[s] == '#'
				if lc.Kind != keyfile.KindGroup && !isComment {
					continue
				}
				t := len(bytes.TrimRight(c, " \t\r\f"))
				if t == len(c) {
					continue
				}
				msg := "trailing whitespace after the group header"
				if isComment {
					msg = "trailing whitespace in a comment"
				}
				if bytes.IndexByte(c[t:], '\r') >= 0 {
					msg += " (including carriage returns)"
				}
				p.Report(diag.Diagnostic{
					Span:    diag.Span{Start: l.Start + t, End: l.Start + len(c)},
					Message: msg,
					Fix: &diag.Fix{
						Applicability: diag.Safe,
						Description:   "Remove the trailing whitespace",
						Edits:         []diag.Edit{{Start: l.Start + t, End: l.Start + len(c)}},
					},
				})
			}
		},
	})
}
