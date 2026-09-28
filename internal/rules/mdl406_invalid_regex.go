package rules

import (
	"fmt"

	"github.com/dlclark/regexp2"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL406", Name: "invalid-regex", Family: FamilyOptions,
			Severity: diag.Warning,
			Summary:  "A regular-expression option's value does not compile.",
			Why: "mydumper compiles options such as `regex` with PCRE2 and aborts at startup when the " +
				"expression is invalid. mydumper-lint checks it with a PCRE-like engine (regexp2), so a " +
				"few PCRE-only constructs may be reported by mistake: hence a warning. The value is used " +
				"as written, quotes included (F4).",
			Refs: []string{"F4"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				if k.entry == nil || !k.effective() || !k.span.IsRegex || k.ke.Value == "" {
					continue
				}
				if _, err := regexp2.Compile(k.ke.Value, regexp2.None); err != nil {
					start, end := k.valueSpan()
					p.Report(diag.Diagnostic{
						Span:        diag.Span{Start: start, End: end},
						Message:     fmt.Sprintf("`%s` is not a valid regular expression: %v", k.entry.Long, err),
						Consequence: fmt.Sprintf("%s aborts when it compiles the expression (\"Regular expression fail\").", k.g.Tool) + productNote(k.g),
					})
				}
			}
		},
	})
}
