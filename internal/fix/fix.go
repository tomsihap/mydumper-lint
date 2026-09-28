// Package fix applies the fixes attached to diagnostics (design §7): edits on
// the original bytes, conflict resolution, repeated passes until nothing
// changes, and the self-check that guards every write.
package fix

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/textdiff"
)

// DefaultMaxPasses bounds the fixpoint loop (design §7.4).
const DefaultMaxPasses = 10

// ErrNoFixpoint means fixes kept producing new fixes: an internal error.
var ErrNoFixpoint = errors.New("fixes did not converge")

// Apply applies as many non-conflicting fixes as possible, lowest rule ID
// first, then in file order. Two fixes conflict when any of their edits
// overlap or touch; the loser waits for the next pass. A fix whose own edits
// overlap is invalid and skipped. It returns the new content and the number
// of fixes applied.
func Apply(src []byte, ds []diag.Diagnostic) ([]byte, int) {
	out, applied := apply(src, ds)
	return out, len(applied)
}

// apply is Apply, returning the fixes that were applied.
func apply(src []byte, ds []diag.Diagnostic) ([]byte, []*diag.Fix) {
	cands := make([]diag.Diagnostic, 0, len(ds))
	for _, d := range ds {
		if d.Fix != nil && len(d.Fix.Edits) > 0 && valid(d.Fix.Edits, len(src)) {
			cands = append(cands, d)
		}
	}
	slices.SortStableFunc(cands, func(a, b diag.Diagnostic) int {
		return cmp.Or(cmp.Compare(a.RuleID, b.RuleID), cmp.Compare(firstStart(a), firstStart(b)))
	})
	var accepted []diag.Edit
	var applied []*diag.Fix
	for _, c := range cands {
		if conflicts(c.Fix.Edits, accepted) {
			continue
		}
		accepted = append(accepted, c.Fix.Edits...)
		applied = append(applied, c.Fix)
	}
	slices.SortFunc(accepted, func(a, b diag.Edit) int { return cmp.Or(a.Start-b.Start, a.End-b.End) })
	out := make([]byte, 0, len(src))
	prev := 0
	for _, e := range accepted {
		out = append(out, src[prev:e.Start]...)
		out = append(out, e.New...)
		prev = e.End
	}
	return append(out, src[prev:]...), applied
}

func firstStart(d diag.Diagnostic) int {
	m := d.Fix.Edits[0].Start
	for _, e := range d.Fix.Edits[1:] {
		m = min(m, e.Start)
	}
	return m
}

// valid checks that edits are in range and do not overlap each other.
func valid(edits []diag.Edit, n int) bool {
	for i, e := range edits {
		if e.Start < 0 || e.End < e.Start || e.End > n {
			return false
		}
		for _, o := range edits[i+1:] {
			if e.Span().Overlaps(o.Span()) || (e.Start == o.Start && e.End == o.End) {
				return false
			}
		}
	}
	return true
}

func conflicts(edits, accepted []diag.Edit) bool {
	for _, e := range edits {
		for _, a := range accepted {
			if e.Span().Touches(a.Span()) {
				return true
			}
		}
	}
	return false
}

// Linter lints content and returns its diagnostics with their fixes.
type Linter func(src []byte) []diag.Diagnostic

// Options selects the fixes to apply.
type Options struct {
	Unsafe     bool            // also apply unsafe fixes
	ExtendSafe map[string]bool // rule IDs whose unsafe fixes are applied as if safe
	MaxPasses  int             // 0 means DefaultMaxPasses
	// Health, when set, guards every pass (design §7.3: health never
	// degrades). A pass whose fixes, applied together, would lower the health
	// of the content is replayed one fix at a time: a fix that lowers it is
	// dropped, and not proposed again.
	Health func(src []byte) model.Health
}

// Result is the outcome of Fixpoint.
type Result struct {
	Output        []byte
	Applied       int               // fixes applied in total
	Passes        int               // passes that applied at least one fix
	UnsafeApplied bool              // at least one applied fix was unsafe
	Remaining     []diag.Diagnostic // diagnostics of Output
	Dropped       []diag.Diagnostic // fixes the Health guard refused
}

