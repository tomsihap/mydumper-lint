package rules

import (
	"fmt"
	"unicode/utf8"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL409", Name: "non-ascii-option-value", Family: FamilyOptions,
			Severity: diag.Warning,
			Summary:  "A string option's value contains non-ASCII characters.",
			Why: "GLib converts the values of string options (and of most callback options) from the " +
				"character set of mydumper's locale. Without a UTF-8 locale, which is the case when LANG " +
				"is unset (cron jobs, the official Docker image), any byte above 127 fails the " +
				"conversion and mydumper aborts at startup with `Invalid byte sequence in conversion " +
				"input`. It is a warning because it depends on how mydumper is run; a value that is not " +
				"valid UTF-8 fails in every locale (error). Filename options such as `outputdir` are not " +
				"converted.\n\nFor an option whose value is optional, such as `compress`, the failed " +
				"conversion does not abort: the option silently receives no value.",
			Refs: []string{"goption: g_locale_to_utf8"},
			E2E:  []string{"mdl409-non-ascii-value-c-locale", "mdl409-non-ascii-callback-value-c-locale"},
		},
		Check: func(p *Pass) {
			for _, k := range optionKeys(p) {
				v := k.ke.Value
				if k.entry == nil || !k.effective() || !k.entry.Converted() || isASCII(v) {
					continue
				}
				start, end := k.valueSpan()
				d := diag.Diagnostic{Span: diag.Span{Start: start, End: end}}
				valid := utf8.ValidString(v)
				if valid {
					d.Message = fmt.Sprintf("the value of `%s` contains non-ASCII characters: GLib converts it from the locale's character set", k.entry.Long)
				} else {
					d.Severity = diag.Error
					d.Message = fmt.Sprintf("the value of `%s` is not valid UTF-8: GLib rejects it in the C locale and in UTF-8 locales", k.entry.Long)
				}
				switch {
				case k.entry.OptionalArg():
					d.Consequence = fmt.Sprintf("Where the conversion fails, `%s` receives no value at all.", k.entry.Long) + productNote(k.g)
				case valid:
					d.Consequence = fmt.Sprintf("Where the conversion fails (without a UTF-8 locale, e.g. LANG unset in cron or Docker), %s aborts at startup: \"option parsing failed: Invalid byte sequence in conversion input, try --help\".", k.g.Tool) + productNote(k.g)
				default:
					d.Consequence = fatalConsequence(&k, "Invalid byte sequence in conversion input")
				}
				p.Report(d)
			}
		},
	})
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
