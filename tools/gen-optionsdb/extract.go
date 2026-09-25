package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// This file turns the source tree of one tag into facts: the options each
// binary registers (per build configuration), table keys, masking functions,
// product names, the unknown-option policy and the loader fingerprint.

var tools = []string{"mydumper", "myloader"}

// optDef is what the knowledge base records about one option in one version
// and one build configuration.
type optDef struct {
	Short string
	Arg   string
	Flags []string // sorted
	Group string   // GOptionGroup name; "" for the main group
}

func (d optDef) key() string {
	return d.Short + "\x00" + d.Arg + "\x00" + strings.Join(d.Flags, ",") + "\x00" + d.Group
}

// optVariant is an option definition with the build condition under which it
// exists.
type optVariant struct {
	def    optDef
	cond   string // "" when present in every configuration
	source string // path:line of the entry, in the first configuration that has it

	// set by the overlay
	isRegex          bool
	values           []string
	valuesIgnoreCase bool
}

// factName is a table key or masking function name; prefix means that any
// string starting with name matches.
type factName struct {
	name   string
	prefix bool
}

// extraction holds everything learned from one tag.
type extraction struct {
	tag           string
	options       map[string]map[string][]optVariant // tool → option → variants
	tableKeys     []factName
	masquerade    []factName
	products      []string
	ignoreUnknown bool
	fingerprint   string            // loader fingerprint (both functions)
	funcPrints    map[string]string // fingerprint of each loader function, for the report
	notes         []string          // findings worth printing in the report
}

// srcTree is the content of one source archive, keyed by slash path relative
// to the repository root ("src/common.c").
type srcTree map[string][]byte

// extract analyses one tag.
func extract(tag string, tree srcTree) (*extraction, error) {
	x := &extraction{tag: tag, options: map[string]map[string][]optVariant{}}

	var units []*unit
	var paths []string
	for p := range tree {
		if strings.HasPrefix(p, "src/") && (strings.HasSuffix(p, ".c") || strings.HasSuffix(p, ".h")) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		u, err := newUnit(p, tree[p])
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	byPath := map[string]*unit{}
	for _, u := range units {
		byPath[u.path] = u
	}

	sources, notes, err := binarySources(tree, paths)
	if err != nil {
		return nil, err
	}
	x.notes = append(x.notes, notes...)

	configs := allConfigs()
	perCfg := make([]*cfgFacts, len(configs))
	var official map[string]*cfgUnit
	noteSet := map[string]bool{}
	for ci, cfg := range configs {
		cus := map[string]*cfgUnit{}
		for _, u := range units {
			cu := filterUnit(u, cfg)
			if err := cu.parse(); err != nil {
				return nil, fmt.Errorf("%s (%s): %w", tag, cfg, err)
			}
			cus[u.path] = cu
		}
		f, err := analyseConfig(cfg, cus, sources)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", tag, cfg, err)
		}
		perCfg[ci] = f
		for _, n := range f.notes {
			noteSet[n] = true
		}
		if ci == officialConfigIndex() {
			official = cus
		}
	}
	x.notes = append(x.notes, sortedKeys(noteSet)...)

	// Facts that are not options must not depend on the build.
	base := perCfg[0]
	for ci, f := range perCfg[1:] {
		if !slices.Equal(f.tableKeys, base.tableKeys) || !slices.Equal(f.masquerade, base.masquerade) ||
			!slices.Equal(f.products, base.products) || f.ignoreUnknown != base.ignoreUnknown {
			return nil, fmt.Errorf("%s: table keys, masking functions, products or the unknown-option policy depend on the build (%s vs %s); the schema cannot represent that",
				tag, configs[0], configs[ci+1])
		}
	}
	x.tableKeys, x.masquerade, x.products, x.ignoreUnknown = base.tableKeys, base.masquerade, base.products, base.ignoreUnknown

	// Options: group the configurations by definition.
	for _, tool := range tools {
		names := map[string]bool{}
		for _, f := range perCfg {
			for n := range f.options[tool] {
				names[n] = true
			}
		}
		x.options[tool] = map[string][]optVariant{}
		for _, n := range sortedKeys(names) {
			type group struct {
				def    optDef
				bits   []int
				source string
			}
			var groups []*group
			for ci, f := range perCfg {
				o, ok := f.options[tool][n]
				if !ok {
					continue
				}
				var g *group
				for _, gg := range groups {
					if gg.def.key() == o.def.key() {
						g = gg
					}
				}
				if g == nil {
					g = &group{def: o.def, source: o.source}
					groups = append(groups, g)
				}
				g.bits = append(g.bits, ci)
			}
			for _, g := range groups {
				x.options[tool][n] = append(x.options[tool][n], optVariant{
					def: g.def, cond: minimalCondition(g.bits, len(knownMacros)), source: g.source,
				})
			}
			slices.SortFunc(x.options[tool][n], func(a, b optVariant) int { return strings.Compare(a.cond, b.cond) })
		}
	}

	// Loader fingerprint, over the raw tokens (directives included) of the
	// official build's definitions.
	fp, prints, err := loaderFingerprint(official)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tag, err)
	}
	x.fingerprint, x.funcPrints = fp, prints
	return x, nil
}

