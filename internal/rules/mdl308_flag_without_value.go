package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL308", Name: "flag-without-value", Family: FamilyValues,
			Severity: diag.Info, Fix: diag.Safe,
			Summary: "An option written without `=`, such as `routines`.",
			Why: "`routines` alone works because mydumper's pre-processor rewrites it into " +
				"`routines= 1` before GLib reads the file. That rewrite is what the loading traps rely " +
				"on: at the end of a file without a final newline, or after a line whose `[` leaks the " +
				"pre-processor's state, the flag loses its `= 1` and the whole file is rejected; " +
				"mydumper v0.19.1-x has no pre-processor at all (MDL113). `routines=1` works everywhere.",
			Refs: []string{"§3.2", "cases 12, 18"},
			E2E:  []string{"mdl308-bare-flag-accepted-from-v0.19.3"},
		},
		Check: func(p *Pass) {
			kinds := map[string]model.GroupKind{}
			for _, g := range p.Model.Groups {
				kinds[g.Name] = g.Kind
			}
			known := map[int]bool{} // lines whose key names an option of the target
			for _, k := range optionKeys(p) {
				known[k.e.Line] = k.entry != nil
			}
			for _, e := range p.KF.Entries {
				kind := kinds[p.KF.Groups[e.Group].Name]
				if !e.Synthesized || !p.KF.Groups[e.Group].Valid || !isOptionName([]byte(e.Key)) ||
					(kind != model.GroupToolOptions && kind != model.GroupProductOptions) {
					continue
				}
				if isKnown, checked := known[e.Line]; checked && !isKnown {
					continue // an unknown option: MDL401
				}
				at := p.File.Line(e.Line).Start + len(content(p, e.Line))
				p.Report(diag.Diagnostic{
					Span:    e.KeySpan,
					Message: fmt.Sprintf("`%s` relies on mydumper's pre-processor to get its value: write `%s=1`", e.Key, e.Key),
					Fix: &diag.Fix{
						Applicability: diag.Safe,
						Description:   "Write the value explicitly (`" + e.Key + "=1`)",
						Edits:         []diag.Edit{{Start: at, End: at, New: "=1"}},
					},
				})
			}
		},
	})
}
