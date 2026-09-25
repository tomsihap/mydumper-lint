// Package report renders lint results in the output formats of design §10:
// text, concise, json, sarif, github, junit and gitlab.
package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// FileResult is the outcome of linting one file.
type FileResult struct {
	Path        string
	Source      []byte            // original bytes, for positions and excerpts
	Loadable    bool              // whether mydumper would load the file
	Version     string            // resolved mydumper version, e.g. "v0.19.3-3"
	Diagnostics []diag.Diagnostic // sorted with diag.Sort
	Fixed       int               // number of fixes applied with --fix
}

// Summary aggregates counts over all files.
type Summary struct {
	Files         int
	Errors        int
	Warnings      int
	Infos         int
	Fixable       int // diagnostics with a safe fix
	UnsafeFixable int // diagnostics with an unsafe fix
	Fixed         int
}

// RuleMeta is the rule information some formats embed (SARIF rules, links).
type RuleMeta struct {
	ID       string
	Name     string
	Summary  string
	Severity diag.Severity // default severity
	Fix      string        // "safe", "unsafe" or "" when the rule has no fix
	DocsURL  string
}

// Options controls rendering.
type Options struct {
	Color       bool       // ANSI colors (text formats only)
	Quiet       bool       // no summary footer (text formats only)
	ToolVersion string     // mydumper-lint version
	Rules       []RuleMeta // every known rule, for SARIF tool.driver.rules
}

// Format renders results to w.
type Format interface {
	Name() string
	Write(w io.Writer, results []FileResult, sum Summary, opt Options) error
}

var formats = map[string]Format{}

// Register makes a format available by name. It panics on duplicates, which
// can only come from a programming error.
func Register(f Format) {
	if _, dup := formats[f.Name()]; dup {
		panic("report: duplicate format " + f.Name())
	}
	formats[f.Name()] = f
}

// Get returns the format called name.
func Get(name string) (Format, error) {
	if f, ok := formats[name]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("unknown format %q (available: %v)", name, Names())
}

// Names lists the registered formats in alphabetical order.
func Names() []string {
	names := make([]string, 0, len(formats))
	for n := range formats {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Summarize counts diagnostics by severity and fix availability.
func Summarize(results []FileResult) Summary {
	s := Summary{Files: len(results)}
	for _, r := range results {
		s.Fixed += r.Fixed
		for _, d := range r.Diagnostics {
			switch d.Severity {
			case diag.Error:
				s.Errors++
			case diag.Warning:
				s.Warnings++
			case diag.Info:
				s.Infos++
			case diag.Off:
			}
			if d.Fix != nil {
				if d.Fix.Applicability == diag.Safe {
					s.Fixable++
				} else {
					s.UnsafeFixable++
				}
			}
		}
	}
	return s
}
