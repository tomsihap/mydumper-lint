//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/txtar"
)

// A scenario is one e2e/scenarios/*.txtar file. Its "scenario" section uses
// the small line-based format below (README.md documents it for authors);
// every other section is a file written into the directory the containers
// see as /work.
//
//	# comment
//	tool: mydumper                      required: mydumper or myloader
//	versions: >= v0.19.3-1              optional: conditions every version must meet
//	args: --defaults-file /work/x.cnf   repeatable: arguments, split on spaces
//	env: LANG=C.UTF-8                   repeatable: container environment
//	lint: MDL102, MDL405                required: expected rule IDs (may be empty)
//	lint-files: x.cnf                   optional: files to lint (default *.cnf)
//	expect <observation> [<arg>]: <v>   repeatable: runtime expectations
//	when < v1.0.1-1                     block: lint and expect lines that only
//	expect exit: nonzero                apply to versions meeting the conditions
//	end
const scenarioSection = "scenario"

// Tools the harness can run.
var knownTools = []string{"mydumper", "myloader"}

// restoreDBPlaceholder is replaced, in args and observation arguments, by a
// database name unique to the run; the harness drops it afterwards.
const restoreDBPlaceholder = "${RESTORE_DB}"

// lintConfigFile is a scenario file passed to the linter with --config
// (for load sets, design §9.3); without it the linter runs with --no-config.
const lintConfigFile = ".mydumper-lint.yaml"

var ruleIDRE = regexp.MustCompile(`^MDL[0-9]{3}$`)

// Scenario is a parsed scenario file.
type Scenario struct {
	Name      string   // file name without .txtar
	Doc       string   // txtar comment: what the scenario proves
	Tool      string   // mydumper or myloader
	Versions  []cond   // every one must hold
	Args      []string // container arguments, placeholders not yet replaced
	Env       []string // NAME=value
	Lint      []string // top-level lint expectation, sorted
	LintFiles []string // files to lint
	Expect    []expectation
	Blocks    []block
	Files     []scenarioFile // every section but the scenario, decoded
}

type scenarioFile struct {
	Name string // without .esc
	Data []byte
}

// block is a `when … end` block.
type block struct {
	Line   int
	Conds  []cond
	Lint   []string // nil when the block does not set lint
	Expect []expectation
}

// expectation is an `expect` line.
type expectation struct {
	Line  int
	Name  string // observation name
	Arg   string // observation argument; empty when it takes none
	Value string
}

func (e expectation) key() string {
	if e.Arg == "" {
		return e.Name
	}
	return e.Name + " " + e.Arg
}

func (e expectation) String() string { return e.key() + ": " + e.Value }

// cond is one condition of a `versions:` or `when` line: a comparison with a
// version tag, or a fact of the knowledge base about the version.
type cond struct {
	Text string // as written, for messages
	Op   string // <, <=, >, >=, =, != ; empty for a knowledge-base fact
	Tag  optionsdb.Tag
	Fact string // kb fact: ignore-unknown-options, masquerade=<function>
	Neg  bool   // kb fact negated with "!"
}

// holds evaluates the condition for a version and its knowledge-base view.
func (c cond) holds(tag optionsdb.Tag, view *optionsdb.View) bool {
	if c.Op != "" {
		n := tag.Compare(c.Tag)
		switch c.Op {
		case "<":
			return n < 0
		case "<=":
			return n <= 0
		case ">":
			return n > 0
		case ">=":
			return n >= 0
		case "=":
			return n == 0
		default: // "!="
			return n != 0
		}
	}
	var v bool
	if name, ok := strings.CutPrefix(c.Fact, "masquerade="); ok {
		v = slices.Contains(view.MasqueradeFunctions(), name)
	} else {
		v = view.IgnoreUnknownOptions()
	}
	return v != c.Neg
}

func allHold(cs []cond, tag optionsdb.Tag, view *optionsdb.View) bool {
	for _, c := range cs {
		if !c.holds(tag, view) {
			return false
		}
	}
	return true
}

