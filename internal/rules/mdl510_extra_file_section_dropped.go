package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL510", Name: "extra-file-section-dropped", Family: FamilyTables,
			Severity: diag.Error,
			Summary:  "The defaults file is rejected, so the extra file's table sections and variable groups are ignored.",
			Why: "mydumper and myloader merge the extra file (`--defaults-extra-file`) into the defaults " +
				"file, then read table sections, server variable groups and per-product option groups " +
				"from the merged file only. When GLib rejects the defaults file there is nothing to merge " +
				"into: these sections of the extra file are silently ignored, masking included, while its " +
				"[mydumper] or [myloader] options still apply. Fix the defaults file. Only load sets " +
				"(design §9.3) let mydumper-lint see both files.",
			Refs: []string{"F15", "F1"},
			E2E:  []string{"mdl510-defaults-rejected-drops-extra-masking", "mdl510-defaults-rejected-extra-options-still-apply"},
		},
		Check: func(*Pass) {},
		CheckSet: func(s *Set) {
			d, x := s.Defaults, s.Extra
			if d.KF.Loadable || !x.KF.Loadable {
				return
			}
			masked := map[string]bool{}
			for _, e := range x.KF.Entries {
				if model.IsMaskedColumn(e.Key) {
					masked[x.KF.Groups[e.Group].Name] = true
				}
			}
			seen := map[string]bool{}
			for gi, g := range x.KF.Groups {
				if seen[g.Name] {
					continue
				}
				seen[g.Name] = true
				var what, consequence string
				switch x.groupKind(gi) {
				case model.GroupTable:
					what, consequence = "table section", "None of its settings apply."
					if masked[g.Name] {
						consequence = "Its masked columns are dumped in plaintext, and its other settings do not apply."
					}
				case model.GroupSessionVariables, model.GroupGlobalVariables:
					what, consequence = "variable group", "These server variables are not set."
				case model.GroupProductOptions:
					what, consequence = "per-product option group", "These options do not apply."
				case model.GroupUnknown, model.GroupToolOptions, model.GroupClient:
					continue
				}
				s.Report(x, diag.Diagnostic{
					Span: g.NameSpan,
					Message: fmt.Sprintf("this %s is ignored: it is read from the defaults file merged with this one, "+
						"and GLib rejects %s (line %d)", what, d.File.Path, d.KF.FirstError.Line),
					Consequence: consequence,
				})
			}
		},
	})
}
