package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/tomsihap/mydumper-lint/internal/buildinfo"
	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/glob"
	"github.com/tomsihap/mydumper-lint/internal/lint"
	"github.com/tomsihap/mydumper-lint/internal/report"
	"github.com/tomsihap/mydumper-lint/internal/rules"
	"github.com/tomsihap/mydumper-lint/internal/textdiff"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/target"
)

type checkCmd struct {
	e *env

	fix, unsafeFixes, diff, statistics, quiet, noConfig, preview bool

	formats                                         repeatFlag
	selectRules, extendSelect, ignore, excludeGlobs listFlag
	version, failOn, configPath, stdinName, color   string
	baseDir                                         string

	fixedConfig *config.Config
	loader      *config.Loader
	targets     *targetCache
}

func (c *checkCmd) flags() *flag.FlagSet {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.BoolVar(&c.fix, "fix", false, "Apply safe fixes.")
	fs.BoolVar(&c.unsafeFixes, "unsafe-fixes", false, "Also apply unsafe fixes (with --fix or --diff).")
	fs.BoolVar(&c.diff, "diff", false, "Print the fixes as a unified diff instead of writing them; exit 1 if there are any.")
	fs.Var(&c.formats, "format", "Output `format`: text (default), concise, json, sarif, github, junit, gitlab. Repeatable; NAME=PATH writes to a file.")
	fs.BoolVar(&c.statistics, "statistics", false, "Print counts per rule instead of the diagnostics.")
	fs.StringVar(&c.version, "mydumper-version", "", "Target mydumper `version` (e.g. 0.19.3-3, 0.19, latest). Default: configuration, else the latest stable release.")
	fs.Var(&c.selectRules, "select", "Rules to enable: IDs, names, prefixes (MDL1) or ALL. Replaces the configured selection.")
	fs.Var(&c.extendSelect, "extend-select", "Rules to enable in addition to the selection (e.g. opt-in rules).")
	fs.Var(&c.ignore, "ignore", "Rules to disable.")
	fs.BoolVar(&c.preview, "preview", false, "Enable preview rules.")
	fs.StringVar(&c.failOn, "fail-on", "", "Lowest `severity` that makes the command fail: error (default), warning, info or none.")
	fs.StringVar(&c.configPath, "config", "", "Use this configuration `file` instead of discovering .mydumper-lint.yaml.")
	fs.BoolVar(&c.noConfig, "no-config", false, "Ignore configuration files.")
	fs.Var(&c.excludeGlobs, "exclude", "Additional `glob`s of files to skip when walking directories.")
	fs.StringVar(&c.stdinName, "stdin-filename", "", "`Name` of the file read from stdin (-), for messages and configuration.")
	fs.StringVar(&c.baseDir, "base-dir", "", "Base `directory` for file references in the configuration (default: working directory).")
	fs.StringVar(&c.color, "color", "auto", "Colors: auto, always or never. Honors NO_COLOR and FORCE_COLOR.")
	fs.BoolVar(&c.quiet, "quiet", false, "Print diagnostics only, without the summary.")
	return fs
}

const checkHelp = `Usage: mydumper-lint check [flags] [path ...]

Lint mydumper/myloader configuration files. A path is a file, a directory
(searched recursively for files matching the configured include globs,
**/*.cnf by default) or - for stdin. Flags may come before or after paths.

Exit codes: 0 no finding at or above --fail-on; 1 findings, or --diff with
changes; 2 usage, configuration or I/O error, or a fixer self-check failure.

Flags:
`

