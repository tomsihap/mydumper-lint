// Package diag defines the diagnostic model shared by rules, the fixer and the
// reporters. It has no dependencies so that every other package can use it.
package diag

import (
	"cmp"
	"fmt"
	"slices"
)

// Severity of a diagnostic. The zero value, Off, means the rule is disabled.
type Severity uint8

// Severities, in increasing order.
const (
	Off Severity = iota
	Info
	Warning
	Error
)

var severityNames = [...]string{Off: "off", Info: "info", Warning: "warning", Error: "error"}

func (s Severity) String() string {
	if int(s) < len(severityNames) {
		return severityNames[s]
	}
	return fmt.Sprintf("severity(%d)", s)
}

// ParseSeverity parses "off", "info", "warning" or "error".
func ParseSeverity(s string) (Severity, error) {
	for i, name := range severityNames {
		if s == name {
			return Severity(i), nil
		}
	}
	return Off, fmt.Errorf("unknown severity %q (want off, info, warning or error)", s)
}

// AtLeast reports whether s is enabled and at least as severe as threshold.
func (s Severity) AtLeast(threshold Severity) bool {
	return s != Off && s >= threshold
}

// Span is a half-open byte range [Start, End) in the original file.
type Span struct {
	Start, End int
}

// Len returns the number of bytes covered by the span.
func (s Span) Len() int { return s.End - s.Start }

// Empty reports whether the span covers no byte (an insertion point).
func (s Span) Empty() bool { return s.End <= s.Start }

// Overlaps reports whether two spans share at least one byte.
func (s Span) Overlaps(o Span) bool {
	return s.Start < o.End && o.Start < s.End
}

// Touches reports whether two spans overlap or are adjacent. Two fixes whose
// edits touch are applied in different passes (design §7.4).
func (s Span) Touches(o Span) bool {
	return s.Start <= o.End && o.Start <= s.End
}

// Applicability tells whether a fix may be applied automatically.
type Applicability uint8

// Fix applicabilities (design §7.1).
const (
	Safe Applicability = iota + 1
	Unsafe
)

func (a Applicability) String() string {
	switch a {
	case Safe:
		return "safe"
	case Unsafe:
		return "unsafe"
	default:
		return fmt.Sprintf("applicability(%d)", a)
	}
}

// Edit replaces the bytes in [Start, End) of the original file with New.
type Edit struct {
	Start, End int
	New        string
}

// Span returns the byte range replaced by the edit.
func (e Edit) Span() Span { return Span{e.Start, e.End} }

// Fix is a set of edits that must be applied together.
type Fix struct {
	Applicability Applicability
	Description   string
	Edits         []Edit
}

// Related points at another location that explains a diagnostic, such as the
// origin line of a bracket state leak.
type Related struct {
	Span    Span
	Message string
}

// Diagnostic is one finding reported by a rule.
type Diagnostic struct {
	RuleID      string // e.g. "MDL102"
	RuleName    string // e.g. "whitespace-only-line"
	Severity    Severity
	Span        Span
	Message     string
	Consequence string // what mydumper does, for the target version
	Related     []Related
	Fix         *Fix
}

// Sort orders diagnostics by span start, span end, then rule ID, which is the
// order used by every output format.
func Sort(ds []Diagnostic) {
	slices.SortStableFunc(ds, func(a, b Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.Span.Start, b.Span.Start),
			cmp.Compare(a.Span.End, b.Span.End),
			cmp.Compare(a.RuleID, b.RuleID),
		)
	})
}
