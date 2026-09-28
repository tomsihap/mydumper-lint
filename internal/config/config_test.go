package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

const full = `
mydumper-version: "0.19.3-3"
mydumper-build: {client: mysql, ssl: true}
include: ["**/*.cnf"]
exclude: ["**/*.deleteme"]
fail-on: warning
preview: false
rules:
  select: ["ALL"]
  extend-select: [MDL112]
  ignore: [MDL310]
  severity:
    MDL306: off
    whitespace-only-line: warning
fix:
  extend-safe: [MDL306]
conventions:
  filename-pattern: '^(?P<schema>[a-z_]+)-extra-file\.cnf$'
  values:
    mydumper.outputdir: '/{schema}$'
  table-schema: '{schema}'
  required:
    mydumper: [outputdir, logfile]
  forbidden:
    "*": [password]
    groups: [client]
load-sets:
  - defaults-file: defaults-file.cnf
    extra-files: ["*-extra-file.cnf"]
overrides:
  - files: ["legacy/*.cnf"]
    mydumper-version: "0.19.1-1"
    conventions: off
    rules:
      ignore: [MDL1]
`

func TestParseFull(t *testing.T) {
	c, err := Parse([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	if c.MydumperVersion != "0.19.3-3" || c.FailOn != "warning" || len(c.Overrides) != 1 ||
		c.Conventions == nil || c.Conventions.Conventions.Required["mydumper"][1] != "logfile" ||
		!c.Overrides[0].Conventions.Off || c.LoadSets[0].DefaultsFile != "defaults-file.cnf" {
		t.Errorf("decoded %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"mydumper-verison: x\n":                   `unknown key "mydumper-verison" (did you mean "mydumper-version"?)`, //nolint:misspell // deliberate typo under test
		"rules:\n  selct: [ALL]\n":                `unknown key "selct" (did you mean "select"?)`,
		"fail-on: fatal\n":                        "fail-on must be error, warning, info or none",
		"rules:\n  select: [MDL999]\n":            `unknown rule "MDL999"`,
		"rules:\n  severity: {MDL102: loud}\n":    `unknown severity "loud"`,
		"mydumper-build: {client: oracle}\n":      "client must be mysql or mariadb",
		"include: ['a/[']\n":                      "invalid glob",
		"overrides:\n  - mydumper-version: x\n":   "files is required",
		"conventions: on\n":                       `conventions must be a mapping or "off"`,
		"conventions:\n  filename-pattern: '('\n": "filename-pattern",
		"load-sets:\n  - extra-files: [a]\n":      "defaults-file is required",
		"rules: [\n":                              "yaml",
	}
	for in, want := range tests {
		_, err := Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want it to contain %q", in, err, want)
		}
	}
}

func TestEmptyConfig(t *testing.T) {
	for _, in := range []string{"", "  \n", "# only a comment\n"} {
		if c, err := Parse([]byte(in)); err != nil || c == nil {
			t.Errorf("Parse(%q) = %v, %v", in, c, err)
		}
	}
}

func TestResolve(t *testing.T) {
	c, err := Parse([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	// Absolute paths of this OS: on Windows "/repo" has no volume, and
	// filepath.Rel cannot relate it to an absolute path.
	c.Dir = filepath.Join(t.TempDir(), "repo")
	s := c.Resolve(filepath.Join(c.Dir, "prod", "app-extra-file.cnf"))
	if s.MydumperVersion != "0.19.3-3" || s.FailOn != "warning" || s.Conventions == nil ||
		s.Selection.Severity["MDL306"] != diag.Off || !s.ExtendSafe["MDL306"] || !*s.Build.SSL {
		t.Errorf("settings %+v", s)
	}
	legacy := c.Resolve(filepath.Join(c.Dir, "legacy", "old.cnf"))
	if legacy.MydumperVersion != "0.19.1-1" || legacy.Conventions != nil || len(legacy.Selection.Ignore) != 1 {
		t.Errorf("legacy settings %+v", legacy)
	}
	def := (*Config)(nil).Resolve("x.cnf")
	if def.FailOn != "error" || def.Build.Client != "mysql" || def.Include[0] != "**/*.cnf" {
		t.Errorf("defaults %+v", def)
	}
}

func TestLoaderDiscovery(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "repo", "a", "b"), 0o755))
	must(os.WriteFile(filepath.Join(root, "repo", ".mydumper-lint.yaml"), []byte("fail-on: info\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "repo", "a", ".mydumper-lint.yml"), []byte("fail-on: warning\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, ".mydumper-lint.yaml"), []byte("fail-on: none\n"), 0o644))

	l := NewLoader()
	c, err := l.For(filepath.Join(root, "repo", "a", "b", "x.cnf"))
	if err != nil || c == nil || c.FailOn != "warning" {
		t.Fatalf("nearest config: %+v, %v", c, err)
	}
	c, err = l.For(filepath.Join(root, "repo", "y.cnf"))
	if err != nil || c == nil || c.FailOn != "info" || !strings.HasSuffix(c.Dir, "repo") {
		t.Fatalf("repo config: %+v, %v", c, err)
	}
	// The search stops at the repository root: the file above it is not used.
	must(os.Remove(filepath.Join(root, "repo", ".mydumper-lint.yaml")))
	l = NewLoader()
	if c, err := l.For(filepath.Join(root, "repo", "y.cnf")); err != nil || c != nil {
		t.Fatalf("search must stop at .git: %+v, %v", c, err)
	}
	// A broken configuration is an error.
	must(os.WriteFile(filepath.Join(root, "repo", ".mydumper-lint.yaml"), []byte("nope: 1\n"), 0o644))
	if _, err := NewLoader().For(filepath.Join(root, "repo", "y.cnf")); err == nil {
		t.Fatal("broken config must fail")
	}
}

func TestSchema(t *testing.T) {
	b, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	props := s["properties"].(map[string]any)
	for _, key := range []string{"mydumper-version", "rules", "overrides", "conventions", "load-sets"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema lacks %q", key)
		}
	}
	if ap, ok := s["additionalProperties"].(bool); !ok || ap {
		t.Error("the schema must reject unknown keys")
	}
	sev := props["rules"].(map[string]any)["properties"].(map[string]any)["severity"].(map[string]any)
	if sev["additionalProperties"].(map[string]any)["enum"] == nil {
		t.Error("severity values must be enumerated")
	}
}
