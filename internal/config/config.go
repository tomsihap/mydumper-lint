// Package config reads .mydumper-lint.yaml (design §9): strict decoding,
// discovery from the linted file up to the repository root, per-file
// resolution of overrides, and the JSON Schema of the format.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/glob"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

// FileNames are the configuration file names, in lookup order.
var FileNames = []string{".mydumper-lint.yaml", ".mydumper-lint.yml"}

// Config is the content of one configuration file.
type Config struct {
	MydumperVersion string     `yaml:"mydumper-version" desc:"Target mydumper version: v0.19.3-3, 0.19.3, 0.19, latest or latest-prerelease."`
	MydumperBuild   *Build     `yaml:"mydumper-build" desc:"How mydumper was built; the default is the official image (MySQL client, SSL)."`
	Include         []string   `yaml:"include" desc:"Globs of files to lint when walking directories (default **/*.cnf)."`
	Exclude         []string   `yaml:"exclude" desc:"Globs of files and directories to skip."`
	FailOn          string     `yaml:"fail-on" enum:"error,warning,info,none" desc:"Lowest severity that makes the command fail (default error)."`
	Preview         bool       `yaml:"preview" desc:"Enable preview rules."`
	Rules           Rules      `yaml:"rules" desc:"Rule selection."`
	Fix             Fix        `yaml:"fix" desc:"Fixer settings."`
	Conventions     *Toggle    `yaml:"conventions" desc:"Project conventions (MDL901-905), or off."`
	LoadSets        []LoadSet  `yaml:"load-sets" desc:"Files mydumper loads together, for cross-file rules."`
	Overrides       []Override `yaml:"overrides" desc:"Settings for files matching globs; later entries win."`
	Path            string     `yaml:"-"` // file the configuration was read from
	Dir             string     `yaml:"-"` // directory globs are relative to
}

// Build describes how mydumper was compiled (design §5.5).
type Build struct {
	Client string `yaml:"client" enum:"mysql,mariadb" desc:"Client library mydumper is built against."`
	SSL    *bool  `yaml:"ssl" desc:"Whether mydumper is built with SSL support."`
}

// Rules selects rules (design §9.2).
type Rules struct {
	Select       []string          `yaml:"select" desc:"Rule IDs, names, prefixes (MDL1) or ALL."`
	ExtendSelect []string          `yaml:"extend-select" desc:"Rules added to the selection, e.g. opt-in rules."`
	Ignore       []string          `yaml:"ignore" desc:"Rules removed from the selection."`
	Severity     map[string]string `yaml:"severity" enum:"off,info,warning,error" desc:"Severity per rule ID or name."`
}

// Fix configures the fixer.
type Fix struct {
	ExtendSafe []string `yaml:"extend-safe" desc:"Rules whose unsafe fixes are applied with --fix."`
}

// Conventions are the project conventions of MDL901-905 (design §6.3).
type Conventions struct {
	FilenamePattern string              `yaml:"filename-pattern" desc:"Go regular expression for file names, with named groups."`
	Values          map[string]string   `yaml:"values" desc:"group.key: value template ({name} from the file name groups)."`
	TableSchema     string              `yaml:"table-schema" desc:"Template for the database of table sections."`
	Required        map[string][]string `yaml:"required" desc:"Keys required per group."`
	Forbidden       map[string][]string `yaml:"forbidden" desc:"Keys forbidden per group (* for all); the groups key lists forbidden groups."`
}

// Toggle is either "off" or a Conventions object.
type Toggle struct {
	Off         bool
	Conventions *Conventions
}

// UnmarshalYAML accepts "off" or a mapping.
func (t *Toggle) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Value != "off" {
			return fmt.Errorf("line %d: conventions must be a mapping or \"off\"", n.Line)
		}
		t.Off = true
		return nil
	}
	var c Conventions
	if err := decodeStrict(n, &c); err != nil {
		return err
	}
	t.Conventions = &c
	return nil
}

// LoadSet declares files loaded by one mydumper invocation (design §9.3).
type LoadSet struct {
	DefaultsFile string   `yaml:"defaults-file" desc:"The --defaults-file."`
	ExtraFiles   []string `yaml:"extra-files" desc:"Globs of --defaults-extra-file files used with it."`
}

// Override applies settings to files matching globs.
type Override struct {
	Files           []string `yaml:"files" desc:"Globs the override applies to."`
	MydumperVersion string   `yaml:"mydumper-version" desc:"Target mydumper version for these files."`
	MydumperBuild   *Build   `yaml:"mydumper-build" desc:"mydumper build for these files."`
	FailOn          string   `yaml:"fail-on" enum:"error,warning,info,none" desc:"Failure threshold for these files."`
	Preview         *bool    `yaml:"preview" desc:"Preview rules for these files."`
	Rules           *Rules   `yaml:"rules" desc:"Rule selection for these files (replaces the lists it sets)."`
	Conventions     *Toggle  `yaml:"conventions" desc:"Conventions for these files, or off."`
}

