package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL202", Name: "group-name-case", Family: FamilyGroups,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A group name matches a known group only when case is ignored.",
			Why: "mydumper looks groups up with `g_key_file_has_group`, which is case-sensitive: " +
				"[MyDumper] is never read, and every option in it is silently lost. [client] is not " +
				"reported: the MySQL client library reads it whatever the case.",
			Refs: []string{"F5", "case 34"},
		},
		Check: func(p *Pass) {
			for _, g := range validHeaders(p) {
				lower := lowerASCII(g.Name)
				// The MySQL client library matches group names ignoring case:
				// [CLIENT] still works for the connection.
				if lower == g.Name || lower == "client" || knownGroup(p, g.Name) || !knownGroup(p, lower) {
					continue
				}
				_, tool := model.ClassifyGroup(lower, products(p))
				if tool == "" {
					tool = "mydumper"
				}
				p.Report(diag.Diagnostic{
					Span:        g.NameSpan,
					Message:     fmt.Sprintf("[%s] is not [%s]: group names are case-sensitive", g.Name, lower),
					Consequence: tool + " never reads this group: every key in it is ignored.",
					Fix: &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   fmt.Sprintf("Rename the group to [%s]", lower),
						Edits:         []diag.Edit{{Start: g.NameSpan.Start, End: g.NameSpan.End, New: lower}},
					},
				})
			}
		},
	})
}
