package rules

import (
	"fmt"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/goption"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL403", Name: "invalid-number", Family: FamilyOptions,
			Severity: diag.Error,
			Summary:  "A numeric option's value is not a number GLib accepts.",
			Why: "GLib parses integer options with C's `strtol` in base 0 and rejects anything left " +
				"over: `08` (a leading 0 means octal, where 8 and 9 are not digits), `1e3`, `10k`, `4.5`. " +
				"A value too large for the option is out of range. Either way mydumper aborts at startup " +
				"with `option parsing failed`.\n\nTrailing spaces are reported by MDL302, empty values " +
				"by MDL404.",
			Refs: []string{"G5", "G7"},
			E2E:  []string{"mdl403-invalid-integer"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				if k.entry == nil || !k.effective() || !numeric(k.entry.Arg) {
					continue
				}
				v := k.ke.Value
				msg := k.ctx.Check(k.ref, k.name(), v)
				if v == "" || msg == "" {
					continue
				}
				if t := strings.TrimRight(v, " \t"); t != v && k.ctx.Check(k.ref, k.name(), t) == "" {
					continue // only trailing whitespace: MDL302
				}
				start, end := k.valueSpan()
				p.Report(diag.Diagnostic{
					Span:        diag.Span{Start: start, End: end},
					Message:     fmt.Sprintf("%s is not a valid value for `%s`: %s (%s)", quote([]byte(v)), k.entry.Long, numberHint(v, msg), glibSays(msg)),
					Consequence: fatalConsequence(&k, msg),
				})
			}
		},
	})
}

func numeric(a goption.Arg) bool {
	return a == goption.ArgInt || a == goption.ArgInt64 || a == goption.ArgDouble
}

// numberHint says why strtol rejects a value.
func numberHint(v, msg string) string {
	t := strings.TrimSpace(v)
	switch {
	case strings.HasSuffix(msg, "out of range"):
		return "too large for the option"
	case len(t) > 1 && t[0] == '0' && t[1] != 'x' && t[1] != 'X' && strings.Trim(t, "0123456789") == "":
		return "a leading 0 makes the number octal, where 8 and 9 are not digits"
	case strings.ContainsAny(t, "eE") && !strings.HasPrefix(lowerASCII(t), "0x"):
		return "exponents are not accepted"
	case strings.Contains(t, "."):
		return "not an integer"
	case t != "" && strings.ContainsAny(t[len(t)-1:], "kKmMgGtTbB"):
		return "unit suffixes are not accepted: write the plain number"
	}
	return "not a number"
}
