package rules

import (
	"unicode/utf8"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL311", Name: "invalid-utf8", Family: FamilyValues,
			Severity: diag.Warning,
			Summary:  "A line contains bytes that are not valid UTF-8.",
			Why: "GLib reads the bytes as they are, but prints them as U+FFFD in messages, and GLib's " +
				"option parser rejects such a value in a string option in every UTF-8 locale (MDL409). " +
				"The file was probably saved in a legacy encoding such as Latin-1: re-save it as UTF-8.",
			Refs: []string{"K16"},
		},
		Check: func(p *Pass) {
			for n := 1; n <= len(p.File.Lines); n++ {
				c := p.File.Content(n)
				if utf8.Valid(c) {
					continue
				}
				i := 0
				for i < len(c) {
					r, size := utf8.DecodeRune(c[i:])
					if r == utf8.RuneError && size <= 1 {
						break
					}
					i += size
				}
				start := p.File.Line(n).Start + i
				p.Report(diag.Diagnostic{
					Span:    diag.Span{Start: start, End: start + 1},
					Message: "invalid UTF-8: the file is probably not saved as UTF-8",
				})
			}
		},
	})
}
