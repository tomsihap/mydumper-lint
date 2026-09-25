package report

import (
	"encoding/json"
	"io"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// jsonFormat is the machine-readable format documented by
// schemas/output.v1.json. Version 1 only ever evolves additively.
type jsonFormat struct{}

func init() { Register(jsonFormat{}) }

func (jsonFormat) Name() string { return "json" }

// jsonVersion is the version of the JSON output layout.
const jsonVersion = 1

// The types below fix the field order of the output. Arrays are always
// allocated, so that they are written as [] rather than null.
type (
	jsonOutput struct {
		Version int         `json:"version"`
		Tool    jsonTool    `json:"tool"`
		Files   []jsonFile  `json:"files"`
		Summary jsonSummary `json:"summary"`
	}
	jsonTool struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	jsonFile struct {
		Path            string           `json:"path"`
		Loadable        bool             `json:"loadable"`
		MydumperVersion string           `json:"mydumper_version"`
		Fixed           int              `json:"fixed"`
		Diagnostics     []jsonDiagnostic `json:"diagnostics"`
	}
	jsonDiagnostic struct {
		ID          string        `json:"id"`
		Name        string        `json:"name"`
		Severity    string        `json:"severity"`
		Message     string        `json:"message"`
		Consequence string        `json:"consequence"`
		Range       jsonRange     `json:"range"`
		Related     []jsonRelated `json:"related"`
		Fix         *jsonFix      `json:"fix"`
	}
	jsonRange struct {
		Start jsonPosition `json:"start"`
		End   jsonPosition `json:"end"`
	}
	jsonPosition struct {
		Line   int `json:"line"`
		Col    int `json:"col"`
		Offset int `json:"offset"`
	}
	jsonRelated struct {
		Range   jsonRange `json:"range"`
		Message string    `json:"message"`
	}
	jsonFix struct {
		Applicability string     `json:"applicability"`
		Description   string     `json:"description"`
		Edits         []jsonEdit `json:"edits"`
	}
	jsonEdit struct {
		Range   jsonRange `json:"range"`
		NewText string    `json:"new_text"`
	}
	jsonSummary struct {
		Files         int `json:"files"`
		Errors        int `json:"errors"`
		Warnings      int `json:"warnings"`
		Infos         int `json:"infos"`
		Fixable       int `json:"fixable"`
		UnsafeFixable int `json:"unsafe_fixable"`
		Fixed         int `json:"fixed"`
	}
)

func (jsonFormat) Write(w io.Writer, results []FileResult, sum Summary, opt Options) error {
	out := jsonOutput{
		Version: jsonVersion,
		Tool:    jsonTool{Name: toolName, Version: opt.ToolVersion},
		Files:   make([]jsonFile, 0, len(results)),
		Summary: jsonSummary(sum),
	}
	for _, r := range results {
		out.Files = append(out.Files, jsonFileOf(r))
	}
	return writeJSON(w, out)
}

func jsonFileOf(r FileResult) jsonFile {
	v := newFileView(r)
	f := jsonFile{
		Path:            r.Path,
		Loadable:        r.Loadable,
		MydumperVersion: r.Version,
		Fixed:           r.Fixed,
		Diagnostics:     make([]jsonDiagnostic, 0, len(r.Diagnostics)),
	}
	for _, d := range r.Diagnostics {
		jd := jsonDiagnostic{
			ID:          d.RuleID,
			Name:        d.RuleName,
			Severity:    d.Severity.String(),
			Message:     d.Message,
			Consequence: d.Consequence,
			Range:       v.jsonRange(d.Span),
			Related:     make([]jsonRelated, 0, len(d.Related)),
		}
		for _, rel := range d.Related {
			jd.Related = append(jd.Related, jsonRelated{Range: v.jsonRange(rel.Span), Message: rel.Message})
		}
		if d.Fix != nil {
			jd.Fix = &jsonFix{
				Applicability: applicability(d.Fix),
				Description:   d.Fix.Description,
				Edits:         make([]jsonEdit, 0, len(d.Fix.Edits)),
			}
			for _, e := range d.Fix.Edits {
				jd.Fix.Edits = append(jd.Fix.Edits, jsonEdit{Range: v.jsonRange(e.Span()), NewText: e.New})
			}
		}
		f.Diagnostics = append(f.Diagnostics, jd)
	}
	return f
}

func (v fileView) jsonRange(sp diag.Span) jsonRange {
	s, e := v.span(sp)
	return jsonRange{Start: jsonPosition(s), End: jsonPosition(e)}
}

// writeJSON writes v as indented JSON. <, > and & are not escaped: the
// output is not meant to be embedded in HTML.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
