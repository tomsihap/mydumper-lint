package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL301", Name: "duplicate-key", Family: FamilyValues,
			Severity: diag.Warning, Fix: diag.Unsafe,
			Summary: "The same key appears twice in a group.",
			Why: "GLib keeps both lines but returns the last value for both: mydumper passes the " +
				"option twice, with the last value each time. The first value is silently lost. It is " +
				"an error when the values differ, a warning when the line is merely repeated. The fix " +
				"deletes the earlier occurrences.",
			Refs: []string{"K13", "G11"},
		},
		Check: func(p *Pass) {
			type at struct{ group, key string }
			last := map[at]int{} // index in p.KF.Entries of the last occurrence
			for i, e := range p.KF.Entries {
				if p.KF.Groups[e.Group].Valid {
					last[at{p.KF.Groups[e.Group].Name, e.Key}] = i
				}
			}
			for i, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				j, ok := last[at{g.Name, e.Key}]
				if !g.Valid || !ok || j == i {
					continue
				}
				final := p.KF.Entries[j]
				d := diag.Diagnostic{
					Span: e.KeySpan,
					Message: fmt.Sprintf("`%s` is set again on line %d in [%s]: only the last value counts",
						e.Key, final.Line, g.Name),
					Related: []diag.Related{{Span: final.KeySpan, Message: "the value that counts"}},
					Fix: &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Delete this earlier occurrence",
						Edits:         []diag.Edit{removeLine(p, e.Line)},
					},
				}
				if e.Value != final.Value {
					d.Severity = diag.Error
					d.Consequence = fmt.Sprintf("The value %s on this line is lost: %s is used instead.",
						quote([]byte(e.Value)), quote([]byte(final.Value)))
				}
				p.Report(d)
			}
		},
	})
}
