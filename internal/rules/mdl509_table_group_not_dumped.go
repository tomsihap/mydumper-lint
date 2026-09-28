package rules

import (
	"fmt"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL509", Name: "table-group-not-dumped", Family: FamilyTables,
			Severity: diag.Info, OptIn: true,
			Summary: "A table section names a table the `regex` of [mydumper] excludes.",
			Why: "mydumper dumps only the tables whose `db.table` matches `regex`. A table section " +
				"for another table is dead: often a leftover of a copied file, or a sign the regex is " +
				"wrong. In a load set, the regex of the extra file wins over the defaults file's, and " +
				"applies to the table sections of both files. " +
				"Opt-in, because the regex is often set on the command line instead.",
		},
		Check: func(p *Pass) {
			if re, value := mydumperRegex(p.KF); re != nil {
				reportExcludedTables(p, p.Report, re, value)
			}
		},
		CheckSet: func(s *Set) {
			// The last regex parsed wins (F15): the extra file's, else the
			// defaults file's. Each file's own regex is checked by Check.
			if re, value := mydumperRegex(s.Extra.KF); re != nil && s.Extra.KF.Loadable {
				if s.Defaults.KF.Loadable {
					reportExcludedTables(s.Defaults, func(d diag.Diagnostic) { s.Report(s.Defaults, d) }, re, value+" (from "+s.Extra.File.Path+")")
				}
				return
			}
			if re, value := mydumperRegex(s.Defaults.KF); re != nil && s.Defaults.KF.Loadable {
				reportExcludedTables(s.Extra, func(d diag.Diagnostic) { s.Report(s.Extra, d) }, re, value+" (from "+s.Defaults.File.Path+")")
			}
		},
	})
}

// mydumperRegex returns the last regex of [mydumper], compiled.
func mydumperRegex(kf *keyfile.Result) (*regexp2.Regexp, string) {
	var value string
	for _, e := range kf.Entries {
		if g := kf.Groups[e.Group]; g.Valid && g.Name == "mydumper" && e.Key == "regex" {
			value = e.Value
		}
	}
	if value == "" {
		return nil, ""
	}
	re, err := regexp2.Compile(value, regexp2.None)
	if err != nil {
		return nil, ""
	}
	return re, value
}

func reportExcludedTables(p *Pass, report func(diag.Diagnostic), re *regexp2.Regexp, value string) {
	for _, g := range validHeaders(p) {
		if !model.IsTableGroup(g.Name) {
			continue
		}
		parts := strings.SplitN(g.Name, "`.`", 2)
		db, table := strings.Trim(parts[0], "`"), strings.Trim(parts[1], "`")
		if db == "" || table == "" {
			continue // a whole database or every database: depends on the tables
		}
		if m, err := re.FindStringMatch(db + "." + table); err == nil && m == nil {
			report(diag.Diagnostic{
				Span:        g.NameSpan,
				Message:     fmt.Sprintf("%s.%s does not match the regex %s: mydumper never dumps it", db, table, value),
				Consequence: "The settings of this section never apply.",
			})
		}
	}
}