// officialConfigIndex is the configuration of the official images: MySQL
// client (no LIBMARIADB), WITH_SSL, and HAVE_MY_BOOL undefined (the upstream
// build never defines it).
func officialConfigIndex() int {
	for i, c := range allConfigs() {
		if c["WITH_SSL"] && !c["LIBMARIADB"] && !c["HAVE_MY_BOOL"] {
			return i
		}
	}
	panic("no official configuration")
}

// cfgFacts is what one configuration yields.
type cfgFacts struct {
	options       map[string]map[string]regOption // tool → option
	tableKeys     []factName
	masquerade    []factName
	products      []string
	ignoreUnknown bool
	notes         []string
}

type regOption struct {
	def    optDef
	source string
	array  string
}

// binarySources returns the translation units of each binary, read from
// CMakeLists.txt, and notes where they differ from the path convention
// (src/<tool>/ plus the other src/ files).
func binarySources(tree srcTree, paths []string) (map[string][]string, []string, error) {
	cm, ok := tree["CMakeLists.txt"]
	if !ok {
		return nil, nil, fmt.Errorf("CMakeLists.txt not found")
	}
	targets, err := cmakeExecutables(string(cm))
	if err != nil {
		return nil, nil, fmt.Errorf("CMakeLists.txt: %w", err)
	}
	out := map[string][]string{}
	var notes []string
	for _, tool := range tools {
		srcs, ok := targets[tool]
		if !ok {
			return nil, nil, fmt.Errorf("CMakeLists.txt: no add_executable(%s …)", tool)
		}
		seen := map[string]bool{}
		for _, s := range srcs {
			if !strings.HasSuffix(s, ".c") || seen[s] {
				continue
			}
			if !slices.Contains(paths, s) {
				return nil, nil, fmt.Errorf("CMakeLists.txt: %s lists %s, which is not in the archive", tool, s)
			}
			seen[s] = true
			out[tool] = append(out[tool], s)
		}
		sort.Strings(out[tool])
		// compare with the path convention
		for _, p := range paths {
			if !strings.HasSuffix(p, ".c") {
				continue
			}
			byPath := pathTool(p) == tool || pathTool(p) == "both"
			if byPath != seen[p] {
				notes = append(notes, fmt.Sprintf("%s: %s compiled into %s: %v by CMakeLists.txt, %v by the path convention", tool, p, tool, seen[p], byPath))
			}
		}
	}
	return out, notes, nil
}

// pathTool applies the path convention: src/mydumper/ ⇒ mydumper,
// src/myloader/ ⇒ myloader, any other src/ file ⇒ both.
func pathTool(p string) string {
	switch {
	case strings.HasPrefix(p, "src/mydumper/"):
		return "mydumper"
	case strings.HasPrefix(p, "src/myloader/"):
		return "myloader"
	}
	return "both"
}

// program is one binary under one configuration.
type program struct {
	name    string
	units   []*cfgUnit // translation units
	headers []*cfgUnit
	globals map[string][]*funcDef              // non-static functions of the translation units and headers
	statics map[*cfgUnit]map[string][]*funcDef // static functions of each unit
	arrays  map[string][]*arrayDef
}

func newProgram(name string, cus map[string]*cfgUnit, srcs []string) *program {
	p := &program{name: name, globals: map[string][]*funcDef{}, statics: map[*cfgUnit]map[string][]*funcDef{}, arrays: map[string][]*arrayDef{}}
	for _, s := range srcs {
		p.units = append(p.units, cus[s])
	}
	for _, path := range sortedKeys(cus) {
		if strings.HasSuffix(path, ".h") {
			p.headers = append(p.headers, cus[path])
		}
	}
	for _, cu := range append(slices.Clone(p.units), p.headers...) {
		p.statics[cu] = map[string][]*funcDef{}
		for _, f := range cu.funcs {
			if !f.static || strings.HasSuffix(cu.u.path, ".h") {
				p.globals[f.name] = append(p.globals[f.name], f)
			} else {
				p.statics[cu][f.name] = append(p.statics[cu][f.name], f)
			}
		}
		for _, a := range cu.arrays {
			if a.fn == nil && (!a.static || strings.HasSuffix(cu.u.path, ".h")) {
				p.arrays[a.name] = append(p.arrays[a.name], a)
			}
		}
	}
	return p
}

