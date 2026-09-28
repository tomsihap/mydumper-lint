package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL507", Name: "columns-count-mismatch", Family: FamilyTables,
			Severity: diag.Warning,
			Summary:  "`columns_on_select` and `columns_on_insert` list different numbers of columns.",
			Why: "mydumper selects the columns of `columns_on_select` and inserts them into those of " +
				"`columns_on_insert`: with different counts, every INSERT of the dump fails at restore " +
				"time.",
		},
		Check: func(p *Pass) {
			type pair struct{ sel, ins *keyfile.Entry }
			sections := map[string]*pair{}
			var order []string
			for i := range p.KF.Entries {
				e := &p.KF.Entries[i]
				g := p.KF.Groups[e.Group]
				if !g.Valid || !model.IsTableGroup(g.Name) {
					continue
				}
				s := sections[g.Name]
				if s == nil {
					s = &pair{}
					sections[g.Name] = s
					order = append(order, g.Name)
				}
				switch e.Key {
				case "columns_on_select":
					s.sel = e
				case "columns_on_insert":
					s.ins = e
				}
			}
			for _, name := range order {
				s := sections[name]
				if s.sel == nil || s.ins == nil {
					continue
				}
				ns, ni := topLevelColumns(s.sel.Value), topLevelColumns(s.ins.Value)
				if ns == ni || ns == 0 || ni == 0 {
					continue
				}
				p.Report(diag.Diagnostic{
					Span:        s.ins.ValueSpan,
					Message:     fmt.Sprintf("columns_on_insert lists %d columns, columns_on_select %d", ni, ns),
					Consequence: "Every INSERT of this table's dump fails at restore time.",
					Related:     []diag.Related{{Span: s.sel.ValueSpan, Message: "columns_on_select"}},
				})
			}
		},
	})
}

// topLevelColumns counts the comma-separated items of an SQL list, ignoring
// commas inside parentheses and quotes; 0 for an unbalanced list (MDL506).
func topLevelColumns(s string) int {
	if at, _ := sqlUnbalanced(s); at >= 0 {
		return 0
	}
	n, depth, empty := 1, 0, true
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"', '`':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' && c != '`' {
					i++
				}
			}
			empty = false
		case '(':
			depth++
			empty = false
		case ')':
			depth--
		case ',':
			if depth == 0 {
				n++
			}
		case ' ', '\t':
		default:
			empty = false
		}
	}
	if empty {
		return 0
	}
	return n
}
