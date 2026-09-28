package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL313", Name: "quoted-value", Family: FamilyValues,
			Severity: diag.Warning, Fix: diag.Unsafe,
			Summary: "An option's value is wrapped in quotes, which mydumper keeps.",
			Why: "The MySQL client library strips quotes in option files; GLib does not. " +
				"`outputdir=\"/backup\"` names a directory `\"/backup\"`, quotes included: relative to the " +
				"working directory, since it no longer starts with `/`. Remove the quotes. SQL-valued " +
				"options (`where`) are not checked: their quotes are SQL.",
			Refs: []string{"F4", "C4"},
			E2E:  []string{"mdl313-quoted-outputdir-keeps-quotes", "mdl313-quoted-absolute-outputdir-aborts"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				v := k.ke.Value
				if k.entry == nil || !k.effective() || k.entry.NoArg() || k.entry.Long == "where" || len(v) < 2 {
					continue
				}
				if q := v[0]; (q != '"' && q != '\'') || v[len(v)-1] != q {
					continue
				}
				inner := v[1 : len(v)-1]
				start, end := k.valueSpan()
				p.Report(diag.Diagnostic{
					Span:        diag.Span{Start: start, End: end},
					Message:     fmt.Sprintf("the quotes are part of the value of `%s`: GLib does not strip them", k.entry.Long),
					Consequence: fmt.Sprintf("%s gets %s, quotes included, not %s.", k.entry.Long, v, inner) + productNote(k.g),
					Fix: &diag.Fix{
						Applicability: diag.Unsafe,
						Description:   "Remove the quotes",
						Edits:         []diag.Edit{{Start: start, End: end, New: inner}},
					},
				})
			}
		},
	})
}