// resolveFunc finds the functions a name refers to from inside unit cu.
func (p *program) resolveFunc(cu *cfgUnit, name string) []*funcDef {
	if local := p.statics[cu][name]; len(local) > 0 {
		return local
	}
	return p.globals[name]
}

// resolveArray finds the array a name refers to from inside function fn.
func (p *program) resolveArray(fn *funcDef, name string) (*arrayDef, error) {
	var found []*arrayDef
	for _, a := range fn.cu.arrays {
		if a.name == name && a.fn == fn {
			found = append(found, a)
		}
	}
	if len(found) == 0 {
		for _, a := range fn.cu.arrays {
			if a.name == name && a.fn == nil && a.static {
				found = append(found, a)
			}
		}
	}
	if len(found) == 0 {
		found = p.arrays[name]
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("%s registers %s, but no GOptionEntry array of that name is defined", fn, name)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("%s registers %s, which is defined %d times", fn, name, len(found))
}

// reachable returns the functions reachable from main, by any reference
// (calls, function pointers, thread entry points).
func (p *program) reachable() (map[*funcDef]bool, error) {
	var mains []*funcDef
	for _, cu := range p.units {
		for _, f := range cu.funcs {
			if f.name == "main" {
				mains = append(mains, f)
			}
		}
	}
	if len(mains) != 1 {
		return nil, fmt.Errorf("%s: found %d definitions of main", p.name, len(mains))
	}
	seen := map[*funcDef]bool{mains[0]: true}
	queue := []*funcDef{mains[0]}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		body := f.body()
		for i, t := range body {
			if t.kind != tkIdent || (i > 0 && (body[i-1].is(".") || body[i-1].is("->"))) {
				continue
			}
			for _, g := range p.resolveFunc(f.cu, t.text) {
				if !seen[g] {
					seen[g] = true
					queue = append(queue, g)
				}
			}
		}
	}
	return seen, nil
}

// call is one call expression found in a function body.
type call struct {
	fn    *funcDef
	name  string
	at    int // index in fn.cu.toks of the callee name
	args  [][]token
	close int // index of ')'
}

// calls returns the calls to name in f's body.
func calls(f *funcDef, name string) ([]call, error) {
	ts := f.cu.toks
	var out []call
	for i := f.open + 1; i < f.close; i++ {
		if !ts[i].isIdent(name) || !ts[i+1].is("(") || ts[i-1].is(".") || ts[i-1].is("->") {
			continue
		}
		j, ok := matchClose(ts, i+1)
		if !ok {
			return nil, f.cu.errorf(i, "unbalanced call to %s", name)
		}
		c := call{fn: f, name: name, at: i, close: j}
		for _, a := range splitTop(ts, i+2, j) {
			c.args = append(c.args, ts[a[0]:a[1]])
		}
		out = append(out, c)
	}
	return out, nil
}

// checkCertain fails when a call depends on a condition the generator cannot
// evaluate.
func checkCertain(c call) error {
	if t, cd, bad := c.fn.cu.uncertain(c.at, c.close); bad {
		return fmt.Errorf("%s:%d: %s depends on a preprocessor condition the generator cannot evaluate: %s",
			c.fn.cu.u.path, t.line, c.name, cd.describe())
	}
	return nil
}

// groupInfo describes the option group an array is added to.
type groupInfo struct {
	name  string
	main  bool
	added bool // added to a context (or made its main group)
}

