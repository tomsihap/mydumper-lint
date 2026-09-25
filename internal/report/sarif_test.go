package report

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func decodeSARIF(t *testing.T, results []FileResult, opt Options) sarifLog {
	t.Helper()
	var log sarifLog
	if err := json.Unmarshal([]byte(render(t, "sarif", results, opt)), &log); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestSARIFStructure(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, sc := range scenarios() {
		log := decodeSARIF(t, sc.results, testOptions())
		if log.Schema != sarifSchema || log.Version != "2.1.0" || len(log.Runs) != 1 {
			t.Fatalf("%s: header %q %q, %d runs", sc.name, log.Schema, log.Version, len(log.Runs))
		}
		run := log.Runs[0]
		d := run.Tool.Driver
		if d.Name != "mydumper-lint" || d.Version != "1.2.3" || d.InformationURI != toolURI || len(d.Rules) != len(testRules()) {
			t.Errorf("%s: driver %+v", sc.name, d)
		}
		if run.ColumnKind != "unicodeCodePoints" || run.Results == nil {
			t.Errorf("%s: columnKind %q, results %v", sc.name, run.ColumnKind, run.Results)
		}
		seen := map[string]bool{}
		for _, res := range run.Results {
			if d.Rules[res.RuleIndex].ID != res.RuleID {
				t.Errorf("%s: ruleIndex %d points to %s, not %s", sc.name, res.RuleIndex, d.Rules[res.RuleIndex].ID, res.RuleID)
			}
			loc := res.Locations[0].PhysicalLocation
			if loc.ArtifactLocation.URIBaseID != "%SRCROOT%" || loc.Region.StartLine < 1 || loc.Region.StartColumn < 1 {
				t.Errorf("%s: location %+v", sc.name, loc)
			}
			fp := res.PartialFingerprints["primaryLocationLineHash"]
			if !hex64.MatchString(fp) || seen[fp] {
				t.Errorf("%s: fingerprint %q is malformed or duplicated", sc.name, fp)
			}
			seen[fp] = true
		}
	}
	levels := map[string]bool{}
	for _, res := range decodeSARIF(t, scenarios()[0].results, testOptions()).Runs[0].Results {
		levels[res.Level] = true
	}
	if !levels["error"] || !levels["warning"] || !levels["note"] || len(levels) != 3 {
		t.Errorf("levels = %v", levels)
	}
}

func TestSARIFRulesMissingFromMetadata(t *testing.T) {
	r := FileResult{Path: "a.cnf", Source: []byte("k=v\n"), Diagnostics: []diag.Diagnostic{
		{RuleID: "MDL401", RuleName: "unknown-option", Severity: diag.Error},
		{RuleID: "MDL102", RuleName: "whitespace-only-line", Severity: diag.Error},
		{RuleID: "MDL401", RuleName: "unknown-option", Severity: diag.Error},
		{RuleID: "X1", Severity: diag.Off},
	}}
	opt := Options{Rules: []RuleMeta{{ID: "MDL102", Name: "whitespace-only-line", Severity: diag.Error}}}
	run := decodeSARIF(t, []FileResult{r}, opt).Runs[0]
	var ids []string
	for _, rule := range run.Tool.Driver.Rules {
		ids = append(ids, rule.ID)
	}
	if strings.Join(ids, ",") != "MDL102,MDL401,X1" {
		t.Errorf("rules = %v", ids)
	}
	if rule := run.Tool.Driver.Rules[0]; rule.ShortDescription != nil || rule.HelpURI != "" || rule.Properties.Tags[0] != "loadability" {
		t.Errorf("rule without summary nor docs = %+v", rule)
	}
	if rule := run.Tool.Driver.Rules[1]; rule.Name != "unknown-option" || rule.DefaultConfiguration != nil || rule.Properties != nil {
		t.Errorf("synthesized rule = %+v", rule)
	}
	for i, want := range []int{1, 0, 1, 2} {
		if got := run.Results[i].RuleIndex; got != want {
			t.Errorf("result %d: ruleIndex = %d, want %d", i, got, want)
		}
	}
	if run.Results[3].Level != "none" {
		t.Errorf("level of an off diagnostic = %q", run.Results[3].Level)
	}
}

func TestRuleFamily(t *testing.T) {
	tests := map[string]string{"MDL001": "suppressions", "MDL112": "loadability", "MDL205": "groups", "MDL313": "lines",
		"MDL401": "options", "MDL502": "masking", "MDL601": "connection", "MDL905": "conventions", "MDL701": "", "MDL": "", "ABC123": ""}
	for id, want := range tests {
		if got := ruleFamily(id); got != want {
			t.Errorf("ruleFamily(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestSARIFArtifactURIs(t *testing.T) {
	tests := map[string]string{
		"conf/app.cnf":           "conf/app.cnf",
		"my dir/a b.cnf":         "my%20dir/a%20b.cnf",
		"a:b.cnf":                "./a:b.cnf",
		"x#y?%.cnf":              "x%23y%3F%25.cnf",
		"donn\xc3\xa9es/app.cnf": "donn%C3%A9es/app.cnf",
	}
	for path, want := range tests {
		if got := sarifArtifactOf(path); got.URI != want || got.URIBaseID != "%SRCROOT%" {
			t.Errorf("sarifArtifactOf(%q) = %+v, want %q relative to %%SRCROOT%%", path, got, want)
		}
	}
	abs, err := filepath.Abs("x y.cnf")
	if err != nil {
		t.Fatal(err)
	}
	got := sarifArtifactOf(abs)
	if !strings.HasPrefix(got.URI, "file:///") || !strings.HasSuffix(got.URI, "/x%20y.cnf") || got.URIBaseID != "" {
		t.Errorf("sarifArtifactOf(%q) = %+v", abs, got)
	}
}

func TestSARIFFixes(t *testing.T) {
	src := "k = v\n"
	r := FileResult{Path: "a.cnf", Source: []byte(src), Diagnostics: []diag.Diagnostic{
		{RuleID: "MDL307", Severity: diag.Info, Span: find(src, " = ", 0),
			Fix: &diag.Fix{Applicability: diag.Safe, Edits: []diag.Edit{{Start: 1, End: 4, New: "="}, {Start: 6, End: 6}}}},
		{RuleID: "MDL301", Severity: diag.Warning, Fix: &diag.Fix{Applicability: diag.Unsafe, Description: "nothing to do"}},
	}}
	res := decodeSARIF(t, []FileResult{r}, Options{}).Runs[0].Results
	if len(res[1].Fixes) != 0 {
		t.Errorf("a fix without edits must be omitted: %+v", res[1].Fixes)
	}
	fix := res[0].Fixes[0]
	if fix.Description != nil || fix.Properties.Applicability != "safe" {
		t.Errorf("fix = %+v", fix)
	}
	reps := fix.ArtifactChanges[0].Replacements
	if reps[0].InsertedContent.Text != "=" || *reps[0].DeletedRegion.ByteOffset != 1 || *reps[0].DeletedRegion.ByteLength != 3 ||
		reps[0].DeletedRegion.StartColumn != 2 || reps[0].DeletedRegion.EndColumn != 5 {
		t.Errorf("replacement = %+v", reps[0])
	}
	if reps[1].InsertedContent != nil || *reps[1].DeletedRegion.ByteLength != 0 || reps[1].DeletedRegion.StartLine != 2 {
		t.Errorf("insertion point after the final newline = %+v", reps[1])
	}
}

// shifted returns r with prefix inserted at the start of the file, every
// offset moved accordingly.
func shifted(r FileResult, prefix string) FileResult {
	n := len(prefix)
	move := func(sp diag.Span) diag.Span { return diag.Span{Start: sp.Start + n, End: sp.End + n} }
	out := r
	out.Source = append([]byte(prefix), r.Source...)
	out.Diagnostics = nil
	for _, d := range r.Diagnostics {
		d.Span = move(d.Span)
		var rel []diag.Related
		for _, x := range d.Related {
			rel = append(rel, diag.Related{Span: move(x.Span), Message: x.Message})
		}
		d.Related = rel
		out.Diagnostics = append(out.Diagnostics, d)
	}
	return out
}

func TestSARIFFingerprintsSurviveLineShifts(t *testing.T) {
	for _, r := range scenarios()[0].results {
		before := decodeSARIF(t, []FileResult{r}, Options{}).Runs[0].Results
		after := decodeSARIF(t, []FileResult{shifted(r, "# a new comment\n")}, Options{}).Runs[0].Results
		for i := range before {
			b, a := before[i], after[i]
			if a.Locations[0].PhysicalLocation.Region.StartLine != b.Locations[0].PhysicalLocation.Region.StartLine+1 {
				t.Fatalf("%s: the shift did not move result %d", r.Path, i)
			}
			if a.PartialFingerprints[fingerprintKey] != b.PartialFingerprints[fingerprintKey] {
				t.Errorf("%s: result %d (%s) changed fingerprint after a line shift", r.Path, i, b.RuleID)
			}
		}
	}
}
