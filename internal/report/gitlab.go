package report

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// gitlabFormat writes a GitLab Code Quality report: a JSON array of issues
// (a subset of the Code Climate format) that GitLab shows in merge requests.
type gitlabFormat struct{}

func init() { Register(gitlabFormat{}) }

func (gitlabFormat) Name() string { return "gitlab" }

type (
	gitlabIssue struct {
		Description string         `json:"description"`
		CheckName   string         `json:"check_name"`
		Fingerprint string         `json:"fingerprint"`
		Severity    string         `json:"severity"`
		Location    gitlabLocation `json:"location"`
	}
	gitlabLocation struct {
		Path  string      `json:"path"`
		Lines gitlabLines `json:"lines"`
	}
	gitlabLines struct {
		Begin int `json:"begin"`
	}
)

func (gitlabFormat) Write(w io.Writer, results []FileResult, _ Summary, _ Options) error {
	issues := []gitlabIssue{}
	for _, r := range results {
		v := newFileView(r)
		fps := fingerprints(r, v)
		path := filepath.ToSlash(r.Path)
		for i, d := range r.Diagnostics {
			start, _ := v.span(d.Span)
			issues = append(issues, gitlabIssue{
				Description: strings.TrimSpace(ruleLabel(d) + " " + fullMessage(d)),
				CheckName:   d.RuleID,
				Fingerprint: fps[i],
				Severity:    gitlabSeverity(d.Severity),
				Location:    gitlabLocation{Path: path, Lines: gitlabLines{Begin: start.Line}},
			})
		}
	}
	return writeJSON(w, issues)
}

// gitlabSeverity maps severities to Code Quality ones: info, minor, critical.
func gitlabSeverity(s diag.Severity) string {
	switch s {
	case diag.Error:
		return "critical"
	case diag.Warning:
		return "minor"
	}
	return "info"
}
