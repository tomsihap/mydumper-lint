package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL203", Name: "group-name-whitespace", Family: FamilyGroups,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A group name starts or ends with spaces.",
			Why: "GLib keeps the spaces inside the brackets: [ mydumper ] is the group \" mydumper \", " +
				"which nobody reads.",
			Refs: []string{"K5"},
		},
		Check: func(p *Pass) {
			for _, g := range validHeaders(p) {
				trimmed := strings.TrimSpace(g.Name)
				// The MySQL client library trims group names: [ client ] works.
				if trimmed == g.Name || trimmed == "" || lowerASCII(trimmed) == "client" ||
					(!knownGroup(p, trimmed) && !knownGroup(p, lowerASCII(trimmed))) {
					continue
				}
				p.Report(diag.Diagnostic{
					Span:        g.NameSpan,
					Message:     fmt.Sprintf("the group name %q has spaces around it: it is not [%s]", g.Name, trimmed),
					Consequence: "Nobody reads this group: every key in it is ignored.",
					Fix: &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Remove the spaces",
						Edits:         []diag.Edit{{Start: g.NameSpan.Start, End: g.NameSpan.End, New: trimmed}},
					},
				})
			}
		},
	})
}
