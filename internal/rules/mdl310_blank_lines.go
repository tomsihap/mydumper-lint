package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL310", Name: "blank-lines", Family: FamilyValues,
			Severity: diag.Info, Fix: diag.Safe,
			Summary: "Several consecutive empty lines, or empty lines at the end of the file.",
			Why: "Empty lines are harmless to GLib, but every empty line is a place where mydumper's " +
				"pre-processor can produce a fatal `= 1` when the line above changes (see MDL108). One " +
				"empty line between sections is enough.",
			Refs: []string{"case 32"},
		},
		Check: func(p *Pass) {
			lines := p.File.Lines
			blank := func(i int) bool {
				return lines[i].HasNewline && lines[i].End == lines[i].Start && p.KF.Lines[i].Kind == keyfile.KindBlank
			}
			for i := 0; i < len(lines); {
				if !blank(i) {
					i++
					continue
				}
				j := i
				for j < len(lines) && blank(j) {
					j++
				}
				switch {
				case j == len(lines):
					p.Report(diag.Diagnostic{
						Span:    diag.Span{Start: lines[i].Start, End: len(p.File.Bytes)},
						Message: fmt.Sprintf("%d empty %s at the end of the file", j-i, plural(j-i, "line", "lines")),
						Fix: &diag.Fix{
							Applicability: diag.Safe,
							Description:   "Remove the empty lines at the end of the file",
							Edits:         []diag.Edit{{Start: lines[i].Start, End: len(p.File.Bytes)}},
						},
					})
				case j-i >= 2:
					start, end := lines[i+1].Start, lines[j-1].End+1
					p.Report(diag.Diagnostic{
						Span:    diag.Span{Start: start, End: end},
						Message: fmt.Sprintf("%d consecutive empty lines", j-i),
						Fix: &diag.Fix{
							Applicability: diag.Safe,
							Description:   "Keep a single empty line",
							Edits:         []diag.Edit{{Start: start, End: end}},
						},
					})
				}
				i = j
			}
		},
	})
}