// groupOf resolves the GOptionGroup held by variable v in function f.
func (p *program) groupOf(f *funcDef, v string, depth int) (groupInfo, error) {
	if depth > 4 {
		return groupInfo{}, fmt.Errorf("%s: group %s: resolution too deep", f, v)
	}
	ts := f.cu.toks
	var assigns []int // index of the first token after '='
	for i := f.open + 1; i < f.close-1; i++ {
		if ts[i].isIdent(v) && ts[i+1].is("=") && !ts[i-1].is(".") && !ts[i-1].is("->") {
			assigns = append(assigns, i+2)
		}
	}
	if len(assigns) != 1 {
		if slices.Contains(f.params, v) {
			return groupInfo{}, fmt.Errorf("%s: group %s is a parameter; the generator does not follow groups across calls that way", f, v)
		}
		return groupInfo{}, fmt.Errorf("%s: group variable %s is assigned %d times", f, v, len(assigns))
	}
	var g groupInfo
	rhs := assigns[0]
	switch {
	case ts[rhs].isIdent("g_option_group_new") && ts[rhs+1].is("("):
		cs, err := calls(f, "g_option_group_new")
		if err != nil {
			return g, err
		}
		for _, c := range cs {
			if c.at != rhs {
				continue
			}
			if err := checkCertain(c); err != nil {
				return g, err
			}
			if len(c.args) < 1 || len(c.args[0]) != 1 || c.args[0][0].kind != tkString {
				return g, fmt.Errorf("%s: g_option_group_new with a non-literal name", f)
			}
			name, err := unquote(c.args[0][0].text)
			if err != nil {
				return g, err
			}
			g.name = name
		}
	case ts[rhs].kind == tkIdent && ts[rhs+1].is("("):
		callees := p.resolveFunc(f.cu, ts[rhs].text)
		if len(callees) != 1 {
			return g, fmt.Errorf("%s: group %s comes from %s, which resolves to %d functions", f, v, ts[rhs].text, len(callees))
		}
		h := callees[0]
		var ret []string
		for i := h.open + 1; i < h.close-2; i++ {
			if h.cu.toks[i].isIdent("return") && h.cu.toks[i+1].kind == tkIdent && h.cu.toks[i+2].is(";") {
				ret = append(ret, h.cu.toks[i+1].text)
			}
		}
		if len(ret) != 1 {
			return g, fmt.Errorf("%s: group %s comes from %s, which does not return exactly one group variable", f, v, h)
		}
		inner, err := p.groupOf(h, ret[0], depth+1)
		if err != nil {
			return g, err
		}
		g = inner
	default:
		return g, fmt.Errorf("%s: cannot tell which group %s holds", f, v)
	}
	for _, fn := range []string{"g_option_context_add_group", "g_option_context_set_main_group"} {
		cs, err := calls(f, fn)
		if err != nil {
			return g, err
		}
		for _, c := range cs {
			if len(c.args) == 2 && len(c.args[1]) == 1 && c.args[1][0].isIdent(v) {
				if err := checkCertain(c); err != nil {
					return g, err
				}
				g.added = true
				if fn == "g_option_context_set_main_group" {
					g.main = true
				}
			}
		}
	}
	return g, nil
}