// parseConds parses "COND[, COND…]".
func parseConds(s string) ([]cond, error) {
	var out []cond
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty condition in %q", s)
		}
		if fact, ok := strings.CutPrefix(part, "kb:"); ok {
			c := cond{Text: part}
			if f, ok := strings.CutPrefix(fact, "!"); ok {
				c.Neg, fact = true, f
			}
			name, isMasq := strings.CutPrefix(fact, "masquerade=")
			switch {
			case fact == "ignore-unknown-options":
			case isMasq && regexp.MustCompile(`^[a-z_]+$`).MatchString(name):
			default:
				return nil, fmt.Errorf("unknown knowledge-base fact %q (want ignore-unknown-options or masquerade=<function>)", fact)
			}
			c.Fact = fact
			out = append(out, c)
			continue
		}
		op, rest, ok := strings.Cut(part, " ")
		if !ok || !slices.Contains([]string{"<", "<=", ">", ">=", "=", "!="}, op) {
			return nil, fmt.Errorf("invalid condition %q (want OP vMAJOR.MINOR.PATCH-REV with OP one of < <= > >= = !=, or kb:FACT)", part)
		}
		tag, err := optionsdb.ParseTag(strings.TrimSpace(rest))
		if err != nil {
			return nil, err
		}
		out = append(out, cond{Text: part, Op: op, Tag: tag})
	}
	return out, nil
}

