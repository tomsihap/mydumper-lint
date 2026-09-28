package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL408", Name: "value-parsed-as-option", Family: FamilyOptions,
			Severity: diag.Error,
			Summary:  "A value starting with `-` is parsed as an option.",
			Why: "An option that takes no value (`routines`), or whose value is optional (`compress`), " +
				"does not consume a value starting with `-`: GLib parses it as options of its own. " +
				"`routines=-t` sets `-t` (threads), which then takes the next key, such as `--outputdir`, " +
				"as its value. `routines=--` is worse: `--` ends option parsing, and every following key " +
				"of the group is silently ignored.",
			Refs: []string{"G8", "G9"},
			E2E:  []string{"mdl408-value-dash-dash"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				v := k.ke.Value
				if k.entry == nil || !k.effective() || len(v) < 2 || v[0] != '-' || (!k.entry.NoArg() && !k.entry.OptionalArg()) {
					continue
				}
				start, end := k.valueSpan()
				d := diag.Diagnostic{Span: diag.Span{Start: start, End: end}}
				takes := "takes no value"
				if k.entry.OptionalArg() {
					takes = "has an optional value, which cannot start with `-`"
				}
				run, el := k.g.GOption, k.e.Element+1
				if v == "--" {
					d.Message = fmt.Sprintf("`%s` %s, so GLib reads `--` as the end of the options", k.entry.Long, takes)
					var dropped []string
					for _, e := range k.g.Entries {
						if e.Reason == model.ReasonAfterEndOfOptions {
							dropped = append(dropped, "`"+e.Key+"`")
						}
					}
					d.Consequence = fmt.Sprintf("%s ignores every key after this one in [%s]", k.g.Tool, k.g.Name)
					if len(dropped) > 0 {
						d.Consequence += ": " + strings.Join(dropped, ", ")
					}
					d.Consequence += "." + productNote(k.g)
					p.Report(d)
					continue
				}
				d.Message = fmt.Sprintf("`%s` %s, so GLib parses %s as an option", k.entry.Long, takes, quote([]byte(v)))
				d.Consequence = parsedAsOption(run, &k, el)
				p.Report(d)
			}
		},
	})
}

// parsedAsOption describes what GLib did with a value parsed as an option.
func parsedAsOption(run *model.GOptionRun, k *optionKey, el int) string {
	res := &run.Result
	if !res.OK && res.ErrorAt == el {
		return fatalConsequence(k, res.Error)
	}
	var parts []string
	for _, a := range res.Applied {
		if a.Element != el {
			continue
		}
		s := fmt.Sprintf("it sets `%s`", run.Context.Entry(a.Ref).Long)
		if a.ValueAt > 0 && a.ValueAt < len(run.Argv) {
			s += fmt.Sprintf(", which takes the next element, %s, as its value", quote([]byte(run.Argv[a.ValueAt])))
		}
		parts = append(parts, s)
	}
	switch res.Uses[el] {
	case goption.UseUnknown:
		if run.Context.IgnoreUnknown {
			parts = append(parts, "it is an unknown option, ignored")
		}
	case goption.UseNotReached:
		return "Parsing stops before this value because of an earlier error; fix that one first."
	case goption.UseOption, goption.UseValue, goption.UseLeftover, goption.UseSeparator, goption.UseAfterSeparator:
	}
	if len(parts) == 0 {
		return "The value is not used as a value."
	}
	return "The value is not used as a value: " + strings.Join(parts, "; ") + "." + productNote(k.g)
}
