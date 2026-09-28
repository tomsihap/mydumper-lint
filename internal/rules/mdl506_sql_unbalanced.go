package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL506", Name: "sql-unbalanced", Family: FamilyTables,
			Severity: diag.Error,
			Summary:  "An SQL value has unbalanced parentheses, quotes or comments.",
			Why: "mydumper pastes `where`, `columns_on_select`, `columns_on_insert` and " +
				"`columns_on_select_replace*` into the SQL it sends. An unclosed quote, parenthesis or " +
				"`/* */` comment makes the statement fail when the table is reached, after the dump has " +
				"started, or swallows the rest of it.",
		},
		Check: func(p *Pass) {
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			for _, e := range p.KF.Entries {
				g := p.KF.Groups[e.Group]
				kind := kinds[g.Name]
				isSQL := (kind == model.GroupTable && sqlKey(e.Key)) ||
					((kind == model.GroupToolOptions || kind == model.GroupProductOptions) && e.Key == "where")
				if !g.Valid || !isSQL {
					continue
				}
				if at, what := sqlUnbalanced(e.Value); at >= 0 {
					p.Report(diag.Diagnostic{
						Span:        diag.Span{Start: e.ValueSpan.Start + at, End: e.ValueSpan.Start + at + 1},
						Message:     fmt.Sprintf("the SQL of `%s` has %s", e.Key, what),
						Consequence: "The statement mydumper builds fails when the table is reached.",
					})
				}
			}
		},
	})
}

// sqlUnbalanced returns the offset of the first unbalanced element of an
// SQL fragment and a description, or -1. It knows '…', "…" and `…` quotes
// (doubled or backslash-escaped), -- and # comments, /* */ comments and
// parentheses.
func sqlUnbalanced(s string) (int, string) {
	var open []int
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for ; j < len(s); j++ {
				if s[j] == '\\' && c != '`' {
					j++
					continue
				}
				if s[j] == c {
					if j+1 < len(s) && s[j+1] == c {
						j++
						continue
					}
					break
				}
			}
			if j >= len(s) {
				return i, fmt.Sprintf("an unclosed %c quote", c)
			}
			i = j
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := indexFrom(s, "*/", i+2)
			if end < 0 {
				return i, "an unclosed /* comment"
			}
			i = end + 1
		case c == '#', c == '-' && i+2 < len(s) && s[i+1] == '-' && (s[i+2] == ' ' || s[i+2] == '\t'):
			i = len(s) // the rest of the line is a comment
		case c == '(':
			open = append(open, i)
		case c == ')':
			if len(open) == 0 {
				return i, "a `)` without `(`"
			}
			open = open[:len(open)-1]
		}
	}
	if len(open) > 0 {
		return open[len(open)-1], "a `(` without `)`"
	}
	return -1, ""
}

func indexFrom(s, sub string, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