// analyseConfig extracts the facts of one build configuration.
func analyseConfig(cfg config, cus map[string]*cfgUnit, sources map[string][]string) (*cfgFacts, error) {
	facts := &cfgFacts{options: map[string]map[string]regOption{}}
	registered := map[*arrayDef]bool{}
	macros, err := stringMacros(cus, cfg)
	if err != nil {
		return nil, err
	}
	ignore := map[string]bool{}
	for _, tool := range tools {
		p := newProgram(tool, cus, sources[tool])
		reach, err := p.reachable()
		if err != nil {
			return nil, err
		}
		opts := map[string]regOption{}
		facts.options[tool] = opts
		type reg struct {
			array *arrayDef
			group groupInfo
		}
		var regs []reg
		seenReg := map[string]bool{}
		for _, f := range sortedFuncs(reach) {
			for _, api := range []string{"g_option_group_add_entries", "g_option_context_add_main_entries"} {
				cs, err := calls(f, api)
				if err != nil {
					return nil, err
				}
				for _, c := range cs {
					if err := checkCertain(c); err != nil {
						return nil, err
					}
					if len(c.args) < 2 || len(c.args[1]) != 1 || c.args[1][0].kind != tkIdent {
						return nil, fmt.Errorf("%s: unexpected arguments to %s", f, api)
					}
					a, err := p.resolveArray(f, c.args[1][0].text)
					if err != nil {
						return nil, err
					}
					var g groupInfo
					if api == "g_option_context_add_main_entries" {
						g = groupInfo{name: "", main: true, added: true}
					} else {
						if len(c.args[0]) != 1 || c.args[0][0].kind != tkIdent {
							return nil, fmt.Errorf("%s: %s with a group that is not a variable", f, api)
						}
						g, err = p.groupOf(f, c.args[0][0].text, 0)
						if err != nil {
							return nil, err
						}
					}
					if !g.added {
						facts.notes = append(facts.notes, fmt.Sprintf("%s: %s adds %s to group %q, which never reaches an option context: ignored", tool, f, a, g.name))
						continue
					}
					k := a.String() + "\x00" + g.name
					if seenReg[k] {
						continue
					}
					seenReg[k] = true
					regs = append(regs, reg{a, g})
				}
			}
		}
		for _, r := range regs {
			registered[r.array] = true
			entries, err := r.array.entries(macros)
			if err != nil {
				return nil, err
			}
			group := r.group.name
			if r.group.main {
				group = ""
			}
			for _, e := range entries {
				def := optDef{Short: e.short, Arg: e.arg, Flags: e.flags, Group: group}
				src := fmt.Sprintf("%s:%d", r.array.cu.u.path, e.line)
				if prev, dup := opts[e.long]; dup {
					return nil, fmt.Errorf("%s: option --%s is registered twice (%s and %s)", tool, e.long, prev.source, src)
				}
				opts[e.long] = regOption{def: def, source: src, array: r.array.name}
			}
		}

		// unknown-option policy of the config-file parser
		for _, f := range sortedFuncs(reach) {
			cs, err := calls(f, "g_option_context_set_ignore_unknown_options")
			if err != nil {
				return nil, err
			}
			for _, c := range cs {
				if err := checkCertain(c); err != nil {
					return nil, err
				}
				if f.name != "parse_key_file_group" {
					return nil, fmt.Errorf("%s: g_option_context_set_ignore_unknown_options is called in %s, not in parse_key_file_group: review how the config-file parser treats unknown options", tool, f)
				}
				if len(c.args) != 2 || len(c.args[1]) != 1 || !(c.args[1][0].isIdent("TRUE") || c.args[1][0].text == "1") {
					return nil, fmt.Errorf("%s: g_option_context_set_ignore_unknown_options with an unexpected value in %s", tool, f)
				}
				ignore[tool] = true
			}
		}
	}
	for _, path := range sortedKeys(cus) {
		for _, a := range cus[path].arrays {
			if !registered[a] {
				facts.notes = append(facts.notes, fmt.Sprintf("GOptionEntry array %s is never registered by mydumper or myloader: ignored", a))
			}
		}
	}
	if ignore["mydumper"] != ignore["myloader"] {
		return nil, fmt.Errorf("mydumper and myloader disagree on unknown options in config files")
	}
	facts.ignoreUnknown = ignore["mydumper"]

	if facts.tableKeys, err = tableKeys(cus, macros); err != nil {
		return nil, err
	}
	if facts.masquerade, err = masqueradeFunctions(cus, macros); err != nil {
		return nil, err
	}
	if facts.products, err = productNames(cus, macros); err != nil {
		return nil, err
	}
	return facts, nil
}

func sortedFuncs(set map[*funcDef]bool) []*funcDef {
	out := make([]*funcDef, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b *funcDef) int {
		if c := strings.Compare(a.cu.u.path, b.cu.u.path); c != 0 {
			return c
		}
		return a.line - b.line
	})
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// macroTable maps string macros to their value under one configuration. A
// name with an undecidable or conflicting definition maps to an error.
type macroTable map[string]macroValue

type macroValue struct {
	value string
	err   error
}

func stringMacros(cus map[string]*cfgUnit, cfg config) (macroTable, error) {
	vals := map[string][]define{}
	unsure := map[string]define{}
	for _, path := range sortedKeys(cus) {
		for _, d := range cus[path].u.strDefs {
			switch d.cond.truth(cfg) {
			case triTrue:
				vals[d.name] = append(vals[d.name], d)
			case triUnknown:
				unsure[d.name] = d
			}
		}
	}
	t := macroTable{}
	for name, ds := range vals {
		v := macroValue{value: ds[0].value}
		for _, d := range ds[1:] {
			if d.value != v.value {
				v.err = fmt.Errorf("macro %s has conflicting definitions (%s:%d and %s:%d)", name, ds[0].path, ds[0].line, d.path, d.line)
			}
		}
		t[name] = v
	}
	for name, d := range unsure {
		t[name] = macroValue{err: fmt.Errorf("macro %s is defined under a condition the generator cannot evaluate (%s:%d)", name, d.path, d.line)}
	}
	return t, nil
}

