package report

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func decodeJUnit(t *testing.T, out string) junitSuites {
	t.Helper()
	var doc junitSuites
	dec := xml.NewDecoder(strings.NewReader(out))
	dec.Strict = true
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("malformed XML: %v\n%s", err, out)
	}
	return doc
}

func TestJUnitIsWellFormed(t *testing.T) {
	for _, sc := range scenarios() {
		out := render(t, "junit", sc.results, testOptions())
		if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`+"\n") {
			t.Errorf("%s: no XML declaration", sc.name)
		}
		doc := decodeJUnit(t, out)
		sum := Summarize(sc.results)
		if len(doc.Suites) != len(sc.results) || doc.Failures != sum.Errors+sum.Warnings+sum.Infos || doc.Errors != 0 {
			t.Errorf("%s: %d suites, %d failures", sc.name, len(doc.Suites), doc.Failures)
		}
		tests := 0
		for i, s := range doc.Suites {
			r := sc.results[i]
			tests += s.Tests
			if s.Name != r.Path || s.Tests != len(s.Cases) || s.Failures != len(r.Diagnostics) {
				t.Errorf("%s: suite %+v", sc.name, s)
			}
			for j, c := range s.Cases {
				if len(r.Diagnostics) == 0 {
					if c.Failure != nil || c.Name != r.Path {
						t.Errorf("%s: passing case %+v", sc.name, c)
					}
					continue
				}
				d := r.Diagnostics[j]
				if c.Classname != d.RuleID || c.File != r.Path || c.Failure.Type != d.Severity.String() ||
					!strings.HasSuffix(c.Name, " "+d.RuleName) || !strings.HasPrefix(c.Failure.Details, r.Path+":") {
					t.Errorf("%s: failing case %+v", sc.name, c)
				}
			}
		}
		if tests != doc.Tests {
			t.Errorf("%s: testsuites tests=%d, sum of suites %d", sc.name, doc.Tests, tests)
		}
	}
}

func TestJUnitEscaping(t *testing.T) {
	msg := `<b>"quoted" & 'single' ]]> end` + "\n\x1b[31m\xff"
	r := FileResult{Path: `a&b<"c">.cnf`, Source: []byte("k=v\n"), Diagnostics: []diag.Diagnostic{{
		RuleID: "MDL999", Severity: diag.Warning, Span: diag.Span{Start: 0, End: 1}, Message: msg, Consequence: "x]]>y",
	}}}
	doc := decodeJUnit(t, render(t, "junit", []FileResult{r}, Options{}))
	c := doc.Suites[0].Cases[0]
	if doc.Suites[0].Name != r.Path || c.File != r.Path || c.Name != r.Path+":1:1 MDL999" {
		t.Errorf("suite %q, case %+v", doc.Suites[0].Name, c)
	}
	if want := sanitize(fullMessage(r.Diagnostics[0])); c.Failure.Message != want {
		t.Errorf("failure message = %q, want %q", c.Failure.Message, want)
	}
	if !strings.Contains(c.Failure.Details, sanitize(msg)) || !strings.Contains(c.Failure.Details, "= mydumper: x]]>y") {
		t.Errorf("failure details = %q", c.Failure.Details)
	}
}

// failAfter fails every write after the first n.
type failAfter struct{ n int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n == 0 {
		return 0, errWrite
	}
	f.n--
	return len(p), nil
}

func TestJUnitWriteErrors(t *testing.T) {
	results := scenarios()[0].results
	for n := range 3 {
		err := junitFormat{}.Write(&failAfter{n: n}, results, Summarize(results), Options{})
		if !errors.Is(err, errWrite) {
			t.Errorf("failing after %d writes: error = %v", n, err)
		}
	}
}
