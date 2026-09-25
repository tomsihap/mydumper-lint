package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL112", Name: "bracket-leak-risk", Family: FamilyLoading,
			Severity: diag.Warning, Unsuppressible: true,
			Summary: "An empty line placed after this line would make mydumper ignore the file.",
			Why: "After a line that contains `[` after other text (a comment such as `# see [docs]`, an " +
				"indented `  [mydumper]`, a localized key `key[fr]=v`), mydumper's pre-processor does not " +
				"reset its state (see MDL108). The file loads today because no empty line follows, but " +
				"adding one, by hand or by a script that appends a block starting with an empty line, " +
				"makes GLib reject the file.\n\nIt is a warning when the line is the last one of the file " +
				"(a script appending to the file is the typical way this breaks), information otherwise. " +
				"Remove the `[` from the comment, or de-indent the header.",
			Refs: []string{"P1", "case 58"},
		},
		Check: func(p *Pass) {
			total := len(p.Pre.Lines)
			for i, info := range p.Pre.Lines {
				// Only lines that start a leak: text before '[' and no '=' before it.
				if info.BracketAt <= 0 || info.StartDirty || info.OutNewLine || info.OutEqualFound {
					continue
				}
				last := i + 1 // 1-based number of the last line of the leak chain
				for last < total && p.Pre.Lines[last].StartDirty && p.Pre.Lines[last].BracketAt >= 0 {
					last++
				}
				if next := last; next < total && (p.KF.Lines[next].Cause == keyfile.CauseBracketLeakBlank ||
					p.KF.Lines[next].Cause == keyfile.CauseBracketLeakFlag) {
					continue // already broken: MDL108
				}
				n := i + 1
				d := diag.Diagnostic{
					Severity: diag.Info,
					Span:     lineSpan(p, n),
					Message: fmt.Sprintf("line contains `[` after other text: an empty line placed right after "+
						"line %d would make mydumper's pre-processor produce `= 1`, which GLib rejects", last),
					Consequence: fmt.Sprintf("Adding an empty line after line %d would make mydumper ignore "+
						"the entire file.", last),
				}
				if last == total {
					d.Severity = diag.Warning
					d.Consequence = "If a block starting with an empty line is appended to this file (for " +
						"example \"\\n[client]\" added by a script), mydumper will ignore the entire file."
				}
				p.Report(d)
			}
		},
	})
}
