package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL201", Name: "unknown-group", Family: FamilyGroups,
			Severity: diag.Warning,
			Summary:  "A group that neither mydumper, myloader nor the MySQL client library reads.",
			Why: "mydumper reads [mydumper], myloader reads [myloader], both read their per-product " +
				"groups ([mydumper_mysql_8_0]), their variable groups ([mydumper_session_variables]) " +
				"and table sections ([`db`.`table`]); the MySQL client library reads [client]. Any other " +
				"group is silently ignored, keys included. [mydumper_variables] and " +
				"[myloader_variables] were renamed [*_session_variables] in v0.14.0-1.",
			Refs: []string{"§3.6"},
		},
		Check: func(p *Pass) {
			for _, g := range validHeaders(p) {
				name := g.Name
				if knownGroup(p, name) || looksLikeTable(name) || knownGroup(p, lowerASCII(name)) ||
					knownGroup(p, strings.TrimSpace(name)) {
					continue // MDL204, MDL202, MDL203
				}
				msg := fmt.Sprintf("[%s] is read by nobody", name)
				switch {
				case name == "mydumper_variables" || name == "myloader_variables":
					msg += fmt.Sprintf(": it was renamed [%s_session_variables] in v0.14.0-1", strings.TrimSuffix(name, "_variables"))
				case productLike(name):
					msg += fmt.Sprintf(": the product is not one mydumper knows (%s)", strings.Join(knownProducts(p), ", "))
				default:
					best, bestD := "", 3
					for _, c := range groupNames {
						if d := Levenshtein(lowerASCII(name), c); d < bestD {
							best, bestD = c, d
						}
					}
					if best != "" {
						msg += fmt.Sprintf(": did you mean [%s]?", best)
					}
				}
				p.Report(diag.Diagnostic{
					Span:        g.NameSpan,
					Message:     msg,
					Consequence: "Every key of this group is ignored.",
				})
			}
		},
	})
}

// productLike reports whether a name has the shape of a per-product group
// (mydumper_<product>…).
func productLike(name string) bool {
	for _, tool := range []string{"mydumper_", "myloader_"} {
		if rest, ok := strings.CutPrefix(name, tool); ok && rest != "" && !strings.Contains(rest, "variables") {
			return true
		}
	}
	return false
}

func knownProducts(p *Pass) []string {
	if ps := products(p); ps != nil {
		return ps
	}
	return []string{"mysql", "percona", "mariadb", "tidb", "rds", "google", "clickhouse", "dolt", "unknown"}
}
