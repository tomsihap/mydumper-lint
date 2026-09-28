package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL307", Name: "spaces-around-equals", Family: FamilyValues,
			Severity: diag.Info, Fix: diag.Safe,
			Summary: "Spaces around `=`.",
			Why: "GLib ignores spaces before `=` and at the start of the value, so `threads = 4` works. " +
				"Writing `threads=4` keeps files uniform and makes trailing spaces (MDL302), which GLib " +
				"does keep, easier to spot.",
			Refs: []string{"K8", "K9"},
		},
		Check: func(p *Pass) {
			for _, e := range p.KF.Entries {
				if e.Synthesized || e.EqOffset < 0 || !p.KF.Groups[e.Group].Valid {
					continue
				}
				b := p.File.Bytes
				start, end := e.KeySpan.End, e.ValueSpan.Start
				if e.ValueSpan.Empty() {
					end = e.EqOffset + 1
					for end < p.File.Line(e.Line).End && (b[end] == ' ' || b[end] == '\t') {
						end++
					}
				}
				if start == e.EqOffset && end == e.EqOffset+1 {
					continue
				}
				spaces := true
				for _, c := range b[start:end] {
					spaces = spaces && (c == ' ' || c == '\t' || c == '=')
				}
				if !spaces {
					continue
				}
				p.Report(diag.Diagnostic{
					Span:    diag.Span{Start: start, End: end},
					Message: "spaces around `=`",
					Fix: &diag.Fix{
						Applicability: diag.Safe,
						Description:   "Remove the spaces",
						Edits:         []diag.Edit{{Start: start, End: end, New: "="}},
					},
				})
			}
		},
	})
}
