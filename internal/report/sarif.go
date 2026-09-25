package report

import (
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// sarifFormat writes a SARIF 2.1.0 log with a single run, for GitHub code
// scanning and other static-analysis platforms.
type sarifFormat struct{}

func init() { Register(sarifFormat{}) }

func (sarifFormat) Name() string { return "sarif" }

const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"
	// srcRoot is the base of relative artifact URIs: the repository root.
	srcRoot = "%SRCROOT%"
	// fingerprintKey is the partial fingerprint GitHub code scanning uses
	// to match alerts across commits.
	fingerprintKey = "primaryLocationLineHash"
)

type (
	sarifLog struct {
		Schema  string     `json:"$schema"`
		Version string     `json:"version"`
		Runs    []sarifRun `json:"runs"`
	}
	sarifRun struct {
		Tool       sarifTool     `json:"tool"`
		ColumnKind string        `json:"columnKind"`
		Results    []sarifResult `json:"results"`
	}
	sarifTool struct {
		Driver sarifDriver `json:"driver"`
	}
	sarifDriver struct {
		Name           string      `json:"name"`
		Version        string      `json:"version,omitempty"`
		InformationURI string      `json:"informationUri"`
		Rules          []sarifRule `json:"rules"`
	}
	sarifRule struct {
		ID                   string           `json:"id"`
		Name                 string           `json:"name,omitempty"`
		ShortDescription     *sarifText       `json:"shortDescription,omitempty"`
		HelpURI              string           `json:"helpUri,omitempty"`
		DefaultConfiguration *sarifRuleConfig `json:"defaultConfiguration,omitempty"`
		Properties           *sarifTags       `json:"properties,omitempty"`
	}
	sarifText struct {
		Text string `json:"text"`
	}
	sarifRuleConfig struct {
		Level string `json:"level"`
	}
	sarifTags struct {
		Tags []string `json:"tags"`
	}
	sarifResult struct {
		RuleID              string            `json:"ruleId"`
		RuleIndex           int               `json:"ruleIndex"`
		Level               string            `json:"level"`
		Message             sarifText         `json:"message"`
		Locations           []sarifLocation   `json:"locations"`
		RelatedLocations    []sarifLocation   `json:"relatedLocations,omitempty"`
		Fixes               []sarifFix        `json:"fixes,omitempty"`
		PartialFingerprints map[string]string `json:"partialFingerprints"`
	}
	sarifLocation struct {
		ID               *int          `json:"id,omitempty"`
		PhysicalLocation sarifPhysical `json:"physicalLocation"`
		Message          *sarifText    `json:"message,omitempty"`
	}
	sarifPhysical struct {
		ArtifactLocation sarifArtifact `json:"artifactLocation"`
		Region           sarifRegion   `json:"region"`
	}
	sarifArtifact struct {
		URI       string `json:"uri"`
		URIBaseID string `json:"uriBaseId,omitempty"`
	}
	sarifRegion struct {
		StartLine   int  `json:"startLine"`
		StartColumn int  `json:"startColumn"`
		EndLine     int  `json:"endLine"`
		EndColumn   int  `json:"endColumn"` // exclusive
		ByteOffset  *int `json:"byteOffset,omitempty"`
		ByteLength  *int `json:"byteLength,omitempty"`
	}
	sarifFix struct {
		Description     *sarifText            `json:"description,omitempty"`
		ArtifactChanges []sarifArtifactChange `json:"artifactChanges"`
		Properties      sarifFixProperties    `json:"properties"`
	}
	sarifArtifactChange struct {
		ArtifactLocation sarifArtifact      `json:"artifactLocation"`
		Replacements     []sarifReplacement `json:"replacements"`
	}
	sarifReplacement struct {
		DeletedRegion   sarifRegion `json:"deletedRegion"`
		InsertedContent *sarifText  `json:"insertedContent,omitempty"`
	}
	sarifFixProperties struct {
		Applicability string `json:"applicability"`
	}
)