func runCheck(args []string, e *env) int {
	c := &checkCmd{e: e, targets: newTargetCache()}
	fs := c.flags()
	paths, err := parse(fs, args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(e.stdout, checkHelp+flagUsage(fs))
		return ExitOK
	}
	if err != nil {
		return usageError(e, "check", err)
	}
	if err := c.validate(); err != nil {
		return usageError(e, "check", err)
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if c.configPath != "" {
		cfg, err := config.Load(c.configPath)
		if err != nil {
			fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
			return ExitError
		}
		abs, _ := filepath.Abs(c.configPath)
		cfg.Dir = filepath.Dir(abs)
		c.fixedConfig = cfg
	} else if !c.noConfig {
		c.loader = config.NewLoader()
	}
	files, err := c.collect(paths)
	if err != nil {
		fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	outcomes := c.run(files)
	return c.finish(outcomes)
}

func (c *checkCmd) validate() error {
	if c.failOn != "" && c.failOn != "none" {
		if s, err := diag.ParseSeverity(c.failOn); err != nil || s == diag.Off {
			return fmt.Errorf("--fail-on must be error, warning, info or none, not %q", c.failOn)
		}
	}
	switch c.color {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("--color must be auto, always or never, not %q", c.color)
	}
	if c.unsafeFixes && !c.fix && !c.diff {
		return errors.New("--unsafe-fixes needs --fix or --diff")
	}
	if c.fix && c.diff {
		return errors.New("--fix and --diff cannot be used together")
	}
	for _, f := range c.formats.values {
		name, _, _ := strings.Cut(f, "=")
		if _, err := report.Get(name); err != nil {
			return err
		}
	}
	// Reject unknown rules up front, even when no file ends up being linted.
	sel := rules.Selection{Select: c.selectRules.values, ExtendSelect: c.extendSelect.values, Ignore: c.ignore.values, Preview: true}
	if len(sel.Select) == 0 {
		sel.Select = []string{"ALL"}
	}
	if _, err := sel.Resolve(); err != nil {
		return err
	}
	if c.version != "" {
		if _, err := target.Resolve(c.version, optionsdb.DefaultBuild); err != nil {
			return fmt.Errorf("--mydumper-version: %w", err)
		}
	}
	return nil
}

// settings resolves the configuration for a file and applies the flags.
func (c *checkCmd) settings(path string) (config.Settings, error) {
	cfg := c.fixedConfig
	if c.loader != nil {
		var err error
		if cfg, err = c.loader.For(path); err != nil {
			return config.Settings{}, err
		}
	}
	s := cfg.Resolve(path)
	if c.version != "" {
		s.MydumperVersion = c.version
	}
	if c.failOn != "" {
		s.FailOn = c.failOn
	}
	if c.preview {
		s.Preview, s.Selection.Preview = true, true
	}
	if c.selectRules.set {
		s.Selection.Select = c.selectRules.values
	}
	s.Selection.ExtendSelect = append(append([]string{}, s.Selection.ExtendSelect...), c.extendSelect.values...)
	s.Selection.Ignore = append(append([]string{}, s.Selection.Ignore...), c.ignore.values...)
	s.Exclude = append(append([]string{}, s.Exclude...), c.excludeGlobs.values...)
	return s, nil
}

// collect expands the paths into the list of files to lint, in a stable
// order. Files given explicitly are always linted; files found by walking a
// directory must match the include globs and no exclude glob.
func (c *checkCmd) collect(paths []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		if p == "-" {
			add(p)
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			add(p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != p && (d.Name() == ".git" || d.Name() == ".hg" || d.Name() == ".svn") {
					return filepath.SkipDir
				}
				if path != p {
					if st, err := c.settings(filepath.Join(path, "x")); err == nil &&
						glob.MatchAny(st.Exclude, relTo(st, path)) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			st, err := c.settings(path)
			if err != nil {
				return err
			}
			rel := relTo(st, path)
			if glob.MatchAny(st.Include, rel) && !glob.MatchAny(st.Exclude, rel) {
				add(path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// relTo returns path relative to the configuration directory (or the
// working directory), slash-separated, for glob matching.
func relTo(s config.Settings, path string) string {
	base := "."
	if s.Config != nil && s.Config.Dir != "" {
		base = s.Config.Dir
	}
	absBase, err1 := filepath.Abs(base)
	abs, err2 := filepath.Abs(path)
	if err1 == nil && err2 == nil {
		if rel, err := filepath.Rel(absBase, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// outcome is the result of processing one file.
type outcome struct {
	isStdin  bool
	path     string // as shown to the user
	src      []byte
	settings config.Settings
	result   *lint.Result // of the fixed content when fixing
	output   []byte       // fixed content (--fix, --diff)
	fixed    int
	version  string
	warning  string // e.g. version resolution notice
	err      error
}

func (c *checkCmd) run(files []string) []outcome {
	outcomes := make([]outcome, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, min(runtime.GOMAXPROCS(0), len(files))) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				outcomes[i] = c.process(files[i])
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return outcomes
}

func (c *checkCmd) process(path string) outcome {
	o := outcome{path: path, isStdin: path == "-"}
	var err error
	cfgPath := path
	if o.isStdin {
		o.path = "<stdin>"
		if c.stdinName != "" {
			o.path, cfgPath = c.stdinName, c.stdinName
		}
		o.src, err = io.ReadAll(c.e.stdin)
	} else {
		o.src, err = os.ReadFile(path)
	}
	if err != nil {
		o.err = err
		return o
	}
	if o.settings, err = c.settings(cfgPath); err != nil {
		o.err = err
		return o
	}
	l, version, warning, err := c.targets.linter(o.settings)
	if err != nil {
		o.err = err
		return o
	}
	o.version, o.warning = version, warning
	if !c.fix && !c.diff {
		o.result = l.Check(o.path, o.src)
		return o
	}
	res, err := l.Fix(o.path, o.src, fix.Options{Unsafe: c.unsafeFixes, ExtendSafe: o.settings.ExtendSafe})
	if err != nil {
		o.err = err
		return o
	}
	o.output, o.fixed = res.Output, res.Applied
	o.result = l.Check(o.path, res.Output)
	if c.fix && !o.isStdin && !bytes.Equal(res.Output, o.src) {
		if err := fix.WriteAtomic(path, res.Output); err != nil {
			o.err = fmt.Errorf("writing fixes: %w", err)
		}
	}
	return o
}

func (c *checkCmd) finish(outcomes []outcome) int {
	code := ExitOK
	diagOut := c.e.stdout
	if c.fix && hasStdin(outcomes) {
		diagOut = c.e.stderr // stdout carries the fixed content
	}
	warned := map[string]bool{}
	var results []report.FileResult
	var diffs bytes.Buffer
	for _, o := range outcomes {
		if o.warning != "" && !warned[o.warning] {
			warned[o.warning] = true
			fmt.Fprintf(c.e.stderr, "mydumper-lint: %s\n", o.warning)
		}
		if o.err != nil {
			fmt.Fprintf(c.e.stderr, "mydumper-lint: %s: %v\n", o.path, o.err)
			code = ExitError
			continue
		}
		src := o.src
		if o.output != nil {
			src = o.output
		}
		if c.diff && !bytes.Equal(o.output, o.src) {
			diffs.Write(textdiff.Diff(o.path, o.src, o.path, o.output))
		}
		if c.fix && o.isStdin {
			if _, err := c.e.stdout.Write(o.output); err != nil {
				code = ExitError
			}
		}
		results = append(results, report.FileResult{
			Path: o.path, Source: src, Loadable: o.result.KF.Loadable, Version: o.version,
			Diagnostics: o.result.Diagnostics, Fixed: o.fixed,
		})
		if code != ExitError && failing(o.result.Diagnostics, o.settings.FailOn) {
			code = ExitFindings
		}
	}
	if c.diff {
		if _, err := c.e.stdout.Write(diffs.Bytes()); err != nil {
			return ExitError
		}
		if diffs.Len() > 0 && code == ExitOK {
			code = ExitFindings
		}
		return code
	}
	if c.statistics {
		printStatistics(diagOut, results)
		return code
	}
	if err := c.write(diagOut, results); err != nil {
		fmt.Fprintf(c.e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	return code
}

func hasStdin(outcomes []outcome) bool {
	for _, o := range outcomes {
		if o.isStdin {
			return true
		}
	}
	return false
}

func failing(ds []diag.Diagnostic, failOn string) bool {
	if failOn == "none" {
		return false
	}
	threshold, err := diag.ParseSeverity(failOn)
	if err != nil {
		threshold = diag.Error
	}
	for _, d := range ds {
		if d.Severity.AtLeast(threshold) {
			return true
		}
	}
	return false
}

// write renders the results in every requested format.
func (c *checkCmd) write(stdout io.Writer, results []report.FileResult) error {
	specs := c.formats.values
	if len(specs) == 0 {
		specs = []string{"text"}
	}
	sum := report.Summarize(results)
	for _, spec := range specs {
		name, path, toFile := strings.Cut(spec, "=")
		f, err := report.Get(name)
		if err != nil {
			return err
		}
		w, closeFn := stdout, func() error { return nil }
		if toFile {
			file, err := os.Create(path)
			if err != nil {
				return err
			}
			w, closeFn = file, file.Close
		}
		opt := report.Options{
			Color:       !toFile && useColor(c.color, w, c.e.getenv),
			Quiet:       c.quiet,
			ToolVersion: buildinfo.Version,
			Rules:       ruleMetas(),
		}
		if err := f.Write(w, results, sum, opt); err != nil {
			return errors.Join(err, closeFn())
		}
		if err := closeFn(); err != nil {
			return err
		}
	}
	return nil
}

func useColor(mode string, w io.Writer, getenv func(string) string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" {
		return false
	}
	if getenv("FORCE_COLOR") != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// ruleMetas lists every rule for the formats that embed rule metadata.
func ruleMetas() []report.RuleMeta {
	var out []report.RuleMeta
	for _, r := range rules.All() {
		fix := ""
		if r.Fix != 0 {
			fix = r.Fix.String()
		}
		out = append(out, report.RuleMeta{
			ID: r.ID, Name: r.Name, Summary: r.Summary, Severity: r.Severity, Fix: fix,
			DocsURL: "https://github.com/tomsihap/mydumper-lint/blob/main/docs/rules/" + r.ID + ".md",
		})
	}
	return out
}

func printStatistics(w io.Writer, results []report.FileResult) {
	type stat struct {
		id, name   string
		n, fixable int
	}
	m := map[string]*stat{}
	for _, r := range results {
		for _, d := range r.Diagnostics {
			s, ok := m[d.RuleID]
			if !ok {
				s = &stat{id: d.RuleID, name: d.RuleName}
				m[d.RuleID] = s
			}
			s.n++
			if d.Fix != nil && d.Fix.Applicability == diag.Safe {
				s.fixable++
			}
		}
	}
	var stats []*stat
	for _, s := range m {
		stats = append(stats, s)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].n != stats[j].n {
			return stats[i].n > stats[j].n
		}
		return stats[i].id < stats[j].id
	})
	for _, s := range stats {
		mark := ""
		if s.fixable > 0 {
			mark = " [*]"
		}
		fmt.Fprintf(w, "%5d  %s  %s%s\n", s.n, s.id, s.name, mark)
	}
	if len(stats) > 0 {
		fmt.Fprintln(w, "[*] fixable with --fix")
	}
}
