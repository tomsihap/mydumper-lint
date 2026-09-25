package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// textFormat is the default, human-oriented format: a rustc-style block per
// diagnostic and a summary footer.
type textFormat struct{}

func init() { Register(textFormat{}) }

func (textFormat) Name() string { return "text" }

func (textFormat) Write(w io.Writer, results []FileResult, sum Summary, opt Options) error {
	p := painter(opt.Color)
	var b strings.Builder
	for _, r := range results {
		v := newFileView(r)
		for _, d := range r.Diagnostics {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			writeBlock(&b, v, d, p)
		}
	}
	if !opt.Quiet {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		writeFooter(&b, results, sum)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// ANSI styles of the text formats.
const (
	styleReset = "\x1b[0m"
	styleBold  = "\x1b[1m"
	styleCyan  = "\x1b[36m"
)

// painter applies ANSI styles when colors are enabled, and nothing otherwise.
type painter bool

func (p painter) paint(style, s string) string {
	if !p || s == "" {
		return s
	}
	return style + s + styleReset
}

// severityStyle is bold red for errors, bold yellow for warnings and bold
// blue for infos.
func severityStyle(s diag.Severity) string {
	switch s {
	case diag.Error:
		return "\x1b[1;31m"
	case diag.Warning:
		return "\x1b[1;33m"
	case diag.Info, diag.Off:
	}
	return "\x1b[1;34m"
}

// location formats "path:line:col" in cyan.
func location(v fileView, pos source.Position, p painter) string {
	return p.paint(styleCyan, sanitize(v.path)+":"+strconv.Itoa(pos.Line)+":"+strconv.Itoa(pos.Col))
}

// header is the first line of a diagnostic, and the whole of it in the
// concise format: "path:line:col: severity MDL102[name] message".
func header(v fileView, d diag.Diagnostic, p painter) string {
	start, _ := v.span(d.Span)
	h := location(v, start, p) + ": " + p.paint(severityStyle(d.Severity), d.Severity.String()) +
		" " + p.paint(styleBold, sanitize(ruleLabel(d)))
	if d.Message != "" {
		h += " " + sanitize(d.Message)
	}
	return h
}

// writeBlock writes the full text rendering of d: header, consequence,
// source excerpt with the span underlined, related locations and fix.
func writeBlock(b *strings.Builder, v fileView, d diag.Diagnostic, p painter) {
	start, end := v.span(d.Span)
	gutter := strings.Repeat(" ", len(strconv.Itoa(start.Line)))
	b.WriteString(header(v, d, p) + "\n")
	if d.Consequence != "" {
		fmt.Fprintf(b, "%s = %s %s\n", gutter, p.paint(styleBold, "impact:"), sanitize(d.Consequence))
	}

	ls := v.lineStart(start.Line)
	ex := makeExcerpt(v.line(start.Line), start.Offset-ls, end.Offset-ls)
	fmt.Fprintf(b, "%s |\n", gutter)
	if ex.text == "" {
		fmt.Fprintf(b, "%d |\n", start.Line)
	} else {
		fmt.Fprintf(b, "%d | %s\n", start.Line, ex.text)
	}
	carets := p.paint(severityStyle(d.Severity), strings.Repeat("^", ex.width))
	fmt.Fprintf(b, "%s | %s%s%s\n", gutter, strings.Repeat(" ", ex.col), carets, moreLines(start, end))

	var notes []string
	for _, rel := range d.Related {
		rs, _ := v.span(rel.Span)
		note := p.paint(styleBold, "note:") + " " + location(v, rs, p)
		if rel.Message != "" {
			note += ": " + sanitize(rel.Message)
		}
		notes = append(notes, note)
	}
	if d.Fix != nil {
		help := p.paint(styleBold, "help:") + " fixable with " + fixCommand(d.Fix)
		if d.Fix.Description != "" {
			help += ": " + sanitize(d.Fix.Description)
		}
		notes = append(notes, help)
	}
	if len(notes) > 0 {
		fmt.Fprintf(b, "%s |\n", gutter)
	}
	for _, n := range notes {
		fmt.Fprintf(b, "%s = %s\n", gutter, n)
	}
}

// moreLines tells how many lines a span covers beyond its first one. A span
// that ends at the start of a line (after a '\n') does not cover that line.
func moreLines(start, end source.Position) string {
	n := end.Line - start.Line
	if n > 0 && end.Col == 1 {
		n--
	}
	if n <= 0 {
		return ""
	}
	return " (and " + plural(n, "more line") + ")"
}

// writeFooter writes the summary: what was fixed, then either the counts and
// what --fix can do, or a clean message.
func writeFooter(b *strings.Builder, results []FileResult, sum Summary) {
	if sum.Fixed > 0 {
		fmt.Fprintf(b, "Fixed %s.\n", plural(sum.Fixed, "problem"))
	}
	if sum.Errors+sum.Warnings+sum.Infos == 0 {
		if sum.Files == 0 {
			b.WriteString("No files were checked.\n")
		} else {
			fmt.Fprintf(b, "All checks passed: no problems found in %s.\n", plural(sum.Files, "file"))
		}
		return
	}
	files := 0
	for _, r := range results {
		if len(r.Diagnostics) > 0 {
			files++
		}
	}
	fmt.Fprintf(b, "Found %s, %s, %s in %s%s.\n",
		plural(sum.Errors, "error"), plural(sum.Warnings, "warning"), plural(sum.Infos, "info"),
		plural(files, "file"), fixHint(sum))
}

func fixHint(sum Summary) string {
	switch {
	case sum.Fixable > 0 && sum.UnsafeFixable > 0:
		return fmt.Sprintf(" (%d fixable with --fix, %d more with --unsafe-fixes)", sum.Fixable, sum.UnsafeFixable)
	case sum.Fixable > 0:
		return fmt.Sprintf(" (%d fixable with --fix)", sum.Fixable)
	case sum.UnsafeFixable > 0:
		return fmt.Sprintf(" (%d fixable with --fix --unsafe-fixes)", sum.UnsafeFixable)
	}
	return ""
}
