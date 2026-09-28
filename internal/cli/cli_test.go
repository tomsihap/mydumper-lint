package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &out, &errb)
	return result{code, out.String(), errb.String()}
}

// workspace creates a repository root (with .git, so configuration discovery
// stops there) holding the given files.
func workspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const (
	clean   = "[mydumper]\nthreads=4\n"
	broken  = "[mydumper]\nthreads=4\n  \nroutines\n"
	warning = "[mydumper]\nthreads=4\n# see [docs]\n"
)

func TestVersionAndHelp(t *testing.T) {
	if r := run(t, "", "version"); r.code != 0 || !strings.HasPrefix(r.stdout, "mydumper-lint ") {
		t.Errorf("version: %+v", r)
	}
	if r := run(t, "", "--version"); r.code != 0 || !strings.HasPrefix(r.stdout, "mydumper-lint ") {
		t.Errorf("--version: %+v", r)
	}
	r := run(t, "", "--help")
	for _, want := range []string{"check", "inspect", "explain", "completion"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	if r := run(t, "", "help", "check"); r.code != 0 || !strings.Contains(r.stdout, "--fail-on") {
		t.Errorf("help check: %+v", r)
	}
	if r := run(t, "", "check", "--help"); r.code != 0 || !strings.Contains(r.stdout, "Exit codes") {
		t.Errorf("check --help: %+v", r)
	}
}

func TestCheckExitCodes(t *testing.T) {
	root := workspace(t, map[string]string{"ok.cnf": clean, "bad.cnf": broken, "warn.cnf": warning})
	if r := run(t, "", "check", filepath.Join(root, "ok.cnf")); r.code != ExitOK {
		t.Errorf("clean file: %+v", r)
	}
	r := run(t, "", "check", root, "--color", "never")
	if r.code != ExitFindings || !strings.Contains(r.stdout, "MDL102[whitespace-only-line]") || !strings.Contains(r.stdout, "impact:") {
		t.Errorf("directory: %+v", r)
	}
	if r := run(t, "", "check", filepath.Join(root, "warn.cnf")); r.code != ExitOK {
		t.Errorf("a warning does not fail by default: %+v", r)
	}
	if r := run(t, "", "check", "--fail-on", "warning", filepath.Join(root, "warn.cnf")); r.code != ExitFindings {
		t.Errorf("--fail-on warning: %+v", r)
	}
	if r := run(t, "", "check", "--fail-on", "none", root); r.code != ExitOK {
		t.Errorf("--fail-on none: %+v", r)
	}
	// Running without a subcommand means check.
	if r := run(t, "", filepath.Join(root, "bad.cnf")); r.code != ExitFindings {
		t.Errorf("implicit check: %+v", r)
	}
}

func TestCheckUsageErrors(t *testing.T) {
	tests := map[string][]string{
		"did you mean --fix": {"check", "--fixx"},
		"--fail-on must be":  {"check", "--fail-on", "fatal"},
		"--color must be":    {"check", "--color", "rainbow"},
		"needs --fix":        {"check", "--unsafe-fixes"},
		"cannot be used":     {"check", "--fix", "--diff"},
		"unknown format":     {"check", "--format", "xml"},
		"needs a value":      {"check", "--select"},
		"no such file":       {"check", "does-not-exist.cnf"},
		`did you mean "MDL1`: {"check", "--select", "MDL1O2", "."},
	}
	for want, args := range tests {
		r := run(t, "", args...)
		if r.code != ExitError || !strings.Contains(r.stderr, want) {
			t.Errorf("%v: code %d, stderr %q; want %q", args, r.code, r.stderr, want)
		}
	}
}

func TestFormats(t *testing.T) {
	root := workspace(t, map[string]string{"bad.cnf": broken})
	path := filepath.Join(root, "bad.cnf")
	r := run(t, "", "check", "--format", "json", path)
	var doc struct {
		Version int `json:"version"`
		Files   []struct {
			Loadable    bool `json:"loadable"`
			Diagnostics []struct {
				ID string `json:"id"`
			} `json:"diagnostics"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.Version != 1 || doc.Files[0].Loadable ||
		doc.Files[0].Diagnostics[0].ID != "MDL102" {
		t.Errorf("json: %v %+v", err, doc)
	}
	sarif := filepath.Join(root, "out.sarif")
	r = run(t, "", "check", path, "--format", "concise", "--format", "sarif="+sarif, "--format", "github")
	if !strings.Contains(r.stdout, "bad.cnf:3:1: error MDL102") || !strings.Contains(r.stdout, "::error file=") {
		t.Errorf("concise+github on stdout: %q", r.stdout)
	}
	if b, err := os.ReadFile(sarif); err != nil || !bytes.Contains(b, []byte(`"version": "2.1.0"`)) {
		t.Errorf("sarif file: %v %.200s", err, b)
	}
	r = run(t, "", "check", "--statistics", root)
	if !strings.Contains(r.stdout, "MDL102  whitespace-only-line [*]") {
		t.Errorf("statistics: %q", r.stdout)
	}
}

func TestFixAndDiff(t *testing.T) {
	root := workspace(t, map[string]string{"bad.cnf": broken})
	path := filepath.Join(root, "bad.cnf")
	r := run(t, "", "check", "--diff", path)
	if r.code != ExitFindings || !strings.Contains(r.stdout, "-  \n-routines\n+\n+routines=1\n") {
		t.Errorf("diff: %+v", r)
	}
	if b, _ := os.ReadFile(path); string(b) != broken {
		t.Error("--diff must not write")
	}
	r = run(t, "", "check", path, "--fix") // flag after the path
	if r.code != ExitOK {
		t.Errorf("fix: %+v", r)
	}
	if b, _ := os.ReadFile(path); string(b) != "[mydumper]\nthreads=4\n\nroutines=1\n" {
		t.Errorf("fixed content: %q", b)
	}
	// stdin: the fixed file goes to stdout, diagnostics to stderr.
	r = run(t, broken, "check", "--fix", "--stdin-filename", "conf/app.cnf", "-")
	if r.stdout != "[mydumper]\nthreads=4\n\nroutines=1\n" || r.code != ExitOK {
		t.Errorf("stdin fix: %+v", r)
	}
	r = run(t, broken, "check", "-")
	if r.code != ExitFindings || !strings.Contains(r.stdout, "<stdin>:3:1") {
		t.Errorf("stdin check: %+v", r)
	}
}

func TestSelection(t *testing.T) {
	root := workspace(t, map[string]string{"bad.cnf": broken, "warn.cnf": warning})
	if r := run(t, "", "check", "--ignore", "MDL102", filepath.Join(root, "bad.cnf")); r.code != ExitOK {
		t.Errorf("--ignore: %+v", r)
	}
	if r := run(t, "", "check", "--select", "MDL3", filepath.Join(root, "bad.cnf")); r.code != ExitOK {
		t.Errorf("--select MDL3: %+v", r)
	}
	r := run(t, "", "check", "--select", "bracket-leak-risk", "--format", "concise", root)
	if !strings.Contains(r.stdout, "MDL112") || strings.Contains(r.stdout, "MDL102") {
		t.Errorf("--select by name: %q", r.stdout)
	}
}

func TestConfigurationDiscoveryAndOverrides(t *testing.T) {
	root := workspace(t, map[string]string{
		".mydumper-lint.yaml": "rules:\n  ignore: [MDL102, MDL308]\nexclude: [\"skipped/**\"]\noverrides:\n  - files: [\"strict/*.cnf\"]\n    rules:\n      ignore: []\n",
		"a/bad.cnf":           broken,
		"strict/bad.cnf":      broken,
		"skipped/bad.cnf":     broken,
	})
	r := run(t, "", "check", "--format", "concise", root)
	if strings.Contains(r.stdout, "a/bad.cnf") || !strings.Contains(r.stdout, "strict/bad.cnf") || strings.Contains(r.stdout, "skipped") {
		t.Errorf("config: %q", r.stdout)
	}
	if r := run(t, "", "check", "--no-config", "--format", "concise", filepath.Join(root, "a", "bad.cnf")); !strings.Contains(r.stdout, "MDL102") {
		t.Errorf("--no-config: %q", r.stdout)
	}
	cfg := filepath.Join(root, "other.yaml")
	if err := os.WriteFile(cfg, []byte("fail-on: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "", "check", "--config", cfg, root); r.code != ExitOK {
		t.Errorf("--config: %+v", r)
	}
	bad := filepath.Join(root, "broken.yaml")
	if err := os.WriteFile(bad, []byte("rulez: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "", "check", "--config", bad, root); r.code != ExitError || !strings.Contains(r.stderr, `did you mean "rules"`) {
		t.Errorf("broken config: %+v", r)
	}
	if r := run(t, "", "check", "--exclude", "strict/**", "--format", "concise", root); strings.Contains(r.stdout, "strict/") {
		t.Errorf("--exclude: %q", r.stdout)
	}
	if r := run(t, "", "config", "path", filepath.Join(root, "a", "bad.cnf")); !strings.HasSuffix(strings.TrimSpace(r.stdout), ".mydumper-lint.yaml") {
		t.Errorf("config path: %+v", r)
	}
	r = run(t, "", "config", "show", filepath.Join(root, "strict", "bad.cnf"))
	if r.code != 0 || !strings.Contains(r.stdout, `"ignore": []`) {
		t.Errorf("config show: %+v", r)
	}
	if r := run(t, "", "config", "schema"); !strings.Contains(r.stdout, `"$schema"`) {
		t.Errorf("config schema: %+v", r)
	}
	if r := run(t, "", "config", "nope"); r.code != ExitError {
		t.Errorf("config nope: %+v", r)
	}
}

func TestMydumperVersion(t *testing.T) {
	root := workspace(t, map[string]string{"flag.cnf": "[mydumper]\nroutines\n", "clean.cnf": clean})
	flag, cleanPath := filepath.Join(root, "flag.cnf"), filepath.Join(root, "clean.cnf")

	r := run(t, "", "check", cleanPath)
	if r.code != ExitOK || !strings.Contains(r.stderr, "no mydumper version configured") {
		t.Errorf("unpinned: %+v", r)
	}
	if r := run(t, "", "check", "--mydumper-version", "0.19.3-3", cleanPath); r.stderr != "" {
		t.Errorf("pinned: unexpected notice %q", r.stderr)
	}
	// v0.19.1-x has no pre-processor: a valueless flag gets the file rejected.
	r = run(t, "", "check", "--mydumper-version", "0.19.1", "--format", "concise", flag)
	if r.code != ExitFindings || !strings.Contains(r.stdout, "MDL113") {
		t.Errorf("v0.19.1: %+v", r)
	}
	if r := run(t, "", "check", "--mydumper-version", "v0.19.3-1", flag); r.code != ExitOK {
		t.Errorf("v0.19.3-1: %+v", r)
	}
	if r := run(t, "", "check", "--mydumper-version", "0.19.4-23", cleanPath); !strings.Contains(r.stderr, "nearest lower version") {
		t.Errorf("unknown build: %+v", r)
	}
	if r := run(t, "", "check", "--mydumper-version", "0.18", cleanPath); r.code != ExitError || !strings.Contains(r.stderr, "older than") {
		t.Errorf("too old: %+v", r)
	}
	pinned := workspace(t, map[string]string{".mydumper-lint.yaml": "mydumper-version: 0.19.1-2\n", "flag.cnf": "[mydumper]\nroutines\n"})
	if r := run(t, "", "check", "--format", "concise", pinned); r.code != ExitFindings || r.stderr != "" || !strings.Contains(r.stdout, "MDL113") {
		t.Errorf("configured version: %+v", r)
	}
	r = run(t, "", "check", "--mydumper-version", "0.19.1", "--fix", flag)
	if got, _ := os.ReadFile(flag); r.code != ExitOK || string(got) != "[mydumper]\nroutines=1\n" {
		t.Errorf("fix: %+v, file %q", r, got)
	}
	r = run(t, "[mydumper]\nevents\n", "inspect", "--mydumper-version", "0.19.1", "-")
	if !strings.Contains(r.stdout, "no pre-processor") || !strings.Contains(r.stdout, "GLib REJECTS") {
		t.Errorf("inspect: %s", r.stdout)
	}
}

func TestVersions(t *testing.T) {
	r := run(t, "", "versions")
	if r.code != ExitOK || !strings.Contains(r.stdout, "v0.19.1-1") || !strings.Contains(r.stdout, "default target:") ||
		!strings.Contains(r.stdout, " *") {
		t.Errorf("versions: %+v", r)
	}
	r = run(t, "", "versions", "--format", "json")
	var vs []versionJSON
	if err := json.Unmarshal([]byte(r.stdout), &vs); err != nil || len(vs) < 28 {
		t.Fatalf("versions json: %v (%d)", err, len(vs))
	}
	defaults := 0
	for _, v := range vs {
		if v.Default {
			defaults++
			if v.Prerelease {
				t.Errorf("default %s is a pre-release", v.Tag)
			}
		}
	}
	if defaults != 1 || vs[len(vs)-1].Tag != "v0.19.1-1" || vs[len(vs)-1].Preprocessor {
		t.Errorf("defaults=%d last=%+v", defaults, vs[len(vs)-1])
	}
	if r := run(t, "", "versions", "--format", "xml"); r.code != ExitError {
		t.Errorf("bad format: %+v", r)
	}
	if r := run(t, "", "versions", "--help"); r.code != ExitOK || !strings.Contains(r.stdout, "Usage") {
		t.Errorf("help: %+v", r)
	}
}

func TestFixRefusesHarmfulUnsafeFixes(t *testing.T) {
	// Renaming [MyDumper] makes mydumper read it, and its unknown key would
	// then abort startup (v0.19.3-3): the guard refuses that fix.
	root := workspace(t, map[string]string{"x.cnf": "[MyDumper]\nbogus=1\n"})
	path := filepath.Join(root, "x.cnf")
	r := run(t, "", "check", "--mydumper-version", "0.19.3-3", "--fix", "--unsafe-fixes", path)
	got, _ := os.ReadFile(path)
	if string(got) != "[MyDumper]\nbogus=1\n" || !strings.Contains(r.stderr, "not applied: MDL202") {
		t.Errorf("file %q, result %+v", got, r)
	}
}

func TestLoadSetsAndConventions(t *testing.T) {
	root := workspace(t, map[string]string{
		".mydumper-lint.yaml": "mydumper-version: 0.19.3-3\n" +
			"load-sets:\n  - defaults-file: defaults.cnf\n    extra-files: [\"*-extra.cnf\"]\n" +
			"conventions:\n  filename-pattern: '^(?P<schema>[a-z]+)-extra\\.cnf$|^defaults\\.cnf$'\n" +
			"  required:\n    mydumper: [outputdir]\n",
		"defaults.cnf":    "[client]\nhost=db\n[mydumper]\noutputdir=/b\n",
		"sales-extra.cnf": "[mydumper]\nthreads=4\n",
	})
	r := run(t, "", "check", "--format", "concise", root)
	if !strings.Contains(r.stdout, "sales-extra.cnf:1:2: warning MDL603") || !strings.Contains(r.stdout, "sales-extra.cnf:1:2: error MDL904") ||
		strings.Contains(r.stdout, "defaults.cnf:1") {
		t.Errorf("load sets and conventions: %+v", r)
	}
	// Checking the extra file alone still reads the defaults file.
	r = run(t, "", "check", "--format", "concise", filepath.Join(root, "sales-extra.cnf"))
	if !strings.Contains(r.stdout, "MDL603") {
		t.Errorf("extra file alone: %+v", r)
	}
}

func TestInspect(t *testing.T) {
	root := workspace(t, map[string]string{"bad.cnf": broken, "loc.cnf": "[mydumper]\nthreads[fr]=4\nhost=db\n"})
	r := run(t, "", "inspect", "--format", "json", filepath.Join(root, "bad.cnf"))
	var doc inspectDoc
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.Loadable || doc.Error.Line != 3 || len(doc.Rewritten) != 2 {
		t.Errorf("inspect json: %v %+v", err, doc)
	}
	r = run(t, "", "inspect", filepath.Join(root, "bad.cnf"))
	if !strings.Contains(r.stdout, "GLib REJECTS the file") || !strings.Contains(r.stdout, `"  " -> "  = 1"`) {
		t.Errorf("inspect text: %s", r.stdout)
	}
	r = run(t, "", "inspect", filepath.Join(root, "loc.cnf"))
	if !strings.Contains(r.stdout, "<- localized") || !strings.Contains(r.stdout, "<- connection-key") {
		t.Errorf("inspect reasons: %s", r.stdout)
	}
	r = run(t, "", "inspect", "--lang", "fr_FR.UTF-8", "--format", "json", filepath.Join(root, "loc.cnf"))
	if !strings.Contains(r.stdout, `"key": "threads[fr]"`) {
		t.Errorf("inspect --lang: %s", r.stdout)
	}
	if r := run(t, clean, "inspect", "-"); r.code != 0 || !strings.Contains(r.stdout, "GLib loads the file") {
		t.Errorf("inspect stdin: %+v", r)
	}
	if r := run(t, "", "inspect"); r.code != ExitError {
		t.Errorf("inspect without file: %+v", r)
	}
}

func TestInspectLoadSet(t *testing.T) {
	root := workspace(t, map[string]string{
		"defaults.cnf":  "[client]\nhost=db\n[mydumper]\nthreads=2\nkey[bar baz]=1\n",
		"extra.cnf":     "[mydumper]\nthreads=3\n[`app`.`users`]\n`email`=random_string\n",
		"good.cnf":      "[client]\nhost=db\n[`app`.`users`]\n`email`=random_string\n",
		"bad-extra.cnf": "[`app`.`users`]\n`email`=bogus\nkey]=1\n",
	})
	d, x := filepath.Join(root, "defaults.cnf"), filepath.Join(root, "extra.cnf")
	r := run(t, "", "inspect", "--mydumper-version", "v1.0.5-1", "--load-set", d, x)
	for _, want := range []string{
		"GLib REJECTS the file (line 5: Invalid key name: key[bar baz])",
		"its table sections, variable groups and per-product groups are ignored",
		`threads = "3"  (` + x + ":2)",
		"<- defaults-file-rejected",
		"mydumper: [client] and [mydumper] of " + x,
		"myloader: [client] of " + d,
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("inspect --load-set: missing %q in\n%s", want, r.stdout)
		}
	}
	r = run(t, "", "inspect", "--load-set", "--format", "json", filepath.Join(root, "good.cnf"), x)
	var doc inspectSetDoc
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || !doc.Defaults.Loadable || doc.Health != "ok" || len(doc.Connections) != 2 {
		t.Fatalf("inspect --load-set json: %v %+v", err, doc)
	}
	if c := doc.Connections[1]; c.Tool != "myloader" || c.File != filepath.Join(root, "good.cnf") || len(c.Groups) != 1 {
		t.Errorf("myloader connection: %+v", c)
	}
	r = run(t, "[mydumper]\nbogus=1\n", "inspect", "--load-set", "--mydumper-version", "v0.19.3-3", filepath.Join(root, "good.cnf"), "-")
	if !strings.Contains(r.stdout, "mydumper aborts at startup: mydumper: option parsing failed: Unknown option --bogus") ||
		!strings.Contains(r.stdout, "(<stdin>:2)") {
		t.Errorf("inspect --load-set fatal: %s", r.stdout)
	}
	r = run(t, "", "inspect", "--load-set", "--mydumper-version", "v0.19.1-3", d, filepath.Join(root, "bad-extra.cnf"))
	if !strings.Contains(r.stdout, "no pre-processor") || !strings.Contains(r.stdout, "nothing applies") {
		t.Errorf("inspect --load-set, both rejected: %s", r.stdout)
	}
	r = run(t, "", "inspect", "--load-set", "--mydumper-version", "v1.0.5-1", filepath.Join(root, "bad-extra.cnf"), filepath.Join(root, "bad-extra.cnf"))
	if !strings.Contains(r.stdout, "no defaults file") && !strings.Contains(r.stdout, "[client] of") {
		t.Errorf("inspect --load-set connections: %s", r.stdout)
	}
	for _, args := range [][]string{
		{"inspect", "--load-set", d},
		{"inspect", "--load-set", "-", "-"},
		{"inspect", "--load-set", d, filepath.Join(root, "missing.cnf")},
	} {
		if r := run(t, "", args...); r.code != ExitError {
			t.Errorf("%v: %+v", args, r)
		}
	}
	r = run(t, "", "inspect", "--load-set", "--mydumper-version", "v1.0.5-1", filepath.Join(root, "good.cnf"), filepath.Join(root, "good.cnf"))
	if !strings.Contains(r.stdout, "overridden-by-extra-file") {
		t.Errorf("inspect --load-set, same file twice: %s", r.stdout)
	}
	empty := workspace(t, map[string]string{"a.cnf": "", "b.cnf": "[`db`.`t`]\nwhere=1\n"})
	r = run(t, "", "inspect", "--load-set", filepath.Join(empty, "a.cnf"), filepath.Join(empty, "a.cnf"))
	if !strings.Contains(r.stdout, "(no groups)") || !strings.Contains(r.stdout, "mydumper: no defaults file") {
		t.Errorf("inspect --load-set, empty files: %s", r.stdout)
	}
}

func TestRulesAndExplain(t *testing.T) {
	r := run(t, "", "rules")
	if !strings.Contains(r.stdout, "MDL102") || !strings.Contains(r.stdout, "whitespace-only-line") {
		t.Errorf("rules: %s", r.stdout)
	}
	r = run(t, "", "rules", "--format", "json")
	var list []ruleJSON
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil || len(list) < 16 {
		t.Errorf("rules json: %v, %d rules", err, len(list))
	}
	if r := run(t, "", "explain", "MDL108"); !strings.Contains(r.stdout, "does not reset its state") {
		t.Errorf("explain: %s", r.stdout)
	}
	if r := run(t, "", "explain", "whitespace-only-line", "--format", "json"); !strings.Contains(r.stdout, `"id": "MDL102"`) {
		t.Errorf("explain by name: %s", r.stdout)
	}
	if r := run(t, "", "explain", "MDL1O8"); r.code != ExitError || !strings.Contains(r.stderr, "did you mean") {
		t.Errorf("explain typo: %+v", r)
	}
}

func TestCompletion(t *testing.T) {
	for shell, want := range map[string]string{
		"bash": "complete -o filenames", "zsh": "#compdef", "fish": "complete -c mydumper-lint", "powershell": "Register-ArgumentCompleter",
	} {
		fixFlag := "--fix"
		if shell == "fish" {
			fixFlag = "-l fix"
		}
		if r := run(t, "", "completion", shell); r.code != 0 || !strings.Contains(r.stdout, want) || !strings.Contains(r.stdout, fixFlag) {
			t.Errorf("completion %s: %+v", shell, r)
		}
	}
	if r := run(t, "", "completion", "tcsh"); r.code != ExitError {
		t.Errorf("unsupported shell: %+v", r)
	}
	if r := run(t, "", "completion"); r.code != ExitError {
		t.Errorf("missing shell: %+v", r)
	}
}
