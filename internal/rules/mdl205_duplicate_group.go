package rules

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func init() {
	register(&Rule{
		Meta: Meta{
			ID: "MDL205", Name: "duplicate-group", Family: FamilyGroups,
			Severity: diag.Warning, Fix: diag.Unsafe,
			Summary: "The same group is declared twice.",
			Why: "GLib merges the two sections silently: their keys form one group, a key present " +
				"in both keeps the last value (MDL301). The file then reads as if the second section " +
				"were separate, which it is not. The fix moves the later section's lines to the end " +
				"of the first one.",
			Refs: []string{"K12"},
		},
		Check: func(p *Pass) {
			for _, mg := range p.Model.Groups {
				if len(mg.Lines) < 2 {
					continue
				}
				fi := headerAt(p, mg.Lines[0])
				for _, line := range mg.Lines[1:] {
					i := headerAt(p, line)
					if fi < 0 || i < 0 || !p.KF.Groups[fi].Valid || !p.KF.Groups[i].Valid {
						continue
					}
					g := p.KF.Groups[i]
					d := diag.Diagnostic{
						Span:        g.NameSpan,
						Message:     fmt.Sprintf("[%s] is already declared on line %d: GLib merges the two sections", g.Name, p.KF.Groups[fi].Line),
						Consequence: "The keys of both sections form one group; a key set in both keeps the last value.",
						Related:     []diag.Related{{Span: p.KF.Groups[fi].NameSpan, Message: "first declaration"}},
					}
					if edits, ok := moveSection(p, i, fi); ok {
						d.Fix = &diag.Fix{Applicability: diag.Unsafe, Description: "Move these lines to the end of the first section", Edits: edits}
					}
					p.Report(d)
				}
			}
		},
	})
}

// sectionEnd returns the last line of the section starting at header line
// h: the line before the next header, trailing blank lines excluded.
func sectionEnd(p *Pass, h int) int {
	last := h
	for n := h + 1; n <= len(p.File.Lines); n++ {
		if isHeaderLine(p, n) {
			break
		}
		if !isBlankish(p.File.Content(n)) {
			last = n
		}
	}
	return last
}

func isHeaderLine(p *Pass, n int) bool {
	for _, g := range p.KF.Groups {
		if g.Line == n {
			return true
		}
	}
	return false
}

// moveSection moves the entries of the section headed by group index i
// (without its header) to the end of the section of group index fi, and
// deletes the later header. It gives up on files without a final newline
// and when the later section is not after the first one.
func moveSection(p *Pass, i, fi int) ([]diag.Edit, bool) {
	h, fh := p.KF.Groups[i].Line, p.KF.Groups[fi].Line
	b := p.File.Bytes
	if len(b) == 0 || b[len(b)-1] != '\n' || h <= fh {
		return nil, false
	}
	end, firstEnd := sectionEnd(p, h), sectionEnd(p, fh)
	adjacent := true
	for n := firstEnd + 1; n < h; n++ {
		adjacent = adjacent && isBlankish(p.File.Content(n))
	}
	if adjacent { // only blank lines between: dropping the header is enough
		l := p.File.Line(h)
		return []diag.Edit{{Start: l.Start, End: l.End + 1}}, true
	}
	body := b[p.File.Line(h).End+1 : p.File.Line(end).End+1] // entry lines, newline included
	delStart, delEnd := p.File.Line(h).Start, p.File.Line(end).End+1
	at := p.File.Line(firstEnd).End + 1
	return []diag.Edit{
		{Start: at, End: at, New: string(body)},
		{Start: delStart, End: delEnd},
	}, true
}