// stringValue evaluates a token sequence that must be a string: literals
// (concatenated) or one string macro.
func stringValue(toks []token, macros macroTable) (string, bool, error) {
	if len(toks) == 1 && toks[0].kind == tkIdent {
		m, ok := macros[toks[0].text]
		if !ok {
			return "", false, nil
		}
		if m.err != nil {
			return "", false, m.err
		}
		return m.value, true, nil
	}
	var sb strings.Builder
	for _, t := range toks {
		if t.kind != tkString {
			return "", false, nil
		}
		s, err := unquote(t.text)
		if err != nil {
			return "", false, err
		}
		sb.WriteString(s)
	}
	return sb.String(), len(toks) > 0, nil
}

// entry is one parsed GOptionEntry.
type entry struct {
	long  string
	short string
	flags []string
	arg   string
	line  int
}

var glibFlags = map[string]string{
	"G_OPTION_FLAG_NONE":         "",
	"G_OPTION_FLAG_HIDDEN":       "hidden",
	"G_OPTION_FLAG_IN_MAIN":      "in_main",
	"G_OPTION_FLAG_REVERSE":      "reverse",
	"G_OPTION_FLAG_NO_ARG":       "no_arg",
	"G_OPTION_FLAG_FILENAME":     "filename",
	"G_OPTION_FLAG_OPTIONAL_ARG": "optional_arg",
	"G_OPTION_FLAG_NOALIAS":      "noalias",
	"G_OPTION_FLAG_DEPRECATED":   "deprecated",
}

var glibArgs = map[string]string{
	"G_OPTION_ARG_NONE":           "none",
	"G_OPTION_ARG_STRING":         "string",
	"G_OPTION_ARG_INT":            "int",
	"G_OPTION_ARG_CALLBACK":       "callback",
	"G_OPTION_ARG_FILENAME":       "filename",
	"G_OPTION_ARG_STRING_ARRAY":   "string_array",
	"G_OPTION_ARG_FILENAME_ARRAY": "filename_array",
	"G_OPTION_ARG_DOUBLE":         "double",
	"G_OPTION_ARG_INT64":          "int64",
}

var entryFields = []string{"long_name", "short_name", "flags", "arg", "arg_data", "description", "arg_description"}

// entries parses the initializer of a GOptionEntry array up to its
// terminator, like g_option_group_add_entries does.
func (a *arrayDef) entries(macros macroTable) ([]entry, error) {
	cu := a.cu
	ts := cu.toks
	if t, cd, bad := cu.uncertain(a.open, a.close); bad {
		return nil, fmt.Errorf("%s:%d: array %s depends on a preprocessor condition the generator cannot evaluate: %s", cu.u.path, t.line, a.name, cd.describe())
	}
	errf := func(i int, format string, args ...any) error {
		return fmt.Errorf("%s: array %s: %s", cu.errorf(i, "").Error(), a.name, fmt.Sprintf(format, args...))
	}
	var out []entry
	for _, el := range splitTop(ts, a.open+1, a.close) {
		s, e := el[0], el[1]
		if s == e {
			continue // trailing comma
		}
		if e == s+1 && ts[s].isIdent("G_OPTION_ENTRY_NULL") {
			return out, nil
		}
		if !ts[s].is("{") {
			return nil, errf(s, "unexpected element %q", ts[s].text)
		}
		if k, ok := matchClose(ts, s); !ok || k != e-1 {
			return nil, errf(s, "malformed entry")
		}
		fields := map[string][]token{}
		for i, f := range splitTop(ts, s+1, e-1) {
			toks := ts[f[0]:f[1]]
			if len(toks) >= 3 && toks[0].is(".") && toks[1].kind == tkIdent && toks[2].is("=") {
				fields[toks[1].text] = toks[3:]
				continue
			}
			if i >= len(entryFields) {
				return nil, errf(f[0], "too many fields")
			}
			fields[entryFields[i]] = toks
		}
		longToks := fields["long_name"]
		if len(longToks) == 0 || (len(longToks) == 1 && (longToks[0].isIdent("NULL") || longToks[0].text == "0")) {
			return out, nil // terminator: GLib stops here
		}
		long, ok, err := stringValue(longToks, macros)
		if err != nil {
			return nil, errf(s, "long name: %v", err)
		}
		if !ok {
			if len(longToks) == 1 && longToks[0].isIdent("G_OPTION_REMAINING") {
				continue // collects remaining arguments; not a named option
			}
			return nil, errf(s, "long name is not a string")
		}
		if long == "" {
			continue // G_OPTION_REMAINING
		}
		en := entry{long: long, line: longToks[0].line}
		switch st := fields["short_name"]; {
		case len(st) == 0 || (len(st) == 1 && st[0].text == "0"):
		case len(st) == 1 && st[0].kind == tkChar:
			c, err := unquote(st[0].text)
			if err != nil || len(c) != 1 {
				return nil, errf(s, "bad short name %s", st[0].text)
			}
			if c != "\x00" {
				en.short = c
			}
		default:
			return nil, errf(s, "unsupported short name")
		}
		flags := map[string]bool{}
		for _, t := range fields["flags"] {
			switch {
			case t.is("|") || t.is("(") || t.is(")"):
			case t.kind == tkNumber && t.text == "0":
			case t.kind == tkIdent:
				f, ok := glibFlags[t.text]
				if !ok {
					return nil, errf(s, "unknown flag %s", t.text)
				}
				if f != "" {
					flags[f] = true
				}
			default:
				return nil, errf(s, "unsupported flags expression")
			}
		}
		en.flags = sortedKeys(flags)
		switch at := fields["arg"]; {
		case len(at) == 0 || (len(at) == 1 && at[0].text == "0"):
			en.arg = "none"
		case len(at) == 1 && at[0].kind == tkIdent:
			v, ok := glibArgs[at[0].text]
			if !ok {
				return nil, errf(s, "unknown argument type %s", at[0].text)
			}
			en.arg = v
		default:
			return nil, errf(s, "unsupported argument type")
		}
		out = append(out, en)
	}
	return nil, fmt.Errorf("%s:%d: array %s has no terminating entry", cu.u.path, a.line, a.name)
}