// Fixpoint lints and fixes src repeatedly until no applicable fix remains.
func Fixpoint(src []byte, lint Linter, opt Options) (Result, error) {
	maxPasses := opt.MaxPasses
	if maxPasses <= 0 {
		maxPasses = DefaultMaxPasses
	}
	res := Result{Output: src}
	refused := map[string]bool{}
	for pass := 0; ; pass++ {
		ds := lint(res.Output)
		var cands []diag.Diagnostic
		for _, d := range ds {
			if d.Fix != nil && (d.Fix.Applicability == diag.Safe || opt.Unsafe || opt.ExtendSafe[d.RuleID]) &&
				!refused[signature(res.Output, d)] {
				cands = append(cands, d)
			}
		}
		if len(cands) == 0 {
			res.Remaining = ds
			return res, nil
		}
		if pass == maxPasses {
			res.Remaining = ds
			return res, fmt.Errorf("%w after %d passes", ErrNoFixpoint, maxPasses)
		}
		out, applied := apply(res.Output, cands)
		if opt.Health != nil {
			if base := opt.Health(res.Output); opt.Health(out) < base {
				var dropped []diag.Diagnostic
				out, applied, dropped = replay(res.Output, cands, base, opt.Health)
				for _, d := range dropped {
					refused[signature(res.Output, d)] = true
				}
				res.Dropped = append(res.Dropped, dropped...)
			}
		}
		res.Output, res.Applied = out, res.Applied+len(applied)
		if len(applied) > 0 {
			res.Passes++
		}
		for _, f := range applied {
			res.UnsafeApplied = res.UnsafeApplied || f.Applicability == diag.Unsafe
		}
	}
}

// replay applies the fixes of a pass one at a time, in the order apply
// accepts them, keeping each only if the content stays at least as healthy
// as base. The fixes of one pass never overlap, so any subset of them
// applies to src.
func replay(src []byte, cands []diag.Diagnostic, base model.Health, health func([]byte) model.Health) ([]byte, []*diag.Fix, []diag.Diagnostic) {
	_, order := apply(src, cands)
	byFix := map[*diag.Fix]diag.Diagnostic{} // candidates always carry a fix
	for _, d := range cands {
		byFix[d.Fix] = d
	}
	var kept []diag.Diagnostic
	var dropped []diag.Diagnostic
	for _, f := range order {
		trial := append(append([]diag.Diagnostic(nil), kept...), byFix[f])
		if out, _ := apply(src, trial); health(out) >= base {
			kept = trial
		} else {
			dropped = append(dropped, byFix[f])
		}
	}
	out, applied := apply(src, kept)
	return out, applied, dropped
}

// signature identifies a fix by its content, not its offsets, so that a
// refused fix is recognized when a later pass proposes it again.
func signature(src []byte, d diag.Diagnostic) string {
	s := d.RuleID + "\x00" + d.Message + "\x00" + d.Fix.Description
	for _, e := range d.Fix.Edits {
		if e.Start >= 0 && e.End <= len(src) && e.Start <= e.End {
			s += "\x00" + string(src[e.Start:e.End]) + "\x01" + e.New
		}
	}
	return s
}

// Measure returns the projection of the recovered model of content and the
// health of content itself (design §7.3).
type Measure func(src []byte) (recovered string, health model.Health)

// SelfCheckError reports a violated fixer invariant. It is an internal error:
// nothing must be written.
type SelfCheckError struct {
	Reason string
	Diff   string // unified diff of the recovered projections, when relevant
}

func (e *SelfCheckError) Error() string {
	if e.Diff == "" {
		return "fixer self-check failed: " + e.Reason
	}
	return "fixer self-check failed: " + e.Reason + "\n" + e.Diff
}

// SelfCheck verifies the invariants of design §7.3 between before and after:
// health never degrades, and when only safe fixes were applied, the recovered
// model is unchanged.
func SelfCheck(before, after []byte, unsafeApplied bool, measure Measure) error {
	rb, hb := measure(before)
	ra, ha := measure(after)
	if ha < hb {
		return &SelfCheckError{Reason: fmt.Sprintf("health would degrade from %s to %s", hb, ha)}
	}
	if !unsafeApplied && rb != ra {
		d := textdiff.Diff("before", []byte(rb), "after", []byte(ra))
		return &SelfCheckError{Reason: "safe fixes changed the effective configuration", Diff: string(d)}
	}
	return nil
}
