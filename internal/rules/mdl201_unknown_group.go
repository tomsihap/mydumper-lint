package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL201", Name: "unknown-group", Family: FamilyGroups,
			Severity: diag.Warning,
			Summary:  "A group that neither mydumper, myloader nor the MySQL client library reads.",
			Why: "mydumper reads [mydumper], myloader reads [myloader], both read their variable " +
				"groups ([mydumper_session_variables]) and table sections ([`db`.`table`]); the MySQL " +
				"client library reads [client]. mydumper reads its per-product option groups " +
				"([mydumper_mysql_8_0]) from v0.21.2-2 only, and myloader never reads " +
				"[myloader_<product>] groups. Any other group is silently ignored, keys included. " +
				"[mydumper_variables] and [myloader_variables] were renamed [*_session_variables] in v0.14.0-1.",
			Refs: []string{"§3.6", "F16"},
			E2E:  []string{"f16-product-option-group-mydumper", "f16-product-option-group-myloader"},
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
					if tool, ok := model.ProductOptionGroup(name, readers(p).Products); ok {
						msg += fmt.Sprintf(": %s %s does not read per-product option groups (mydumper reads them from v0.21.2-2, myloader never)", tool, p.Version)
						break
					}
					msg += fmt.Sprintf(": the product is not one mydumper knows (%s)", strings.Join(readers(p).Products, ", "))
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
