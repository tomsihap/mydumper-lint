package report

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

func decodeGitLab(t *testing.T, results []FileResult) []gitlabIssue {
	t.Helper()
	var issues []gitlabIssue
	if err := json.Unmarshal([]byte(render(t, "gitlab", results, Options{})), &issues); err != nil {
		t.Fatal(err)
	}
	return issues
}

func TestGitLabIssues(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	results := scenarios()[0].results
	issues := decodeGitLab(t, results)
	sarif := decodeSARIF(t, results, testOptions()).Runs[0].Results
	sum := Summarize(results)
	if len(issues) != sum.Errors+sum.Warnings+sum.Infos {
		t.Fatalf("%d issues for %+v", len(issues), sum)
	}
	severities := map[string]string{"error": "critical", "warning": "minor", "info": "info"}
	seen := map[string]bool{}
	i := 0
	for _, r := range results {
		for _, d := range r.Diagnostics {
			is := issues[i]
			if is.CheckName != d.RuleID || is.Severity != severities[d.Severity.String()] || is.Location.Path != r.Path ||
				is.Location.Lines.Begin < 1 || is.Description != ruleLabel(d)+" "+fullMessage(d) {
				t.Errorf("issue %d = %+v for %+v", i, is, d)
			}
			if !hex64.MatchString(is.Fingerprint) || seen[is.Fingerprint] {
				t.Errorf("issue %d: fingerprint %q is malformed or duplicated", i, is.Fingerprint)
			}
			if is.Fingerprint != sarif[i].PartialFingerprints[fingerprintKey] {
				t.Errorf("issue %d: GitLab and SARIF fingerprints differ", i)
			}
			seen[is.Fingerprint] = true
			i++
		}
	}
	if got := render(t, "gitlab", nil, Options{}); got != "[]\n" {
		t.Errorf("no files: %q", got)
	}
	if gitlabSeverity(diag.Off) != "info" {
		t.Error("an off diagnostic must map to info")
	}
}

func TestGitLabFingerprintsSurviveLineShifts(t *testing.T) {
	for _, r := range scenarios()[0].results {
		before, after := decodeGitLab(t, []FileResult{r}), decodeGitLab(t, []FileResult{shifted(r, "[client]\n\n")})
		for i := range before {
			if after[i].Location.Lines.Begin != before[i].Location.Lines.Begin+2 {
				t.Fatalf("%s: the shift did not move issue %d", r.Path, i)
			}
			if after[i].Fingerprint != before[i].Fingerprint {
				t.Errorf("%s: issue %d (%s) changed fingerprint after a line shift", r.Path, i, before[i].CheckName)
			}
		}
	}
}

func TestFingerprints(t *testing.T) {
	fp := func(path, src string, ds ...diag.Diagnostic) []string {
		r := FileResult{Path: path, Source: []byte(src), Diagnostics: ds}
		return fingerprints(r, newFileView(r))
	}
	d := func(id string, start int) diag.Diagnostic {
		return diag.Diagnostic{RuleID: id, Span: diag.Span{Start: start, End: start + 1}}
	}
	// The same key twice: ranks 0 and 1 keep them distinct and stable.
	twice := fp("a.cnf", "k=1\nk=1\n", d("MDL301", 0), d("MDL301", 4))
	if twice[0] == twice[1] {
		t.Error("identical lines must get distinct fingerprints")
	}
	if got := fp("a.cnf", "x\nk=1\nk=1\n", d("MDL301", 2), d("MDL301", 6)); got[0] != twice[0] || got[1] != twice[1] {
		t.Error("fingerprints changed after a line shift")
	}
	// Indentation, inner spacing and line endings do not matter.
	base := fp("a.cnf", "k = 1\n", d("MDL301", 0))[0]
	for _, src := range []string{"  k   =\t1\r\n", "k = 1"} {
		if got := fp("a.cnf", src, d("MDL301", 0))[0]; got != base {
			t.Errorf("fingerprint of %q differs from %q", src, "k = 1\n")
		}
	}
	// The rule, the path and the line content do matter.
	for name, got := range map[string]string{
		"rule":    fp("a.cnf", "k = 1\n", d("MDL302", 0))[0],
		"path":    fp("b.cnf", "k = 1\n", d("MDL301", 0))[0],
		"content": fp("a.cnf", "k = 2\n", d("MDL301", 0))[0],
	} {
		if got == base {
			t.Errorf("changing the %s must change the fingerprint", name)
		}
	}
	// Paths are hashed in slash form, so that runs on Windows and on Unix
	// agree (a backslash is a separator on Windows only).
	native := fp(`dir\a.cnf`, "k = 1\n", d("MDL301", 0))[0]
	slash := fp("dir/a.cnf", "k = 1\n", d("MDL301", 0))[0]
	if (native == slash) != (filepath.Separator == '\\') {
		t.Error("paths are not hashed in slash form")
	}
}