// findFuncs returns every definition of a function name in the .c files.
func findFuncs(cus map[string]*cfgUnit, name string) []*funcDef {
	var out []*funcDef
	for _, path := range sortedKeys(cus) {
		if !strings.HasSuffix(path, ".c") {
			continue
		}
		for _, f := range cus[path].funcs {
			if f.name == name {
				out = append(out, f)
			}
		}
	}
	return out
}

func findOneFunc(cus map[string]*cfgUnit, name string) (*funcDef, error) {
	fs := findFuncs(cus, name)
	if len(fs) != 1 {
		return nil, fmt.Errorf("expected one definition of %s, found %d", name, len(fs))
	}
	return fs[0], nil
}

// equalityContext reports whether the result of a comparison call is used as
// "the strings are equal" (or "has the prefix"): !strcmp(…), strcmp(…) == 0,
// 0 == strcmp(…), g_str_equal(…), g_str_has_prefix(…).
func equalityContext(ts []token, c call) bool {
	at := func(i int) token {
		if i < 0 || i >= len(ts) {
			return token{}
		}
		return ts[i]
	}
	before := at(c.at - 1)
	switch c.name {
	case "g_strcmp0", "strcmp":
		return before.is("!") ||
			(at(c.close+1).is("==") && at(c.close+2).text == "0") ||
			(before.is("==") && at(c.at-2).text == "0")
	case "g_str_equal", "g_str_has_prefix":
		return !before.is("!")
	}
	return false
}

var stringFuncs = []string{"g_strcmp0", "strcmp", "g_str_equal", "g_str_has_prefix",
	"g_ascii_strcasecmp", "strcasecmp", "g_ascii_strncasecmp", "strncasecmp", "strncmp", "g_str_has_suffix", "g_strstr_len", "strstr"}

// comparisons returns the names compared with subject (a token sequence such
// as keys [ j ]) in f: exact matches and prefix matches.
func comparisons(f *funcDef, isSubject func([]token) bool, macros macroTable) ([]factName, error) {
	var out []factName
	for _, fn := range stringFuncs {
		cs, err := calls(f, fn)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			if len(c.args) < 2 {
				continue
			}
			var other []token
			switch {
			case isSubject(c.args[0]):
				other = c.args[1]
			case isSubject(c.args[1]) && fn != "g_str_has_prefix":
				other = c.args[0]
			default:
				continue
			}
			if err := checkCertain(c); err != nil {
				return nil, err
			}
			name, ok, err := stringValue(other, macros)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("%s: %s compares with something that is not a string constant", f, fn)
			}
			if fn != "g_strcmp0" && fn != "strcmp" && fn != "g_str_equal" && fn != "g_str_has_prefix" {
				return nil, fmt.Errorf("%s: unsupported comparison %s(…, %q): the knowledge base only models exact and prefix matches", f, fn, name)
			}
			if !equalityContext(f.cu.toks, c) {
				return nil, fmt.Errorf("%s: %s(…, %q) is not used as an equality test", f, fn, name)
			}
			out = append(out, factName{name: name, prefix: fn == "g_str_has_prefix"})
		}
	}
	return mergeFacts(out), nil
}

