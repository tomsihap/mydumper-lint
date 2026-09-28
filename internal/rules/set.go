package rules

import (
	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// Set is what a cross-file rule sees: the defaults file and the extra file
// one mydumper invocation loads (design §9.3, load sets).
type Set struct {
	Defaults, Extra *Pass

	rule     *Rule
	severity diag.Severity
	out      map[*Pass][]diag.Diagnostic
}

// Report records a diagnostic for one of the set's files.
func (s *Set) Report(on *Pass, d diag.Diagnostic) {
	d.RuleID, d.RuleName = s.rule.ID, s.rule.Name
	if d.Severity == diag.Off || s.severity != s.rule.Severity {
		d.Severity = s.severity
	}
	s.out[on] = append(s.out[on], d)
}

// RunSet runs the enabled cross-file rules and returns the diagnostics of
// each file, sorted. Suppressions of each file apply.
func RunSet(enabled []Enabled, defaults, extra *Pass) (onDefaults, onExtra []diag.Diagnostic) {
	s := &Set{Defaults: defaults, Extra: extra, out: map[*Pass][]diag.Diagnostic{}}
	for _, e := range enabled {
		if e.Rule.CheckSet == nil {
			continue
		}
		s.rule, s.severity = e.Rule, e.Severity
		e.Rule.CheckSet(s)
	}
	for _, p := range []*Pass{defaults, extra} {
		p.out = s.out[p]
		dirs, _ := p.directives()
		kept := p.out[:0]
		for _, d := range p.out {
			if !p.suppressed(dirs, d) {
				kept = append(kept, d)
			}
		}
		diag.Sort(kept)
		s.out[p] = kept
		p.out = nil
	}
	return s.out[defaults], s.out[extra]
}