// Load reads and validates a configuration file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

// Parse decodes and validates a configuration.
func Parse(b []byte) (*Config, error) {
	c := &Config{}
	if len(bytes.TrimSpace(b)) == 0 {
		return c, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return c, nil
	}
	if err := decodeStrict(root.Content[0], c); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

var unknownField = regexp.MustCompile(`field (\S+) not found in type config\.(\w+)`)

// decodeStrict decodes n into v, rejecting unknown keys with a suggestion.
func decodeStrict(n *yaml.Node, v any) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(n); err != nil {
		return err
	}
	dec := yaml.NewDecoder(&buf)
	dec.KnownFields(true)
	err := dec.Decode(v)
	var te *yaml.TypeError
	if errors.As(err, &te) {
		msgs := make([]string, len(te.Errors))
		for i, m := range te.Errors {
			msgs[i] = m
			if sub := unknownField.FindStringSubmatch(m); sub != nil {
				msgs[i] = fmt.Sprintf("unknown key %q", sub[1])
				if s := suggestKey(sub[1], sub[2]); s != "" {
					msgs[i] += fmt.Sprintf(" (did you mean %q?)", s)
				}
				if line := strings.Split(m, ":")[0]; strings.HasPrefix(line, "line ") {
					msgs[i] = line + ": " + msgs[i]
				}
			}
		}
		return errors.New(strings.Join(msgs, "; "))
	}
	return err
}

func (c *Config) validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	checkFailOn := func(where, v string) {
		if v != "" && v != "none" {
			if _, err := diag.ParseSeverity(v); err != nil || v == "off" {
				add("%s: fail-on must be error, warning, info or none, not %q", where, v)
			}
		}
	}
	checkBuild := func(where string, b *Build) {
		if b != nil && b.Client != "" && b.Client != "mysql" && b.Client != "mariadb" {
			add("%s: mydumper-build.client must be mysql or mariadb, not %q", where, b.Client)
		}
	}
	checkGlobs := func(where string, globs []string) {
		for _, g := range globs {
			if err := glob.Validate(g); err != nil {
				add("%s: invalid glob %q: %v", where, g, err)
			}
		}
	}
	checkRules := func(where string, r *Rules) {
		if r == nil {
			return
		}
		sev := map[string]diag.Severity{}
		for k, v := range r.Severity {
			s, err := diag.ParseSeverity(v)
			if err != nil {
				add("%s: rules.severity.%s: %v", where, k, err)
			}
			sev[k] = s
		}
		sel := rules.Selection{Select: r.Select, ExtendSelect: r.ExtendSelect, Ignore: r.Ignore, Severity: sev, Preview: true}
		if _, err := sel.Resolve(); err != nil {
			add("%s: %v", where, err)
		}
	}
	checkConventions := func(where string, t *Toggle) {
		if t == nil || t.Conventions == nil || t.Conventions.FilenamePattern == "" {
			return
		}
		if _, err := regexp.Compile(t.Conventions.FilenamePattern); err != nil {
			add("%s: conventions.filename-pattern: %v", where, err)
		}
	}
	checkFailOn("config", c.FailOn)
	checkBuild("config", c.MydumperBuild)
	checkGlobs("include", c.Include)
	checkGlobs("exclude", c.Exclude)
	checkRules("config", &c.Rules)
	checkConventions("config", c.Conventions)
	for i, ls := range c.LoadSets {
		if ls.DefaultsFile == "" {
			add("load-sets[%d]: defaults-file is required", i)
		}
		checkGlobs(fmt.Sprintf("load-sets[%d].extra-files", i), ls.ExtraFiles)
	}
	for i, o := range c.Overrides {
		where := fmt.Sprintf("overrides[%d]", i)
		if len(o.Files) == 0 {
			add("%s: files is required", where)
		}
		checkGlobs(where+".files", o.Files)
		checkFailOn(where, o.FailOn)
		checkBuild(where, o.MydumperBuild)
		checkRules(where, o.Rules)
		checkConventions(where, o.Conventions)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Rules converts the conventions for the rules (MDL901-MDL905).
func (c *Conventions) Rules() *rules.Conventions {
	if c == nil {
		return nil
	}
	return &rules.Conventions{
		FilenamePattern: c.FilenamePattern, Values: c.Values, TableSchema: c.TableSchema,
		Required: c.Required, Forbidden: c.Forbidden,
	}
}