// mergeFacts sorts facts by name; a prefix match subsumes an exact match of
// the same name.
func mergeFacts(in []factName) []factName {
	m := map[string]bool{}
	for _, f := range in {
		m[f.name] = m[f.name] || f.prefix
	}
	out := make([]factName, 0, len(m))
	for _, n := range sortedKeys(m) {
		out = append(out, factName{name: n, prefix: m[n]})
	}
	return out
}

// tableKeys reads the keys load_per_table_info_from_key_file compares.
func tableKeys(cus map[string]*cfgUnit, macros macroTable) ([]factName, error) {
	f, err := findOneFunc(cus, "load_per_table_info_from_key_file")
	if err != nil {
		return nil, err
	}
	ts := f.cu.toks
	keyVars := map[string]bool{}
	for i := f.open + 1; i < f.close-2; i++ {
		if ts[i].kind == tkIdent && ts[i+1].is("=") && ts[i+2].isIdent("g_key_file_get_keys") {
			keyVars[ts[i].text] = true
		}
	}
	if len(keyVars) == 0 {
		return nil, fmt.Errorf("%s: no g_key_file_get_keys result found", f)
	}
	isKey := func(a []token) bool {
		return len(a) == 4 && a[0].kind == tkIdent && keyVars[a[0].text] && a[1].is("[") && a[2].kind == tkIdent && a[3].is("]")
	}
	keys, err := comparisons(f, isKey, macros)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s: no table key found", f)
	}
	return keys, nil
}

// masqueradeFunctions reads the names get_function_pointer_for tests.
func masqueradeFunctions(cus map[string]*cfgUnit, macros macroTable) ([]factName, error) {
	f, err := findOneFunc(cus, "get_function_pointer_for")
	if err != nil {
		return nil, err
	}
	if len(f.params) != 1 || f.params[0] == "" {
		return nil, fmt.Errorf("%s: expected one named parameter", f)
	}
	param := f.params[0]
	isParam := func(a []token) bool { return len(a) == 1 && a[0].isIdent(param) }
	names, err := comparisons(f, isParam, macros)
	if err != nil {
		return nil, err
	}
	// The empty string selects the identity function explicitly: it is the
	// fallback, not a masking function.
	names = slices.DeleteFunc(names, func(n factName) bool { return n.name == "" })
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: no masking function found", f)
	}
	return names, nil
}

// productNames reads the names get_product_name returns, lowercased as
// mydumper does when it builds group names.
func productNames(cus map[string]*cfgUnit, macros macroTable) ([]string, error) {
	f, err := findOneFunc(cus, "get_product_name")
	if err != nil {
		return nil, err
	}
	ts := f.cu.toks
	set := map[string]bool{}
	for i := f.open + 1; i < f.close; i++ {
		if !ts[i].isIdent("return") {
			continue
		}
		j := i + 1
		for j < f.close && !ts[j].is(";") {
			j++
		}
		v, ok, err := stringValue(ts[i+1:j], macros)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s: returns something that is not a string constant", f)
		}
		if v != "" {
			set[strings.ToLower(v)] = true
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("%s: no product found", f)
	}
	return sortedKeys(set), nil
}

// loaderFunctions are the functions whose behavior the emulators reproduce
// (design §3.2, §3.4).
var loaderFunctions = []string{"load_config_file", "parse_key_file_group"}

// loaderFingerprint hashes the tokens of the loader functions, from the name
// to the closing brace, directives included, comments and whitespace
// excluded: sha256 of the token texts joined by "\n", the two functions
// separated by "\n--\n". Any token change, even in a branch the official
// build does not compile, changes the fingerprint. It also returns the hash
// of each function alone, so that a report can say which one changed.
func loaderFingerprint(cus map[string]*cfgUnit) (string, map[string]string, error) {
	all := sha256.New()
	each := map[string]string{}
	for i, name := range loaderFunctions {
		f, err := findOneFunc(cus, name)
		if err != nil {
			return "", nil, fmt.Errorf("fingerprint: %w", err)
		}
		var sb strings.Builder
		from, to := f.cu.rawIdx[f.nameIdx], f.cu.rawIdx[f.close]
		for k, t := range f.cu.u.raw[from : to+1] {
			if k > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(t.text)
		}
		if i > 0 {
			all.Write([]byte("\n--\n"))
		}
		all.Write([]byte(sb.String()))
		one := sha256.Sum256([]byte(sb.String()))
		each[name] = hex.EncodeToString(one[:])
	}
	return hex.EncodeToString(all.Sum(nil)), each, nil
}
