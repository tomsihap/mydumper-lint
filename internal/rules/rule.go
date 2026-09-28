// Package rules holds the rule catalog of design §6: metadata, the registry,
// rule selection, and one file per rule.
package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// Family groups rules by their hundred (design §6.3).
type Family string

// Rule families.
const (
	FamilySuppressions Family = "suppressions" // MDL0xx
	FamilyLoading      Family = "loading"      // MDL1xx: the whole file would be rejected
	FamilyGroups       Family = "groups"       // MDL2xx
	FamilyValues       Family = "values"       // MDL3xx: lines, keys, values
	FamilyOptions      Family = "options"      // MDL4xx
	FamilyTables       Family = "tables"       // MDL5xx: table sections and masking
	FamilyConnection   Family = "connection"   // MDL6xx: libmysqlclient
	FamilyConventions  Family = "conventions"  // MDL9xx
)

// Meta describes a rule. It feeds the docs, `explain`, `rules` and SARIF.
type Meta struct {
	ID             string        // e.g. "MDL102"
	Name           string        // stable kebab-case name
	Family         Family        //
	Severity       diag.Severity // default severity
	Fix            diag.Applicability
	OptIn          bool   // disabled unless selected explicitly
	Preview        bool   // requires preview mode
	Unsuppressible bool   // cannot be suppressed by comments (MDL1xx)
	Summary        string // one sentence
	Why            string // Markdown: why it matters, what mydumper does
	Refs           []string
	E2E            []string // end-to-end scenarios proving the runtime consequence
}

// Rule is a rule and its check. CheckSet, when set, also checks the files
// of a load set together (design §9.3).
type Rule struct {
	Meta
	Check    func(p *Pass)
	CheckSet func(s *Set)
}

var (
	registry = map[string]*Rule{}
	byName   = map[string]*Rule{}
)

func register(r *Rule) {
	if _, dup := registry[r.ID]; dup {
		panic("rules: duplicate ID " + r.ID)
	}
	if _, dup := byName[r.Name]; dup {
		panic("rules: duplicate name " + r.Name)
	}
	registry[r.ID], byName[r.Name] = r, r
}