func (sarifFormat) Write(w io.Writer, results []FileResult, _ Summary, opt Options) error {
	rules, index := sarifRules(opt.Rules, results)
	run := sarifRun{
		Tool:       sarifTool{Driver: sarifDriver{Name: toolName, Version: opt.ToolVersion, InformationURI: toolURI, Rules: rules}},
		ColumnKind: "unicodeCodePoints",
		Results:    []sarifResult{},
	}
	for _, r := range results {
		v := newFileView(r)
		artifact := sarifArtifactOf(r.Path)
		fps := fingerprints(r, v)
		for i, d := range r.Diagnostics {
			res := sarifResult{
				RuleID:              d.RuleID,
				RuleIndex:           index[d.RuleID],
				Level:               sarifLevel(d.Severity),
				Message:             sarifText{Text: fullMessage(d)},
				Locations:           []sarifLocation{{PhysicalLocation: sarifPhysical{artifact, v.sarifRegion(d.Span, false)}}},
				PartialFingerprints: map[string]string{fingerprintKey: fps[i]},
			}
			for j, rel := range d.Related {
				loc := sarifLocation{ID: new(j + 1), PhysicalLocation: sarifPhysical{artifact, v.sarifRegion(rel.Span, false)}}
				if rel.Message != "" {
					loc.Message = &sarifText{Text: rel.Message}
				}
				res.RelatedLocations = append(res.RelatedLocations, loc)
			}
			if d.Fix != nil && len(d.Fix.Edits) > 0 {
				res.Fixes = []sarifFix{v.sarifFix(d.Fix, artifact)}
			}
			run.Results = append(run.Results, res)
		}
	}
	return writeJSON(w, sarifLog{Schema: sarifSchema, Version: sarifVersion, Runs: []sarifRun{run}})
}

// sarifRules returns tool.driver.rules and the index of each rule ID in it.
// Rules that diagnostics use but that are missing from the metadata are
// appended with their ID and name, so that every ruleIndex is valid.
func sarifRules(meta []RuleMeta, results []FileResult) ([]sarifRule, map[string]int) {
	rules := make([]sarifRule, 0, len(meta))
	index := make(map[string]int, len(meta))
	for _, m := range meta {
		rule := sarifRule{
			ID: m.ID, Name: m.Name, HelpURI: m.DocsURL,
			DefaultConfiguration: &sarifRuleConfig{Level: sarifLevel(m.Severity)},
		}
		if m.Summary != "" {
			rule.ShortDescription = &sarifText{Text: m.Summary}
		}
		if family := ruleFamily(m.ID); family != "" {
			rule.Properties = &sarifTags{Tags: []string{family}}
		}
		index[m.ID] = len(rules)
		rules = append(rules, rule)
	}
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if _, ok := index[d.RuleID]; !ok {
				index[d.RuleID] = len(rules)
				rules = append(rules, sarifRule{ID: d.RuleID, Name: d.RuleName})
			}
		}
	}
	return rules, index
}

// ruleFamily names the family of a rule ID (design §6.3).
func ruleFamily(id string) string {
	if len(id) < 4 || !strings.HasPrefix(id, "MDL") {
		return ""
	}
	return map[byte]string{
		'0': "suppressions", '1': "loadability", '2': "groups", '3': "lines",
		'4': "options", '5': "masking", '6': "connection", '9': "conventions",
	}[id[3]]
}

func sarifLevel(s diag.Severity) string {
	switch s {
	case diag.Error:
		return "error"
	case diag.Warning:
		return "warning"
	case diag.Info:
		return "note"
	case diag.Off:
	}
	return "none"
}

// sarifArtifactOf returns the location of a file: a URI relative to
// %SRCROOT%, or a file:// URI for an absolute path.
func sarifArtifactOf(path string) sarifArtifact {
	p := filepath.ToSlash(path)
	if filepath.IsAbs(path) {
		if !strings.HasPrefix(p, "/") {
			p = "/" + p // a Windows drive letter: file:///C:/...
		}
		return sarifArtifact{URI: (&url.URL{Scheme: "file", Path: p}).String()}
	}
	return sarifArtifact{URI: (&url.URL{Path: p}).String(), URIBaseID: srcRoot}
}

// sarifRegion converts a span; withBytes adds its byte offset and length,
// which fixes need to be applied exactly.
func (v fileView) sarifRegion(sp diag.Span, withBytes bool) sarifRegion {
	s, e := v.span(sp)
	r := sarifRegion{StartLine: s.Line, StartColumn: s.Col, EndLine: e.Line, EndColumn: e.Col}
	if withBytes {
		r.ByteOffset, r.ByteLength = new(s.Offset), new(e.Offset-s.Offset)
	}
	return r
}

func (v fileView) sarifFix(f *diag.Fix, artifact sarifArtifact) sarifFix {
	change := sarifArtifactChange{ArtifactLocation: artifact, Replacements: make([]sarifReplacement, 0, len(f.Edits))}
	for _, e := range f.Edits {
		rep := sarifReplacement{DeletedRegion: v.sarifRegion(e.Span(), true)}
		if e.New != "" {
			rep.InsertedContent = &sarifText{Text: e.New}
		}
		change.Replacements = append(change.Replacements, rep)
	}
	fix := sarifFix{ArtifactChanges: []sarifArtifactChange{change}, Properties: sarifFixProperties{Applicability: applicability(f)}}
	if f.Description != "" {
		fix.Description = &sarifText{Text: f.Description}
	}
	return fix
}
