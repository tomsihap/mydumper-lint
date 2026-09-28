package rules

import (
	"bytes"
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL108", Name: "bracket-state-leak", Family: FamilyLoading,
			Severity: diag.Error, Fix: diag.Safe, Unsuppressible: true,
			Summary: "mydumper's pre-processor mis-handles a line because an earlier line contains `[`.",
			Why: "mydumper's config pre-processor appends `= 1` to lines that contain no `=`. When it " +
				"meets a `[`, it copies the rest of the line verbatim and does not reset its state at the " +
				"end of that line. The next line inherits a stale state:\n\n" +
				"- after a line with text before `[` (`# see [docs]`, an indented `  [mydumper]`, " +
				"`key[fr]=v`), an empty line becomes `= 1`, a key/value pair with an empty key;\n" +
				"- after a line with `=` before `[` (`regex=^(a[bc])`), a flag such as `routines` gets " +
				"no `= 1` and is not a key/value pair;\n" +
				"- on the line itself, a line without `=` that contains `[` (`; see [docs]`, " +
				"`routines # [x]`) never gets its `= 1` either.\n\nIn every case GLib rejects the file and " +
				"mydumper ignores all of it. The leak even crosses a valid `[group]` header placed in between.",
			Refs: []string{"P1", "cases 12, 13, 16, 37, 48, 58"},
		},
		Check: func(p *Pass) {
			forCause(p, keyfile.CauseBracketNoValue, func(n int, lc keyfile.LineClass) {
				text := content(p, n)
				p.Report(diag.Diagnostic{
					Span: lineSpan(p, n),
					Message: quote(text[firstSignificant(text):]) + " contains `[` and no `=`: mydumper's " +
						"pre-processor copies everything from the `[` verbatim and never appends `= 1`, so " +
						"this line is not a key/value pair (" + glibSays(lc.Message) + ")",
					Consequence: rejectionConsequence(p),
				})
			})
			for _, c := range []keyfile.Cause{keyfile.CauseBracketLeakBlank, keyfile.CauseBracketLeakFlag} {
				forCause(p, c, func(n int, lc keyfile.LineClass) {
					origin := p.Pre.Lines[n-1].LeakOrigin
					d := diag.Diagnostic{
						Span:        lineSpan(p, n),
						Consequence: rejectionConsequence(p),
					}
					if origin > 0 {
						d.Related = []diag.Related{{
							Span: lineSpan(p, origin),
							Message: "mydumper's pre-processor does not reset its state after this line, " +
								"because it contains `[`",
						}}
					}
					l := p.File.Line(n)
					if c == keyfile.CauseBracketLeakBlank {
						d.Message = fmt.Sprintf("mydumper's pre-processor turns this empty line into `= 1` because "+
							"line %d contains `[` after other text (%s)", origin, glibSays(lc.Message))
						// Removing only this line would make the next empty line the
						// victim: remove the whole run of empty lines at once.
						end := n
						for end < len(p.File.Lines) && p.File.Line(end+1).HasNewline && len(p.File.Content(end+1)) == 0 {
							end++
						}
						start, _ := p.File.LineSpan(n, true)
						_, stop := p.File.LineSpan(end, true)
						d.Fix = &diag.Fix{
							Applicability: diag.Safe,
							Description:   "Remove the empty line",
							Edits:         []diag.Edit{{Start: start, End: stop}},
						}
						if end > n {
							d.Fix.Description = "Remove the empty lines"
						}
						// Once the run is gone, the lines after it inherit the leak:
						// lines containing '[' carry it on, an empty line becomes
						// the next victim, any other line resets the state. When
						// deleting would only move the problem, a "#" line absorbs
						// the leak and resets the pre-processor instead.
						if leakReachesEmptyLine(p, end+1) {
							d.Fix = &diag.Fix{
								Applicability: diag.Safe,
								Description:   "Turn the empty line into a `#` line, which stops the leak",
								Edits:         []diag.Edit{{Start: l.Start, End: l.End, New: "#"}},
							}
						}
					} else {
						text := content(p, n)
						d.Message = fmt.Sprintf("%s does not get its implicit `= 1` because line %d contains `=` "+
							"before `[` (%s)", quote(text[firstSignificant(text):]), origin, glibSays(lc.Message))
						at := l.Start + len(text)
						d.Fix = &diag.Fix{
							Applicability: diag.Safe,
							Description:   "Write the value explicitly (`=1`)",
							Edits:         []diag.Edit{{Start: at, End: at, New: "=1"}},
						}
					}
					p.Report(d)
				})
			}
		},
	})
}

// leakReachesEmptyLine reports whether a leaked pre-processor state entering
// line n would reach an empty line: it crosses lines that contain '[' and
// stops at the first other line.
func leakReachesEmptyLine(p *Pass, n int) bool {
	for ; n <= len(p.File.Lines); n++ {
		c := p.File.Content(n)
		switch {
		case len(c) == 0 && p.File.Line(n).HasNewline:
			return true
		case bytes.IndexByte(c, '[') < 0:
			return false
		}
	}
	return false
}