// parseLint parses "MDL102, MDL405" (possibly empty) into a sorted set.
func parseLint(s string) ([]string, error) {
	out := []string{}
	for part := range strings.SplitSeq(s, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			if strings.TrimSpace(s) == "" {
				break
			}
			return nil, fmt.Errorf("empty rule ID in %q", s)
		}
		if !ruleIDRE.MatchString(id) {
			return nil, fmt.Errorf("invalid rule ID %q (want MDLnnn)", id)
		}
		if slices.Contains(out, id) {
			return nil, fmt.Errorf("rule ID %s listed twice", id)
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// parseExpect parses "<observation> [<arg>]: <value>" and validates it.
func parseExpect(s string, line int, tool string) (expectation, error) {
	head, value, ok := strings.Cut(s, ":")
	if !ok {
		return expectation{}, fmt.Errorf("expect %q: missing ':'", s)
	}
	e := expectation{Line: line, Value: strings.TrimSpace(value)}
	fields := strings.Fields(head)
	switch len(fields) {
	case 1:
		e.Name = fields[0]
	case 2:
		e.Name, e.Arg = fields[0], fields[1]
	default:
		return expectation{}, fmt.Errorf("expect %q: want <observation> [<argument>]: <value>", s)
	}
	o, ok := observations[e.Name]
	if !ok {
		return expectation{}, fmt.Errorf("unknown observation %q (known: %s)", e.Name, strings.Join(observationNames(), ", "))
	}
	if !slices.Contains(o.Tools, tool) {
		return expectation{}, fmt.Errorf("observation %q is not available for %s", e.Name, tool)
	}
	if err := o.validate(e.Arg, e.Value); err != nil {
		return expectation{}, fmt.Errorf("expect %s: %w", e.key(), err)
	}
	return e, nil
}

// LoadScenario reads and validates a scenario file.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), ".txtar")
	s, err := ParseScenario(name, data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// ParseScenario parses a scenario archive. It is strict: unknown directives,
// observations or values, duplicates and missing sections are errors.
func ParseScenario(name string, data []byte) (*Scenario, error) {
	a := txtar.Parse(data)
	s := &Scenario{Name: name, Doc: strings.TrimSpace(string(a.Comment))}
	if s.Doc == "" {
		return nil, errors.New("missing comment: say what the scenario proves")
	}
	var spec []byte
	seen := map[string]bool{}
	for _, f := range a.Files {
		base := strings.TrimSuffix(f.Name, ".esc")
		if seen[base] {
			return nil, fmt.Errorf("section %q appears twice", base)
		}
		seen[base] = true
		if base == scenarioSection {
			if f.Name != scenarioSection {
				return nil, errors.New("the scenario section cannot be escaped")
			}
			spec = f.Data
			continue
		}
		if base == "" || strings.ContainsAny(base, `/\`) || strings.HasPrefix(base, ".") && base != lintConfigFile {
			return nil, fmt.Errorf("invalid file name %q (plain names only)", f.Name)
		}
		content, _, err := a.Get(base)
		if err != nil {
			return nil, err
		}
		s.Files = append(s.Files, scenarioFile{Name: base, Data: content})
	}
	if spec == nil {
		return nil, errors.New("missing section -- scenario --")
	}
	if err := s.parseSpec(string(spec)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Scenario) parseSpec(spec string) error {
	var cur *block
	seen := map[string]int{}
	lintSet := false
	for i, raw := range strings.Split(spec, "\n") {
		line := i + 1
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fail := func(format string, args ...any) error {
			return fmt.Errorf("scenario line %d: %s", line, fmt.Sprintf(format, args...))
		}
		if rest, ok := strings.CutPrefix(text, "when "); ok {
			if cur != nil {
				return fail("`when` inside a block: close the block at line %d with `end` first", cur.Line)
			}
			cs, err := parseConds(rest)
			if err != nil {
				return fail("%v", err)
			}
			cur = &block{Line: line, Conds: cs}
			continue
		}
		if text == "end" {
			if cur == nil {
				return fail("`end` without `when`")
			}
			if cur.Lint == nil && len(cur.Expect) == 0 {
				return fail("empty block (line %d)", cur.Line)
			}
			s.Blocks = append(s.Blocks, *cur)
			cur = nil
			continue
		}
		if rest, ok := strings.CutPrefix(text, "expect "); ok {
			if s.Tool == "" {
				return fail("`tool:` must come first")
			}
			e, err := parseExpect(rest, line, s.Tool)
			if err != nil {
				return fail("%v", err)
			}
			list := &s.Expect
			if cur != nil {
				list = &cur.Expect
			}
			if !observations[e.Name].Multi {
				for _, prev := range *list {
					if prev.key() == e.key() {
						return fail("%s already expected at line %d", e.key(), prev.Line)
					}
				}
			}
			*list = append(*list, e)
			continue
		}
		key, value, ok := strings.Cut(text, ":")
		if !ok {
			return fail("want `key: value`, `expect …`, `when …` or `end`, got %q", text)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "lint" {
			ids, err := parseLint(value)
			if err != nil {
				return fail("%v", err)
			}
			if cur != nil {
				if cur.Lint != nil {
					return fail("lint set twice in the block")
				}
				cur.Lint = ids
				continue
			}
			if lintSet {
				return fail("lint set twice")
			}
			s.Lint, lintSet = ids, true
			continue
		}
		if cur != nil {
			return fail("only `lint:` and `expect` lines are allowed in a `when` block, got %q", key)
		}
		repeatable := key == "args" || key == "env"
		if prev, dup := seen[key]; dup && !repeatable {
			return fail("%s already set at line %d", key, prev)
		}
		seen[key] = line
		switch key {
		case "tool":
			if !slices.Contains(knownTools, value) {
				return fail("unknown tool %q (want mydumper or myloader)", value)
			}
			s.Tool = value
		case "versions":
			cs, err := parseConds(value)
			if err != nil {
				return fail("%v", err)
			}
			s.Versions = cs
		case "args":
			if value == "" {
				return fail("empty args")
			}
			s.Args = append(s.Args, strings.Fields(value)...)
		case "env":
			name, _, ok := strings.Cut(value, "=")
			if !ok || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
				return fail("env %q: want NAME=value", value)
			}
			s.Env = append(s.Env, value)
		case "lint-files":
			s.LintFiles = strings.Fields(value)
			if len(s.LintFiles) == 0 {
				return fail("empty lint-files")
			}
		default:
			return fail("unknown directive %q (want tool, versions, args, env, lint, lint-files, expect, when or end)", key)
		}
	}
	if cur != nil {
		return fmt.Errorf("scenario line %d: `when` block not closed with `end`", cur.Line)
	}
	if s.Tool == "" {
		return errors.New("scenario: missing `tool:`")
	}
	if !lintSet {
		return errors.New("scenario: missing `lint:` (use an empty `lint:` when no diagnostic is expected)")
	}
	if len(s.Expect) == 0 && len(s.Blocks) == 0 {
		return errors.New("scenario: no `expect` line: a scenario must observe something")
	}
	if len(s.Args) == 0 {
		return errors.New("scenario: missing `args:`")
	}
	return s.checkFiles()
}

// checkFiles validates lint-files and the files the arguments reference.
func (s *Scenario) checkFiles() error {
	has := func(name string) bool {
		return slices.ContainsFunc(s.Files, func(f scenarioFile) bool { return f.Name == name })
	}
	if s.LintFiles == nil {
		for _, f := range s.Files {
			if strings.HasSuffix(f.Name, ".cnf") {
				s.LintFiles = append(s.LintFiles, f.Name)
			}
		}
	}
	if len(s.LintFiles) == 0 {
		return errors.New("scenario: no .cnf file to lint")
	}
	for _, name := range s.LintFiles {
		if !has(name) {
			return fmt.Errorf("scenario: lint-files names %q, which is not a section", name)
		}
	}
	for _, a := range s.Args {
		if p, ok := strings.CutPrefix(a, "/work/"); ok && !strings.Contains(p, "/") && strings.HasSuffix(p, ".cnf") && !has(p) {
			return fmt.Errorf("scenario: args reference /work/%s, which is not a section", p)
		}
	}
	return nil
}

// plan is what a scenario expects from one version.
type plan struct {
	Lint   []string
	Expect []expectation
}

// planFor merges the top-level expectations with the blocks that apply to a
// version. A block overrides a top-level value; two applying blocks that set
// the same single-valued observation, or lint, must agree.
func (s *Scenario) planFor(tag optionsdb.Tag, view *optionsdb.View) (plan, error) {
	p := plan{Lint: s.Lint}
	lintFrom := 0 // line of the block that set lint
	byKey := map[string]int{}
	setBy := map[string]int{}
	for _, e := range s.Expect {
		if !observations[e.Name].Multi {
			byKey[e.key()] = len(p.Expect)
		}
		p.Expect = append(p.Expect, e)
	}
	for _, b := range s.Blocks {
		if !allHold(b.Conds, tag, view) {
			continue
		}
		if b.Lint != nil {
			if lintFrom != 0 && !slices.Equal(p.Lint, b.Lint) {
				return plan{}, fmt.Errorf("blocks at lines %d and %d both apply to %s and expect different lint results", lintFrom, b.Line, tag)
			}
			p.Lint, lintFrom = b.Lint, b.Line
		}
		for _, e := range b.Expect {
			if observations[e.Name].Multi {
				p.Expect = append(p.Expect, e)
				continue
			}
			i, ok := byKey[e.key()]
			if !ok {
				byKey[e.key()] = len(p.Expect)
				setBy[e.key()] = b.Line
				p.Expect = append(p.Expect, e)
				continue
			}
			if from, ok := setBy[e.key()]; ok && p.Expect[i].Value != e.Value {
				return plan{}, fmt.Errorf("blocks at lines %d and %d both apply to %s and expect different %s",
					from, b.Line, tag, e.key())
			}
			setBy[e.key()] = b.Line
			p.Expect[i] = e
		}
	}
	return p, nil
}

// appliesTo reports whether the scenario runs on a version.
func (s *Scenario) appliesTo(tag optionsdb.Tag, view *optionsdb.View) bool {
	return allHold(s.Versions, tag, view)
}

// LoadScenarios reads every scenario of dir, sorted by name.
func LoadScenarios(dir string) ([]*Scenario, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.txtar"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no scenario in %s", dir)
	}
	sort.Strings(paths)
	var out []*Scenario
	var errs []error
	for _, p := range paths {
		s, err := LoadScenario(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

// atoiStrict parses a non-negative decimal integer without sign or spaces.
func atoiStrict(s string) (int, error) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, fmt.Errorf("%q is not a non-negative integer", s)
	}
	return strconv.Atoi(s)
}
