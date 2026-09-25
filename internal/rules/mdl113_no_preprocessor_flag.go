package rules

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL113", Name: "unsupported-flag-without-value", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "A line without `=` in a mydumper version that has no pre-processor.",
			Why: "Starting with v0.19.3-1, mydumper rewrites a valueless option such as `routines` into " +
				"`routines= 1` before handing the file to GLib. Earlier versions (v0.19.1-x) load the file " +
				"with GLib directly: `routines` is then neither a key/value pair, a group nor a comment, " +
				"so GLib rejects the file and mydumper ignores all of it. Write the value explicitly " +
				"(`routines=1`), which works with every version.",
			Refs: []string{"§3.2", "generator: loader of v0.19.1-x"},
			E2E:  []string{"mdl113-bare-flag-v0.19.1"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseNoPreprocessor, func(n int, lc keyfile.LineClass) {
				text := content(p, n)
				name := bytes.Trim(text, " \t\f\r") // GLib's whitespace; \v is not
				d := diag.Diagnostic{
					Span: lineSpan(p, n),
					Message: quote(name) + " has no `=`: mydumper " + versionLabel(p) + " hands the file to GLib " +
						"without rewriting valueless options into `key= 1` (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				}
				if name[0] == ';' {
					d.Message = quote(name) + " is not a comment for GLib, only `#` starts one, and mydumper " +
						versionLabel(p) + " does not rewrite it into a key (" + glibSays(lc.Message) + ")"
				}
				// Only a bare option name is surely a flag: "host localhost" is
				// more likely a missing '=' than a key named "host localhost".
				if isOptionName(name) {
					at := p.File.Line(n).Start + len(text)
					d.Fix = &diag.Fix{
						Applicability: diag.Safe,
						Description:   "Write the value explicitly (`" + string(name) + "=1`)",
						Edits:         []diag.Edit{{Start: at, End: at, New: "=1"}},
					}
				}
				p.Report(d)
			})
		},
	})
}

// isOptionName reports whether b looks like one option or table key name.
func isOptionName(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// versionLabel names the target version in messages.
func versionLabel(p *Pass) string {
	if p.Version != "" {
		return p.Version
	}
	return "(this version)"
}
