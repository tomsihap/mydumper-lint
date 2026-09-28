package rules

import (
	"fmt"
	"slices"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL511", Name: "table-sections-lost", Family: FamilyTables,
			Severity: diag.Error,
			Summary:  "This mydumper version ignores every table section, masking included.",
			Why: "mydumper v0.21.2-2 and v0.21.2-3 read the table sections before creating the " +
				"tables that store them: GLib refuses every insertion (a `GLib-CRITICAL` line on " +
				"stderr) and the dump goes on without any per-table setting. Masked columns are " +
				"dumped in plaintext; `where`, `limit` and the other table keys do not apply. " +
				"myloader is not affected. Upgrade mydumper (v0.21.2-4 fixes it), or pin another " +
				"version. Reported as a warning for sections without masked columns.",
			Refs: []string{"F17"},
			E2E:  []string{"mdl511-table-sections-lost"},
		},
		Check: func(p *Pass) {
			if p.Target == nil || !slices.Contains(p.Target.TableSectionsIgnored(), "mydumper") || !p.KF.Loadable {
				return
			}
			headers := validHeaders(p)
			hasTool := map[string]bool{}
			for _, g := range headers {
				hasTool[g.Name] = true
			}
			if hasTool["myloader"] && !hasTool["mydumper"] {
				return // a myloader file: myloader reads its table sections
			}
			masked := map[string]bool{}
			for _, e := range p.KF.Entries {
				if model.IsMaskedColumn(e.Key) {
					masked[p.KF.Groups[e.Group].Name] = true
				}
			}
			seen := map[string]bool{}
			for _, g := range headers {
				if !model.IsTableGroup(g.Name) || seen[g.Name] {
					continue
				}
				seen[g.Name] = true
				d := diag.Diagnostic{
					Span: g.NameSpan,
					Message: fmt.Sprintf("mydumper %s ignores this table section: it reads table sections "+
						"before creating the tables that store them", p.Version),
					Consequence: "None of its settings apply to the dump; myloader still reads them.",
				}
				if masked[g.Name] {
					d.Consequence = "Its masked columns are dumped in plaintext, and its other settings do not apply."
				} else {
					d.Severity = diag.Warning
				}
				p.Report(d)
			}
		},
	})
}
