package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL402", Name: "boolean-flag-value", Family: FamilyOptions,
			Severity: diag.Error, Fix: diag.Unsafe,
			Summary: "An option that takes no value is given one that looks like `false`.",
			Why: "Options such as `routines` or `no-data` take no value: mydumper passes `--routines 0`, " +
				"GLib sets the option and leaves `0` aside. `routines=0`, `routines=false` and " +
				"`routines=off` therefore all ENABLE routines. Options flagged reverse behave the same " +
				"way: their presence applies them, whatever the value.\n\nTo disable an option, remove the " +
				"line (the fix comments it out). Values such as `true` or `yes` are harmless but " +
				"misleading (info); any other value is ignored too (warning).",
			Refs: []string{"G1", "G2", "G12"},
			E2E:  []string{"mdl402-routines-zero-dumps-routines", "mdl402-no-data-zero-skips-data"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				if k.entry == nil || !k.entry.NoArg() || !k.effective() {
					continue
				}
				v := k.ke.Value
				if v == "" || v == "1" || (len(v) >= 2 && v[0] == '-') {
					continue // "-x" values are MDL408's
				}
				start, end := k.valueSpan()
				d := diag.Diagnostic{Span: diag.Span{Start: start, End: end}}
				name := k.entry.Long
				switch lowerASCII(v) {
				case "0", "false", "no", "off":
					d.Message = fmt.Sprintf("`%s=%s` turns %s on: the option takes no value, so GLib ignores `%s`", k.e.Key, v, name, v)
					d.Consequence = fmt.Sprintf("%s takes effect, the opposite of what `%s` says.", name, v) + productNote(k.g)
					d.Fix = &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Comment the line out to leave the option off",
						Edits:         []diag.Edit{commentOut(p, k.e.Line)},
					}
				case "true", "yes", "on":
					d.Severity = diag.Info
					d.Message = fmt.Sprintf("`%s` takes no value: `%s` is ignored; write `%s=1`", name, v, k.e.Key)
				default:
					d.Severity = diag.Warning
					d.Message = fmt.Sprintf("`%s` takes no value: `%s` is ignored and the option takes effect", name, v)
				}
				p.Report(d)
			}
		},
	})
}
