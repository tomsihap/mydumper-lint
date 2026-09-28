package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL501", Name: "unknown-table-key", Family: FamilyTables,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A key of a table section that mydumper does not know.",
			Why: "In a table section mydumper reads a fixed set of keys (`where`, `limit`, `rows`, " +
				"`columns_on_select`, …, which depend on the version) and masked columns written " +
				"`` `column` ``. Any other key is ignored without a word: a misspelled `wehre` dumps the " +
				"whole table.",
			Refs: []string{"F8"},
		},
		Check: func(p *Pass) {
			if p.Target == nil {
				return
			}
			var keys []string
			if l, ok := p.Target.(interface{ TableKeys() []string }); ok {
				keys = l.TableKeys()
			}
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				if !g.Valid || p.groupKind(e.Group) != model.GroupTable || e.Key == "" || e.Key[0] == '`' || p.Target.TableKey(e.Key) {
					continue // masked columns and malformed ones (MDL505) aside
				}
				msg := fmt.Sprintf("`%s` is not a table key mydumper knows", e.Key)
				if p.Version != "" {
					msg += " in " + p.Version
				}
				best, bestD, ties := "", 3, 0
				for _, k := range keys {
					switch d := Levenshtein(e.Key, k); {
					case d < bestD:
						best, bestD, ties = k, d, 1
					case d == bestD:
						ties++
					}
				}
				d := diag.Diagnostic{
					Span:        e.KeySpan,
					Message:     msg,
					Consequence: fmt.Sprintf("mydumper ignores it: [%s] behaves as if the line were absent.", g.Name),
				}
				if best != "" {
					d.Message += fmt.Sprintf(": did you mean `%s`?", best)
					if ties == 1 {
						d.Fix = &diag.Fix{
							Applicability: diag.Unsafe,
							Description:   "Rename the key to `" + best + "`",
							Edits:         []diag.Edit{{Start: e.KeySpan.Start, End: e.KeySpan.End, New: best}},
						}
					}
				}
				p.Report(d)
			}
		},
	})
}
