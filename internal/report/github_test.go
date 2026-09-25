package report

import (
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// TestGitHubEscaping follows escapeData and escapeProperty of
// actions/toolkit (packages/core/src/command.ts).
func TestGitHubEscaping(t *testing.T) {
	data := map[string]string{
		"plain":                "plain",
		"100%":                 "100%25",
		"a\r\nb":               "a%0D%0Ab",
		"%0A is not a newline": "%250A is not a newline",
		"a:b,c":                "a:b,c",
		"tab\tesc\x1b":         "tab\U00002409esc\U0000241b",
	}
	for in, want := range data {
		if got := githubData(in); got != want {
			t.Errorf("githubData(%q) = %q, want %q", in, got, want)
		}
	}
	props := map[string]string{
		"conf/app.cnf":       "conf/app.cnf",
		"C:\\x,y.cnf":        "C%3A\\x%2Cy.cnf",
		"50%\r\n":            "50%25%0D%0A",
		"MDL102 name":        "MDL102 name",
		"a::b":               "a%3A%3Ab",
		"nul\x00bad\xffbyte": `nul` + "\U00002400" + `bad\xffbyte`,
	}
	for in, want := range props {
		if got := githubProperty(in); got != want {
			t.Errorf("githubProperty(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitHubCommands(t *testing.T) {
	src := "[g]\nk=1\n\n"
	r := FileResult{Path: "dir,x/a:b.cnf", Source: []byte(src), Diagnostics: []diag.Diagnostic{
		{RuleID: "MDL310", RuleName: "blank-lines", Severity: diag.Info, Span: lineSpan(src, 3, true),
			Message: "50% blank\nline"},
		{RuleID: "MDL999", Severity: diag.Warning, Span: diag.Span{Start: 0, End: len(src)}, Message: "m"},
		{Severity: diag.Error, Span: lineSpan(src, 2, true), Message: "no rule"},
	}}
	got := render(t, "github", []FileResult{r}, Options{})
	want := "::notice file=dir%2Cx/a%3Ab.cnf,line=3,col=1,endLine=3,endColumn=1,title=MDL310 blank-lines::50%25 blank%0Aline\n" +
		"::warning file=dir%2Cx/a%3Ab.cnf,line=1,endLine=3,title=MDL999::m\n" +
		"::error file=dir%2Cx/a%3Ab.cnf,line=2,col=1,endLine=2,endColumn=4::no rule\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
