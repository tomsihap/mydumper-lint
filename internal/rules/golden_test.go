package rules_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/lint"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/rules"
	"github.com/tomsihap/mydumper-lint/internal/source"
	"github.com/tomsihap/mydumper-lint/internal/target"
	"github.com/tomsihap/mydumper-lint/internal/txtar"
)

var update = flag.Bool("update", false, "rewrite the expected sections of golden files")

// A golden case is testdata/<RULE ID>/<name>.txtar with sections:
//
//	input.cnf[.esc]        the file to lint (required)
//	options                optional "key: value" lines: select, ignore (comma-separated),
//	                       mydumper-version (default: none, GLib-level rules only),
//	                       path (the file's name), base-dir, conventions (JSON),
//	                       load-set (input.cnf is then the extra file of defaults.cnf),
//	                       doc-example (the case docs/rules shows)
//	defaults.cnf[.esc]     the defaults file of a load set
//	diagnostics            expected diagnostics (see formatDiagnostics)
//	fixed.cnf[.esc]        expected output of --fix (absent: no change)
//	fixed-unsafe.cnf[.esc] expected output of --fix --unsafe-fixes (absent: same as fixed)
//
// By default only the rule named by the directory is enabled.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "MDL*", "*.txtar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		t.Run(filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path), func(t *testing.T) {
			runGolden(t, path)
		})
	}
}

type caseResult struct {
	rule        string
	diagnostics int // diagnostics of the rule under test
}

var results = map[string][]caseResult{}

