package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/glob"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

// DefaultInclude is the default include glob.
var DefaultInclude = []string{"**/*.cnf"}

// Settings are the effective settings for one file.
type Settings struct {
	Config          *Config // nil when no configuration file applies
	MydumperVersion string  // empty: latest stable
	Build           Build   // resolved: Client and SSL always set
	FailOn          string  // error, warning, info or none
	Preview         bool
	Selection       rules.Selection
	ExtendSafe      map[string]bool // rule IDs
	Conventions     *Conventions    // nil when off or absent
	Include         []string
	Exclude         []string
}

// Resolve returns the settings for file (nil receiver: defaults only).
func (c *Config) Resolve(file string) Settings {
	ssl := true
	s := Settings{Config: c, Build: Build{Client: "mysql", SSL: &ssl}, FailOn: "error", Include: DefaultInclude}
	if c == nil {
		return s
	}
	s.MydumperVersion, s.Preview = c.MydumperVersion, c.Preview
	s.Build = mergeBuild(s.Build, c.MydumperBuild)
	if c.FailOn != "" {
		s.FailOn = c.FailOn
	}
	if len(c.Include) > 0 {
		s.Include = c.Include
	}
	s.Exclude = c.Exclude
	sel := c.Rules
	if c.Conventions != nil && !c.Conventions.Off {
		s.Conventions = c.Conventions.Conventions
	}
	rel := c.relative(file)
	for _, o := range c.Overrides {
		if !glob.MatchAny(o.Files, rel) {
			continue
		}
		if o.MydumperVersion != "" {
			s.MydumperVersion = o.MydumperVersion
		}
		s.Build = mergeBuild(s.Build, o.MydumperBuild)
		if o.FailOn != "" {
			s.FailOn = o.FailOn
		}
		if o.Preview != nil {
			s.Preview = *o.Preview
		}
		if o.Rules != nil {
			sel = mergeRules(sel, *o.Rules)
		}
		if o.Conventions != nil {
			s.Conventions = nil
			if !o.Conventions.Off {
				s.Conventions = o.Conventions.Conventions
			}
		}
	}
	s.Selection = selection(sel, s.Preview)
	s.ExtendSafe = map[string]bool{}
	for _, tok := range c.Fix.ExtendSafe {
		if r, ok := rules.Lookup(tok); ok {
			s.ExtendSafe[r.ID] = true
		}
	}
	return s
}

func (c *Config) relative(file string) string {
	if c.Dir == "" {
		return filepath.ToSlash(file)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return filepath.ToSlash(file)
	}
	rel, err := filepath.Rel(c.Dir, abs)
	if err != nil {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(rel)
}

func mergeBuild(base Build, o *Build) Build {
	if o == nil {
		return base
	}
	if o.Client != "" {
		base.Client = o.Client
	}
	if o.SSL != nil {
		v := *o.SSL
		base.SSL = &v
	}
	return base
}

func mergeRules(base, o Rules) Rules {
	if o.Select != nil {
		base.Select = o.Select
	}
	if o.ExtendSelect != nil {
		base.ExtendSelect = o.ExtendSelect
	}
	if o.Ignore != nil {
		base.Ignore = o.Ignore
	}
	if len(o.Severity) > 0 {
		m := map[string]string{}
		for k, v := range base.Severity {
			m[k] = v
		}
		for k, v := range o.Severity {
			m[k] = v
		}
		base.Severity = m
	}
	return base
}

func selection(r Rules, preview bool) rules.Selection {
	sev := map[string]diag.Severity{}
	for k, v := range r.Severity {
		if s, err := diag.ParseSeverity(v); err == nil {
			sev[k] = s
		}
	}
	return rules.Selection{Select: r.Select, ExtendSelect: r.ExtendSelect, Ignore: r.Ignore, Severity: sev, Preview: preview}
}

// Loader discovers configuration files and caches them per directory.
type Loader struct {
	mu    sync.Mutex
	cache map[string]cached
	home  string
}

type cached struct {
	cfg *Config
	err error
}

// NewLoader returns a Loader that stops its search at the home directory.
func NewLoader() *Loader {
	home, _ := os.UserHomeDir()
	return &Loader{cache: map[string]cached{}, home: home}
}

// For returns the configuration that applies to file: the nearest
// configuration file in the file's directory or its parents, searching up to
// and including the repository root (a directory containing .git) or the home
// directory. It returns nil when there is none.
func (l *Loader) For(file string) (*Config, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	return l.dir(filepath.Dir(abs))
}

func (l *Loader) dir(dir string) (*Config, error) {
	l.mu.Lock()
	if c, ok := l.cache[dir]; ok {
		l.mu.Unlock()
		return c.cfg, c.err
	}
	l.mu.Unlock()
	var res cached
	found := false
	for _, name := range FileNames {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			cfg, err := Load(p)
			if err == nil {
				cfg.Dir = dir
			}
			res, found = cached{cfg, err}, true
			break
		}
	}
	if !found && !l.stop(dir) {
		parent := filepath.Dir(dir)
		if parent != dir {
			res.cfg, res.err = l.dir(parent)
		}
	}
	l.mu.Lock()
	l.cache[dir] = res
	l.mu.Unlock()
	return res.cfg, res.err
}

func (l *Loader) stop(dir string) bool {
	if dir == l.home {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// suggestKey proposes the closest known key of a configuration type.
func suggestKey(field, typeName string) string {
	types := map[string]reflect.Type{
		"Config": reflect.TypeFor[Config](), "Build": reflect.TypeFor[Build](),
		"Rules": reflect.TypeFor[Rules](), "Fix": reflect.TypeFor[Fix](),
		"Conventions": reflect.TypeFor[Conventions](), "LoadSet": reflect.TypeFor[LoadSet](),
		"Override": reflect.TypeFor[Override](),
	}
	t, ok := types[typeName]
	if !ok {
		return ""
	}
	best, bestD := "", 4
	for i := 0; i < t.NumField(); i++ {
		key := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		if d := rules.Levenshtein(field, key); d < bestD {
			best, bestD = key, d
		}
	}
	return best
}
