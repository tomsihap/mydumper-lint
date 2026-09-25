package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL306", Name: "leading-whitespace", Family: FamilyValues,
			Severity: diag.Info, Fix: diag.Safe,
			Summary: "A key, comment or group header is indented.",
			Why: "GLib ignores indentation, so it is harmless for keys and comments. An indented group " +
				"header is not: mydumper's pre-processor does not reset its state after it, and an empty " +
				"line below it makes the file unloadable (see MDL108 and MDL112). Keeping every line " +
				"flush left avoids the trap.",
			Refs: []string{"K2", "cases 15, 16, 17"},
		},
		Check: func(p *Pass) {
			for i, l := range p.File.Lines {
				n := i + 1
				c := content(p, n)
				j := 0
				for j < len(c) && (c[j] == ' ' || c[j] == '\t') {
					j++
				}
				lc := p.KF.Lines[i]
				if j == 0 || j == len(c) || lc.Kind == keyfile.KindRejected {
					continue
				}
				msg := "indented line"
				switch lc.Kind {
				case keyfile.KindGroup:
					msg = "indented group header: mydumper's pre-processor does not reset its state after it"
				case keyfile.KindComment:
					msg = "indented comment"
				case keyfile.KindEntry:
					msg = "indented key"
				case keyfile.KindBlank, keyfile.KindRejected: // excluded above
				}
				p.Report(diag.Diagnostic{
					Span:    diag.Span{Start: l.Start, End: l.Start + j},
					Message: msg,
					Fix: &diag.Fix{
						Applicability: diag.Safe,
						Description:   "Remove the indentation",
						Edits:         []diag.Edit{{Start: l.Start, End: l.Start + j}},
					},
				})
			}
		},
	})
}
