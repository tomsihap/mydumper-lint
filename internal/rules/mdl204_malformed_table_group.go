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
			ID: "MDL204", Name: "malformed-table-group", Family: FamilyGroups,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "A group looks like a table section but is not written ``[`db`.`table`]``.",
			Why: "mydumper only treats a group as a table section when its name starts with a " +
				"backtick, contains `` `.` `` and ends with a backtick: ``[`db`.`table`]``, " +
				"``[`db`.``]`` (every table of db) or ``[``.`table`]``. [db.table] or " +
				"``[`db.table`]`` are ignored, with their `where`, `rows` and masking settings: the " +
				"masked columns are dumped in plaintext.",
			Refs: []string{"F7"},
		},
		Check: func(p *Pass) {
			for _, g := range validHeaders(p) {
				name := g.Name
				if !looksLikeTable(name) || model.IsTableGroup(name) || knownGroup(p, name) {
					continue
				}
				d := diag.Diagnostic{
					Span:        g.NameSpan,
					Message:     fmt.Sprintf("[%s] is not a table section: write it [`db`.`table`], each name in backticks", name),
					Consequence: "mydumper ignores this section: its where, rows and masking settings do not apply.",
				}
				if hasMasking(p, name) {
					d.Consequence += " The masked columns are dumped in plaintext."
				}
				if fixed, ok := tableGroupName(name); ok {
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   fmt.Sprintf("Rename the section to [%s]", fixed),
						Edits:         []diag.Edit{{Start: g.NameSpan.Start, End: g.NameSpan.End, New: fixed}},
					}
				}
				p.Report(d)
			}
		},
	})
}

// tableGroupName rewrites db.table, `db.table` or `db`.table as
// `db`.`table` when that is unambiguous: exactly one dot and no backtick
// inside the names.
func tableGroupName(name string) (string, bool) {
	bare := strings.ReplaceAll(name, "`", "")
	db, table, ok := strings.Cut(bare, ".")
	if !ok || strings.Contains(table, ".") || strings.TrimSpace(db) != db || strings.TrimSpace(table) != table ||
		(db == "" && table == "") {
		return "", false
	}
	return "`" + db + "`.`" + table + "`", true
}

// hasMasking reports whether a group of that name declares a masked column.
func hasMasking(p *Pass, group string) bool {
	for _, e := range p.KF.Entries {
		if p.KF.Groups[e.Group].Name == group && isMaskedColumnKey(e.Key) {
			return true
		}
	}
	return false
}