// All returns every rule, sorted by ID.
func All() []*Rule {
	out := make([]*Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Lookup finds a rule by ID (case-insensitive) or name.
func Lookup(s string) (*Rule, bool) {
	if r, ok := registry[strings.ToUpper(s)]; ok {
		return r, true
	}
	r, ok := byName[s]
	return r, ok
}

// Pass is what a rule sees when it checks one file.
type Pass struct {
	File      *source.File
	Pre       *preprocess.Result
	KF        *keyfile.Result
	Model     *model.Model
	Target    model.Target // nil when no mydumper version is known
	Version   string       // resolved mydumper version, e.g. "v0.19.3-3"
	Languages []string     // runtime language list (K14)
	// Preprocessor tells whether the target version runs mydumper's
	// pre-processor (false for v0.19.1-x). Latent pre-processor risks are
	// only reported when it does.
	Preprocessor bool
	// BaseDir resolves relative file references of the configuration
	// (MDL508); empty means the working directory.
	BaseDir string
	// Conventions are the team's conventions (MDL901-MDL905); nil when off.
	Conventions *Conventions

	rule     *Rule
	severity diag.Severity
	out      []diag.Diagnostic
	kinds    []model.GroupKind // groupKind's cache
	optKeys  []optionKey       // optionKeys' cache
	optDone  bool
	headers  []keyfile.Group // validHeaders' cache
}

// groupKind returns who reads the group of header gi (an index in
// KF.Groups), computed once per pass.
func (p *Pass) groupKind(gi int) model.GroupKind {
	if p.kinds == nil {
		p.kinds = make([]model.GroupKind, len(p.KF.Groups))
		for i, g := range p.KF.Groups {
			p.kinds[i], _ = model.ClassifyGroup(g.Name, products(p))
		}
	}
	return p.kinds[gi]
}

// NewPass prepares a pass over a parsed file.
func NewPass(f *source.File, pre *preprocess.Result, kf *keyfile.Result, m *model.Model) *Pass {
	return &Pass{File: f, Pre: pre, KF: kf, Model: m, Preprocessor: !pre.Passthrough}
}

// Report records a diagnostic for the running rule. The rule ID and name are
// filled in. The severity is the configured one; a rule may set a different
// default for a given diagnostic, which a configured override still replaces.
func (p *Pass) Report(d diag.Diagnostic) {
	d.RuleID, d.RuleName = p.rule.ID, p.rule.Name
	if d.Severity == diag.Off || p.severity != p.rule.Severity {
		d.Severity = p.severity
	}
	p.out = append(p.out, d)
}

// Enabled is a selected rule with its effective severity.
type Enabled struct {
	Rule     *Rule
	Severity diag.Severity
}

// Run runs the enabled rules on the pass and returns the sorted diagnostics.
func (p *Pass) Run(enabled []Enabled) []diag.Diagnostic {
	p.out = nil
	for _, e := range enabled {
		p.rule, p.severity = e.Rule, e.Severity
		e.Rule.Check(p)
	}
	p.rule = nil
	p.suppress(enabled)
	diag.Sort(p.out)
	return p.out
}

// Selection chooses rules and severities (design §8.2, §9.2).
type Selection struct {
	Select       []string                 // IDs, names, prefixes such as "MDL1", or "ALL"; empty means ["ALL"]
	ExtendSelect []string                 // added to Select
	Ignore       []string                 // removed afterwards
	Severity     map[string]diag.Severity // per rule (ID or name); diag.Off disables
	Preview      bool                     // enable preview rules
}

// Resolve returns the enabled rules, sorted by ID. Opt-in rules are only
// enabled when selected by exact ID or name; preview rules need Preview.
func (s Selection) Resolve() ([]Enabled, error) {
	sel := s.Select
	if len(sel) == 0 {
		sel = []string{"ALL"}
	}
	chosen := map[string]bool{}
	for _, tok := range append(append([]string{}, sel...), s.ExtendSelect...) {
		rs, explicit, err := match(tok)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			if (r.OptIn && !explicit) || (r.Preview && !s.Preview) {
				continue
			}
			chosen[r.ID] = true
		}
	}
	for _, tok := range s.Ignore {
		rs, _, err := match(tok)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			delete(chosen, r.ID)
		}
	}
	sev := map[string]diag.Severity{}
	for tok, v := range s.Severity {
		r, ok := Lookup(tok)
		if !ok {
			return nil, unknownRule(tok)
		}
		sev[r.ID] = v
	}
	var out []Enabled
	for _, r := range All() {
		if !chosen[r.ID] {
			continue
		}
		e := Enabled{Rule: r, Severity: r.Severity}
		if v, ok := sev[r.ID]; ok {
			e.Severity = v
		}
		if e.Severity != diag.Off {
			out = append(out, e)
		}
	}
	return out, nil
}

// match resolves a selection token. explicit is true for an exact ID or name.
func match(tok string) (rs []*Rule, explicit bool, err error) {
	if strings.EqualFold(tok, "ALL") {
		return All(), false, nil
	}
	if r, ok := Lookup(tok); ok {
		return []*Rule{r}, true, nil
	}
	up := strings.ToUpper(tok)
	if strings.HasPrefix(up, "MDL") {
		for _, r := range All() {
			if strings.HasPrefix(r.ID, up) {
				rs = append(rs, r)
			}
		}
		if len(rs) > 0 {
			return rs, false, nil
		}
	}
	return nil, false, unknownRule(tok)
}

func unknownRule(tok string) error {
	if s := Suggest(tok); s != "" {
		return fmt.Errorf("unknown rule %q (did you mean %q?)", tok, s)
	}
	return fmt.Errorf("unknown rule %q", tok)
}

// Suggest returns the closest rule ID or name to s, or "".
func Suggest(s string) string {
	best, bestD := "", 3
	for _, r := range All() {
		for _, c := range []string{r.ID, r.Name} {
			if d := Levenshtein(strings.ToLower(s), strings.ToLower(c)); d < bestD {
				best, bestD = c, d
			}
		}
	}
	return best
}

// Levenshtein is the edit distance between a and b (bytes).
func Levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
