package report

import (
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// githubFormat writes one GitHub Actions workflow command per diagnostic,
// which GitHub turns into annotations on the pull request diff:
//
//	::error file=a.cnf,line=3,col=1,endLine=3,endColumn=4,title=MDL102 whitespace-only-line::message
type githubFormat struct{}

func init() { Register(githubFormat{}) }

func (githubFormat) Name() string { return "github" }

func (githubFormat) Write(w io.Writer, results []FileResult, _ Summary, _ Options) error {
	var b strings.Builder
	for _, r := range results {
		v := newFileView(r)
		file := filepath.ToSlash(r.Path)
		for _, d := range r.Diagnostics {
			s, e := v.span(d.Span)
			if e.Line > s.Line && e.Col == 1 {
				// The span ends with a '\n': annotate up to that line only.
				e = v.src.Position(e.Offset - 1)
			}
			var props []string
			add := func(key, val string) {
				if val != "" { // like the toolkit, skip empty properties
					props = append(props, key+"="+githubProperty(val))
				}
			}
			add("file", file)
			add("line", strconv.Itoa(s.Line))
			if s.Line == e.Line { // columns are only allowed on single-line annotations
				add("col", strconv.Itoa(s.Col))
				add("endLine", strconv.Itoa(e.Line))
				add("endColumn", strconv.Itoa(e.Col))
			} else {
				add("endLine", strconv.Itoa(e.Line))
			}
			add("title", strings.TrimSpace(d.RuleID+" "+d.RuleName))
			b.WriteString("::" + githubCommand(d.Severity) + " " + strings.Join(props, ",") + "::" + githubData(fullMessage(d)) + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func githubCommand(s diag.Severity) string {
	switch s {
	case diag.Error:
		return "error"
	case diag.Warning:
		return "warning"
	}
	return "notice"
}

var (
	githubDataEscaper     = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	githubPropertyEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

// githubData escapes a command message exactly like escapeData in GitHub's
// actions/toolkit, then shows the control characters the toolkit leaves
// untouched (ESC, NUL, …) as visible characters, so that they cannot reach
// the log raw.
func githubData(s string) string { return sanitize(githubDataEscaper.Replace(s)) }

// githubProperty escapes a property value exactly like escapeProperty in
// GitHub's actions/toolkit, and sanitizes it like githubData.
func githubProperty(s string) string { return sanitize(githubPropertyEscaper.Replace(s)) }