func runGolden(t *testing.T, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a := txtar.Parse(raw)
	input, ok, err := a.Get("input.cnf")
	if !ok || err != nil {
		t.Fatalf("missing or invalid input.cnf section (err %v)", err)
	}
	ruleID := filepath.Base(filepath.Dir(path))
	sel := rules.Selection{Select: []string{ruleID}}
	cfg := lint.Config{}
	name, loadSet := "input.cnf", false
	if opts, ok, _ := a.Get("options"); ok {
		for _, line := range strings.Split(string(opts), "\n") {
			k, v, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			list := strings.Split(strings.ReplaceAll(v, " ", ""), ",")
			switch strings.TrimSpace(k) {
			case "select":
				sel.Select = list
			case "ignore":
				sel.Ignore = list
			case "path":
				name = strings.TrimSpace(v)
			case "base-dir":
				cfg.BaseDir = strings.TrimSpace(v)
			case "load-set":
				loadSet = true
			case "doc-example": // tools/gendocs shows this case in the rule's page
			case "conventions":
				var c struct {
					FilenamePattern string              `json:"filename-pattern"`
					Values          map[string]string   `json:"values"`
					TableSchema     string              `json:"table-schema"`
					Required        map[string][]string `json:"required"`
					Forbidden       map[string][]string `json:"forbidden"`
				}
				if err := json.Unmarshal([]byte(v), &c); err != nil {
					t.Fatalf("conventions: %v", err)
				}
				cfg.Conventions = &rules.Conventions{
					FilenamePattern: c.FilenamePattern, Values: c.Values, TableSchema: c.TableSchema,
					Required: c.Required, Forbidden: c.Forbidden,
				}
			case "mydumper-version":
				tg, err := target.Resolve(strings.TrimSpace(v), optionsdb.DefaultBuild)
				if err != nil {
					t.Fatal(err)
				}
				tg.Configure(&cfg)
			default:
				t.Fatalf("unknown option %q", k)
			}
		}
	}
	cfg.Selection = sel
	l, err := lint.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res := l.Check(name, input)
	if loadSet {
		defaults, ok, err := a.Get("defaults.cnf")
		if !ok || err != nil {
			t.Fatalf("load-set needs a defaults.cnf section (err %v)", err)
		}
		_, onExtra := l.CheckSet(l.Check("defaults.cnf", defaults), res)
		res.Diagnostics = append(res.Diagnostics, onExtra...)
		diag.Sort(res.Diagnostics)
	}
	n := 0
	for _, d := range res.Diagnostics {
		if d.RuleID == ruleID {
			n++
		}
	}
	results[ruleID] = append(results[ruleID], caseResult{rule: ruleID, diagnostics: n})

	got := map[string][]byte{"diagnostics": []byte(formatDiagnostics(res.File, res.Diagnostics))}
	safe, err := l.Fix(name, input, fix.Options{})
	if err != nil {
		t.Fatalf("fix: %v", err)
	}
	unsafe, err := l.Fix(name, input, fix.Options{Unsafe: true})
	if err != nil {
		t.Fatalf("fix --unsafe-fixes: %v", err)
	}
	// Idempotence: fixing the fixed output again, with the same options,
	// changes nothing.
	for _, run := range []struct {
		out    []byte
		unsafe bool
	}{{safe.Output, false}, {unsafe.Output, true}} {
		again, err := l.Fix("input.cnf", run.out, fix.Options{Unsafe: run.unsafe})
		if err != nil || !bytes.Equal(again.Output, run.out) {
			t.Errorf("fix is not idempotent (unsafe=%v): %q then %q (err %v)", run.unsafe, run.out, again.Output, err)
		}
	}
	if !bytes.Equal(safe.Output, input) {
		got["fixed.cnf"] = safe.Output
	}
	if !bytes.Equal(unsafe.Output, safe.Output) {
		got["fixed-unsafe.cnf"] = unsafe.Output
	}

	if *update {
		for _, name := range []string{"diagnostics", "fixed.cnf", "fixed-unsafe.cnf"} {
			if b, ok := got[name]; ok {
				if name == "diagnostics" {
					a.Delete(name)
					a.Files = append(a.Files, txtar.File{Name: name, Data: b})
				} else {
					a.Set(name, b)
				}
			} else {
				a.Delete(name)
			}
		}
		if err := os.WriteFile(path, txtar.Format(a), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, name := range []string{"diagnostics", "fixed.cnf", "fixed-unsafe.cnf"} {
		want, ok, err := a.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		g, has := got[name]
		switch {
		case !ok && has:
			t.Errorf("%s: unexpected output (run with -update to record it):\n%s", name, g)
		case ok && !has && name != "diagnostics":
			t.Errorf("%s: expected\n%s\nbut the fixer changed nothing", name, want)
		case ok && !bytes.Equal(g, want):
			t.Errorf("%s mismatch (run with -update to record it)\n got:\n%s\nwant:\n%s", name, g, want)
		}
	}
}

// formatDiagnostics renders diagnostics in the stable golden form:
//
//	L:C-L:C severity ID[name] message
//	  = consequence
//	  note L:C-L:C: related message
//	  fix (safe): description
func formatDiagnostics(f *source.File, ds []diag.Diagnostic) string {
	if len(ds) == 0 {
		return "(none)\n"
	}
	var b strings.Builder
	pos := func(s diag.Span) string {
		p, q := f.Position(s.Start), f.Position(s.End)
		return fmt.Sprintf("%d:%d-%d:%d", p.Line, p.Col, q.Line, q.Col)
	}
	for _, d := range ds {
		fmt.Fprintf(&b, "%s %s %s[%s] %s\n", pos(d.Span), d.Severity, d.RuleID, d.RuleName, d.Message)
		if d.Consequence != "" {
			fmt.Fprintf(&b, "  = %s\n", d.Consequence)
		}
		for _, r := range d.Related {
			fmt.Fprintf(&b, "  note %s: %s\n", pos(r.Span), r.Message)
		}
		if d.Fix != nil {
			fmt.Fprintf(&b, "  fix (%s): %s\n", d.Fix.Applicability, d.Fix.Description)
		}
	}
	return b.String()
}

// TestMain runs the golden tests, then checks that every rule has at least one
// positive and one negative golden case (design §11.8).
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 && flag.Lookup("test.run").Value.String() == "" && !*update {
		// MDL109 is the safety net: no known input reaches it, and
		// keyfile.FuzzNoUnknownCause checks that none does.
		exemptPositive := map[string]bool{"MDL109": true}
		var missing []string
		for _, r := range rules.All() {
			pos, neg := exemptPositive[r.ID], false
			for _, c := range results[r.ID] {
				pos = pos || c.diagnostics > 0
				neg = neg || c.diagnostics == 0
			}
			if !pos || !neg {
				missing = append(missing, fmt.Sprintf("%s (positive: %v, negative: %v)", r.ID, pos, neg))
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			fmt.Fprintf(os.Stderr, "rules without positive and negative golden cases:\n  %s\n", strings.Join(missing, "\n  "))
			code = 1
		}
	}
	os.Exit(code)
}
