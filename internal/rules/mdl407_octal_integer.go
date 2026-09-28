package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL407", Name: "octal-integer", Family: FamilyOptions,
			Severity: diag.Warning,
			Summary:  "An integer written with a leading 0 is read in octal.",
			Why: "GLib parses integer options with C's `strtol` in base 0: a leading `0` means octal. " +
				"`threads=010` sets 8 threads, not 10. Write the number without the leading zero, or " +
				"keep it if octal is really meant (no fix: both readings are plausible).",
			Refs: []string{"G7"},
			E2E:  []string{"mdl407-octal-threads"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				if k.entry == nil || !k.effective() || !numeric(k.entry.Arg) || k.ctx.Check(k.ref, k.name(), k.ke.Value) != "" {
					continue
				}
				t := strings.TrimLeft(strings.TrimSpace(k.ke.Value), "+-")
				if len(t) < 2 || t[0] != '0' || strings.Trim(t, "01234567") != "" {
					continue
				}
				oct, err1 := strconv.ParseInt(t, 8, 64)
				dec, err2 := strconv.ParseInt(t, 10, 64)
				if err1 != nil || err2 != nil || oct == dec {
					continue
				}
				start, end := k.valueSpan()
				p.Report(diag.Diagnostic{
					Span:        diag.Span{Start: start, End: end},
					Message:     fmt.Sprintf("`%s` is octal: `%s` gets %d, not %d", t, k.entry.Long, oct, dec),
					Consequence: fmt.Sprintf("%s uses %d.", k.entry.Long, oct),
				})
			}
		},
	})
}
