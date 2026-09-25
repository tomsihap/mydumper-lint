package report

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func TestNamesListsEveryFormat(t *testing.T) {
	want := []string{"concise", "github", "gitlab", "json", "junit", "sarif", "text"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	for _, name := range want {
		f, err := Get(name)
		if err != nil || f.Name() != name {
			t.Errorf("Get(%q) = %v, %v", name, f, err)
		}
	}
}

func TestGetUnknownFormat(t *testing.T) {
	_, err := Get("xml")
	if err == nil || !strings.Contains(err.Error(), `unknown format "xml"`) || !strings.Contains(err.Error(), "[concise github gitlab json junit sarif text]") {
		t.Errorf("Get(xml) error = %v", err)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a format twice must panic")
		}
	}()
	Register(textFormat{})
}

func TestSummarize(t *testing.T) {
	results := []FileResult{
		{Fixed: 2, Diagnostics: []diag.Diagnostic{
			{Severity: diag.Error, Fix: &diag.Fix{Applicability: diag.Safe}},
			{Severity: diag.Error, Fix: &diag.Fix{Applicability: diag.Unsafe}},
			{Severity: diag.Warning},
			{Severity: diag.Info, Fix: &diag.Fix{Applicability: diag.Safe}},
			{Severity: diag.Off},
		}},
		{},
		{Fixed: 1, Diagnostics: []diag.Diagnostic{{Severity: diag.Warning, Fix: &diag.Fix{Applicability: diag.Unsafe}}}},
	}
	want := Summary{Files: 3, Errors: 2, Warnings: 2, Infos: 1, Fixable: 2, UnsafeFixable: 2, Fixed: 3}
	if got := Summarize(results); got != want {
		t.Errorf("Summarize = %+v, want %+v", got, want)
	}
	if got := Summarize(nil); got != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v", got)
	}
	got := Summarize(scenarios()[0].results)
	want = Summary{Files: 5, Errors: 8, Warnings: 6, Infos: 4, Fixable: 9, UnsafeFixable: 3, Fixed: 2}
	if got != want {
		t.Errorf("Summarize(fixtures) = %+v, want %+v", got, want)
	}
}

type failingWriter struct{}

var errWrite = errors.New("disk full")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestWriteErrorsPropagate(t *testing.T) {
	results := scenarios()[0].results
	for _, name := range Names() {
		f, _ := Get(name)
		if err := f.Write(failingWriter{}, results, Summarize(results), testOptions()); !errors.Is(err, errWrite) {
			t.Errorf("%s: Write error = %v, want %v", name, err, errWrite)
		}
	}
}

// TestBogusSpansAreClamped checks that spans outside the file, inverted
// spans and a missing source never make a format panic.
func TestBogusSpansAreClamped(t *testing.T) {
	bogus := []diag.Diagnostic{
		{
			RuleID: "MDL900", Severity: diag.Error, Span: diag.Span{Start: -5, End: 2},
			Related: []diag.Related{{Span: diag.Span{Start: 100, End: 200}}},
			Fix:     &diag.Fix{Applicability: diag.Safe, Edits: []diag.Edit{{Start: 50, End: -1}}},
		},
		{RuleID: "MDL901", Severity: diag.Off, Span: diag.Span{Start: 9, End: 3}},
		{RuleID: "MDL902", Severity: diag.Warning, Span: diag.Span{Start: 1000, End: 1000}},
	}
	results := []FileResult{
		{Path: "a.cnf", Source: []byte("k=v\n"), Diagnostics: bogus},
		{Path: "b.cnf", Diagnostics: bogus},
	}
	for _, name := range Names() {
		out := render(t, name, results, Options{})
		if out == "" {
			t.Errorf("%s: no output", name)
		}
	}
	v := newFileView(results[0])
	if s, e := v.span(diag.Span{Start: 9, End: 3}); s.Offset != 4 || e.Offset != 4 || s.Line != 2 {
		t.Errorf("inverted span = %+v %+v", s, e)
	}
	if s, _ := v.span(diag.Span{Start: -5}); s.Offset != 0 || s.Line != 1 || s.Col != 1 {
		t.Errorf("negative offset = %+v", s)
	}
}

func TestHeaderWithoutNameOrMessage(t *testing.T) {
	r := FileResult{Path: "x.cnf", Source: []byte("[g]\n"), Diagnostics: []diag.Diagnostic{{RuleID: "MDL999", Severity: diag.Info}}}
	if got, want := render(t, "concise", []FileResult{r}, Options{}), "x.cnf:1:1: info MDL999\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathsAndMessagesAreSanitized(t *testing.T) {
	r := FileResult{Path: "evil\n\x1b[2Jdir/a.cnf", Source: []byte("k=v\n"), Diagnostics: []diag.Diagnostic{{
		RuleID: "MDL312", RuleName: "control\tcharacter", Severity: diag.Warning, Span: diag.Span{Start: 0, End: 1},
		Message: "line one\nline two\r", Consequence: "bell\a",
		Related: []diag.Related{{Span: diag.Span{Start: 2, End: 3}, Message: "x\x00y"}},
		Fix:     &diag.Fix{Applicability: diag.Unsafe, Description: "del\x7f"},
	}}}
	out := render(t, "text", []FileResult{r}, Options{Quiet: true})
	for _, want := range []string{
		"evil\U0000240a\U0000241b[2Jdir/a.cnf:1:1: warning MDL312[control\U00002409character] line one\U0000240aline two\U0000240d\n",
		"= impact: bell\U00002407\n",
		"= note: evil\U0000240a\U0000241b[2Jdir/a.cnf:1:3: x\U00002400y\n",
		"= help: fixable with --fix --unsafe-fixes: del\U00002421\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(render(t, "concise", []FileResult{r}, Options{}), "\n"); n != 1 {
		t.Errorf("concise printed %d lines for one diagnostic", n)
	}
}

func TestFixHint(t *testing.T) {
	tests := map[Summary]string{
		{}:                             "",
		{Fixable: 2}:                   " (2 fixable with --fix)",
		{UnsafeFixable: 1}:             " (1 fixable with --fix --unsafe-fixes)",
		{Fixable: 1, UnsafeFixable: 3}: " (1 fixable with --fix, 3 more with --unsafe-fixes)",
	}
	for sum, want := range tests {
		if got := fixHint(sum); got != want {
			t.Errorf("fixHint(%+v) = %q, want %q", sum, got, want)
		}
	}
}

func TestFooterSingulars(t *testing.T) {
	r := FileResult{Path: "a.cnf", Source: []byte("k\n"), Diagnostics: []diag.Diagnostic{
		{RuleID: "MDL308", Severity: diag.Info, Fix: &diag.Fix{Applicability: diag.Unsafe}},
	}}
	out := render(t, "text", []FileResult{r}, Options{})
	if want := "Found 0 errors, 0 warnings, 1 info in 1 file (1 fixable with --fix --unsafe-fixes).\n"; !strings.HasSuffix(out, want) {
		t.Errorf("footer:\n%s\nwant suffix %q", out, want)
	}
}
