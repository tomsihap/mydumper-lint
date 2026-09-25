package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL312", Name: "control-character", Family: FamilyValues,
			Severity: diag.Warning,
			Summary:  "A line contains an invisible control character.",
			Why: "Control characters are invisible in editors and reviews, and GLib gives some of them " +
				"surprising meanings:\n\n" +
				"- a NUL byte at the start of a line turns the whole line into a comment: the key " +
				"silently disappears;\n- a NUL byte in a value cuts the value there;\n" +
				"- a vertical tab (`\\v`) is not whitespace for GLib: `\\vroutines=1` defines the key " +
				"`\\vroutines`, not `routines`.\n\nNUL bytes that make a line unparsable are reported by " +
				"MDL111; carriage returns by MDL103.",
			Refs: []string{"K2", "K17", "cases 31, 57, 60, 66"},
		},
		Check: func(p *Pass) {
			for i, l := range p.File.Lines {
				n := i + 1
				lc := p.KF.Lines[i]
				if lc.Kind == keyfile.KindRejected && lc.Cause == keyfile.CauseNulByte {
					continue
				}
				c := p.File.Content(n)
				j := -1
				for k, ch := range c {
					if (ch < 0x20 && ch != '\t' && ch != '\r') || ch == 0x7f {
						j = k
						break
					}
				}
				if j < 0 {
					continue
				}
				ch := c[j]
				d := diag.Diagnostic{
					Span:    diag.Span{Start: l.Start + j, End: l.Start + j + 1},
					Message: "control character " + controlName(ch) + " in this line",
				}
				s := firstSignificant(c)
				switch {
				case ch == 0 && j == s:
					d.Severity = diag.Error
					d.Message = "NUL byte at the start of the line: GLib treats the whole line as a comment"
					d.Consequence = "mydumper silently ignores " + quote(c[j+1:]) + "."
				case ch == 0 && lc.Kind == keyfile.KindEntry:
					e := p.KF.Entries[lc.Entry]
					d.Severity = diag.Error
					d.Message = "NUL byte in the value of " + quote([]byte(e.Key)) + ": GLib cuts the value there"
					d.Consequence = "mydumper reads the value " + quote([]byte(e.Value)) + "."
				case ch == '\v' && j == s && lc.Kind == keyfile.KindEntry:
					e := p.KF.Entries[lc.Entry]
					d.Message = "vertical tab before the key: it is not whitespace for GLib"
					d.Consequence = "The key is " + quote([]byte(e.Key)) + ", which matches no option."
				}
				p.Report(d)
			}
		},
	})
}
