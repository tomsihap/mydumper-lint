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
}

// Result is the outcome of Fixpoint.
type Result struct {
	Output        []byte
	Applied       int               // fixes applied in total
	Passes        int               // passes that applied at least one fix
	UnsafeApplied bool              // at least one applied fix was unsafe
	Remaining     []diag.Diagnostic // diagnostics of Output
}

// Fixpoint lints and fixes src repeatedly until no applicable fix remains.
func Fixpoint(src []byte, lint Linter, opt Options) (Result, error) {
	maxPasses := opt.MaxPasses
	if maxPasses <= 0 {
		maxPasses = DefaultMaxPasses
	}
	res := Result{Output: src}
	for pass := 0; ; pass++ {
		ds := lint(res.Output)
		var cands []diag.Diagnostic
		for _, d := range ds {
			if d.Fix != nil && (d.Fix.Applicability == diag.Safe || opt.Unsafe || opt.ExtendSafe[d.RuleID]) {
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
		res.Output, res.Applied, res.Passes = out, res.Applied+len(applied), res.Passes+1
		for _, f := range applied {
			res.UnsafeApplied = res.UnsafeApplied || f.Applicability == diag.Unsafe
		}
	}
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
