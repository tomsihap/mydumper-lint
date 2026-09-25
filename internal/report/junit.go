package report

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// junitFormat writes a JUnit XML report: one test suite per file, one
// failing test case per diagnostic, and one passing test case for a file
// without diagnostics. CircleCI (store_test_results), Jenkins and GitLab
// read it.
type junitFormat struct{}

func init() { Register(junitFormat{}) }

func (junitFormat) Name() string { return "junit" }

type (
	junitSuites struct {
		XMLName  xml.Name     `xml:"testsuites"`
		Name     string       `xml:"name,attr"`
		Tests    int          `xml:"tests,attr"`
		Failures int          `xml:"failures,attr"`
		Errors   int          `xml:"errors,attr"`
		Suites   []junitSuite `xml:"testsuite"`
	}
	junitSuite struct {
		Name     string      `xml:"name,attr"`
		Tests    int         `xml:"tests,attr"`
		Failures int         `xml:"failures,attr"`
		Errors   int         `xml:"errors,attr"`
		Cases    []junitCase `xml:"testcase"`
	}
	junitCase struct {
		Name      string        `xml:"name,attr"`
		Classname string        `xml:"classname,attr"`
		File      string        `xml:"file,attr"`
		Failure   *junitFailure `xml:"failure"`
	}
	junitFailure struct {
		Message string `xml:"message,attr"`
		Type    string `xml:"type,attr"`
		Details string `xml:",cdata"`
	}
)

func (junitFormat) Write(w io.Writer, results []FileResult, _ Summary, _ Options) error {
	doc := junitSuites{Name: toolName}
	for _, r := range results {
		v := newFileView(r)
		path := sanitize(r.Path)
		suite := junitSuite{Name: path, Failures: len(r.Diagnostics)}
		for _, d := range r.Diagnostics {
			start, _ := v.span(d.Span)
			name := d.RuleName
			if name == "" {
				name = d.RuleID
			}
			var details strings.Builder
			writeBlock(&details, v, d, painter(false))
			suite.Cases = append(suite.Cases, junitCase{
				Name:      path + ":" + strconv.Itoa(start.Line) + ":" + strconv.Itoa(start.Col) + " " + sanitize(name),
				Classname: sanitize(d.RuleID),
				File:      path,
				Failure: &junitFailure{
					Message: sanitize(fullMessage(d)),
					Type:    d.Severity.String(),
					Details: strings.TrimSuffix(details.String(), "\n"),
				},
			})
		}
		if len(suite.Cases) == 0 {
			suite.Cases = []junitCase{{Name: path, Classname: toolName, File: path}}
		}
		suite.Tests = len(suite.Cases)
		doc.Tests += suite.Tests
		doc.Failures += suite.Failures
		doc.Suites = append(doc.Suites, suite)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
