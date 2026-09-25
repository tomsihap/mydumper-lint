package report

import (
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// find returns the span of the n-th (0-based) occurrence of sub in src.
func find(src, sub string, n int) diag.Span {
	off := 0
	for ; n >= 0; n-- {
		i := strings.Index(src[off:], sub)
		if i < 0 {
			panic("fixture: " + sub + " not found")
		}
		off += i
		if n > 0 {
			off += len(sub)
		}
	}
	return diag.Span{Start: off, End: off + len(sub)}
}

// point is the empty span at off.
func point(off int) diag.Span { return diag.Span{Start: off, End: off} }

// line returns the span of line n (1-based) of src; withNewline includes its '\n'.
func lineSpan(src string, n int, withNewline bool) diag.Span {
	start := 0
	for ; n > 1; n-- {
		start += strings.IndexByte(src[start:], '\n') + 1
	}
	end := start + strings.IndexByte(src[start:], '\n')
	if withNewline {
		end++
	}
	return diag.Span{Start: start, End: end}
}

func edit(sp diag.Span, s string) diag.Edit { return diag.Edit{Start: sp.Start, End: sp.End, New: s} }

func fix(a diag.Applicability, desc string, edits ...diag.Edit) *diag.Fix {
	return &diag.Fix{Applicability: a, Description: desc, Edits: edits}
}

const fatal = "rejects the whole file: every option in it is ignored"

// appResult has every severity, safe and unsafe fixes, related locations,
// an empty line, a tab and a multi-line span. LF line endings.
func appResult() FileResult {
	const src = "[mydumper]\nhost=db\n  \t\nthreads = 4\n# see [docs]\n\noutputdir=\"/backup\" \n[mydumper]\n\troutines\n"
	ds := []diag.Diagnostic{
		{RuleID: "MDL102", RuleName: "whitespace-only-line", Severity: diag.Error, Span: find(src, "  \t", 0),
			Message: "line contains only spaces and tabs", Consequence: fatal,
			Fix: fix(diag.Safe, "empty the line", edit(find(src, "  \t", 0), ""))},
		{RuleID: "MDL307", RuleName: "spaces-around-equals", Severity: diag.Info, Span: find(src, " = ", 0),
			Message: `spaces around "="`, Fix: fix(diag.Safe, "write threads=4", edit(find(src, " = ", 0), "="))},
		{RuleID: "MDL108", RuleName: "bracket-state-leak", Severity: diag.Error, Span: point(lineSpan(src, 6, false).Start),
			Message: `empty line becomes "= 1" because of the "[" on line 5`, Consequence: fatal,
			Related: []diag.Related{{Span: find(src, "[", 1), Message: `the pre-processor state freezes at this "["`}},
			Fix:     fix(diag.Safe, "delete the empty line", edit(lineSpan(src, 6, true), ""))},
		{RuleID: "MDL313", RuleName: "quoted-value", Severity: diag.Warning, Span: find(src, `"/backup"`, 0),
			Message: "value is wrapped in quotes", Consequence: `writes the dump into a directory named "/backup", quotes included`,
			Fix: fix(diag.Unsafe, "remove the quotes", edit(find(src, `"`, 0), ""), edit(find(src, `"`, 1), ""))},
		{RuleID: "MDL302", RuleName: "trailing-whitespace-in-value", Severity: diag.Error, Span: head(find(src, " \n[", 0)),
			Message: "value ends with whitespace", Consequence: "keeps the space: outputdir names another directory",
			Fix: fix(diag.Unsafe, "trim the value", edit(head(find(src, " \n[", 0)), ""))},
		{RuleID: "MDL205", RuleName: "duplicate-group", Severity: diag.Warning,
			Span:    diag.Span{Start: lineSpan(src, 8, false).Start, End: lineSpan(src, 9, false).End},
			Message: "group [mydumper] is declared twice", Consequence: "merges both groups silently",
			Related: []diag.Related{{Span: lineSpan(src, 1, false), Message: "first declared here"}},
			Fix:     fix(diag.Unsafe, "", edit(lineSpan(src, 8, true), ""))},
		{RuleID: "MDL306", RuleName: "leading-whitespace", Severity: diag.Info, Span: find(src, "\t", 1),
			Message: "line is indented", Fix: fix(diag.Safe, "remove the indentation", edit(find(src, "\t", 1), ""))},
		{RuleID: "MDL308", RuleName: "flag-without-value", Severity: diag.Info, Span: find(src, "routines", 0),
			Message: "option without a value", Fix: fix(diag.Safe, "write routines=1", edit(point(find(src, "routines", 0).End), "=1"))},
	}
	diag.Sort(ds)
	return FileResult{Path: "conf/app.cnf", Source: []byte(src), Version: "v0.19.3-3", Diagnostics: ds}
}

// head is the span of the first byte of sp.
func head(sp diag.Span) diag.Span { return diag.Span{Start: sp.Start, End: sp.Start + 1} }

// crlfResult has CRLF line endings, a fix with several edits and a span over
// several lines.
func crlfResult() FileResult {
	const src = "[myloader]\r\nthreads=4\r\n\r\n\r\n\r\noverwrite-tables\r\n"
	var crs []diag.Edit
	for i := range 6 {
		crs = append(crs, edit(find(src, "\r", i), ""))
	}
	ds := []diag.Diagnostic{
		{RuleID: "MDL103", RuleName: "carriage-return", Severity: diag.Error, Span: find(src, "\r", 0),
			Message: "file uses CRLF line endings", Fix: fix(diag.Safe, `convert "\r\n" to "\n"`, crs...)},
		{RuleID: "MDL103", RuleName: "carriage-return", Severity: diag.Error, Span: find(src, "\r", 2),
			Message:     "empty line made of a carriage return",
			Consequence: `the line becomes "\r= 1", which rejects the whole file`},
		{RuleID: "MDL310", RuleName: "blank-lines", Severity: diag.Info,
			Span:    diag.Span{Start: lineSpan(src, 3, false).Start, End: lineSpan(src, 5, true).End},
			Message: "3 consecutive blank lines",
			Fix:     fix(diag.Safe, "keep one blank line", edit(diag.Span{Start: lineSpan(src, 4, false).Start, End: lineSpan(src, 5, true).End}, ""))},
	}
	diag.Sort(ds)
	return FileResult{Path: "conf/crlf.cnf", Source: []byte(src), Version: "v0.19.3-3", Diagnostics: ds}
}
