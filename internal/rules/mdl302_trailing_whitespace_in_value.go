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
			ID: "MDL302", Name: "trailing-whitespace-in-value", Family: FamilyValues,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A value ends with spaces or tabs, which GLib keeps.",
			Why: "GLib strips the spaces before a value but keeps those after it. An integer option " +
				"such as `threads=4 ` then fails to parse and mydumper aborts at startup; a path such as " +
				"`outputdir=/backup ` names a directory with a trailing space; a regular expression " +
				"matches something else. The fix trims the value; it is unsafe because it changes what " +
				"mydumper reads (promote it with `fix.extend-safe` when trailing spaces are never " +
				"intended).\n\nSQL-valued keys of table sections (`where`, `columns_on_select`, ...) " +
				"are not checked: trailing spaces are harmless in SQL.",
			Refs: []string{"K9", "G5"},
		},
		Check: func(p *Pass) {
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			options := map[int]*optionKey{}
			keys := optionKeys(p)
			for i := range keys {
				options[keys[i].e.Line] = &keys[i]
			}
			for _, e := range p.KF.Entries {
				t := strings.TrimRight(e.Value, " \t")
				if t == e.Value || !p.KF.Groups[e.Group].Valid {
					continue
				}
				consequence := "The whitespace becomes part of the value."
				switch kinds[p.KF.Groups[e.Group].Name] {
				case model.GroupToolOptions, model.GroupProductOptions:
					if k := options[e.Line]; k != nil && k.entry != nil {
						if k.entry.NoArg() {
							continue // the value is ignored anyway (MDL402)
						}
						if msg := k.ctx.Check(k.ref, k.name(), e.Value); msg != "" {
							consequence = fatalConsequence(k, msg)
						}
					}
				case model.GroupTable:
					if sqlKey(e.Key) {
						continue
					}
				case model.GroupUnknown, model.GroupSessionVariables, model.GroupGlobalVariables, model.GroupClient:
					continue // nobody reads it, the server trims it, or libmysqlclient does
				}
				p.Report(diag.Diagnostic{
					Span:        diag.Span{Start: e.ValueSpan.Start + len(t), End: e.ValueSpan.End},
					Message:     fmt.Sprintf("the value of `%s` ends with whitespace, which GLib keeps", e.Key),
					Consequence: consequence,
					Fix: &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Remove the trailing whitespace",
						Edits:         []diag.Edit{{Start: e.ValueSpan.Start + len(t), End: e.ValueSpan.End}},
					},
				})
			}
		},
	})
}

// sqlKey reports whether a table-section key holds SQL (design MDL506).
func sqlKey(key string) bool {
	switch key {
	case "where", "columns_on_select", "columns_on_insert":
		return true
	}
	return strings.HasPrefix(key, "columns_on_select_replace")
}
