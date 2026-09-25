// Package preprocess emulates the byte-level rewrite that mydumper applies to
// a configuration file before handing it to GLib (design §3.2).
//
// mydumper appends "= 1" before the '\n' of every line that contains no '='
// so that valueless options ("routines") become "routines= 1". Its state
// machine is not reset after a line containing '[': the rest of such a line is
// copied verbatim and the next line starts with a stale state (P1).
//
// This package reproduces that behavior exactly, line by line, and also
// computes what an "ideal" pre-processor (one that resets its state after
// every line) would do. Lines on which the two differ are the ones affected by
// the state leak (MDL108). The implementation follows the behavioral
// description of the design document; it does not derive from mydumper's code.
package preprocess

import (
	"bytes"

	"github.com/tomsihap/mydumper-lint/internal/source"
)

// EqOne is the text mydumper inserts before '\n'.
const EqOne = "= 1"

// LineInfo describes what the pre-processor does with one line.
type LineInfo struct {
	// AppendsEqOne reports whether mydumper inserts "= 1" before this line's '\n'.
	AppendsEqOne bool
	// IdealAppendsEqOne reports whether a pre-processor that resets its state
	// after every line would.
	IdealAppendsEqOne bool
	// BracketAt is the index in the line content of the first '[', from which
	// the rest of the line is copied verbatim; -1 if the line has no '['.
	BracketAt int
	// StartDirty reports whether the line starts with a state leaked from a
	// previous line containing '['.
	StartDirty bool
	// LeakOrigin is the 1-based number of the line that started the leak chain
	// this line inherits from (the first '[' line with text before its '['),
	// or 0 when the line starts clean.
	LeakOrigin int
	// OutNewLine and OutEqualFound are mydumper's state after the line. After
	// a line with a '\n' and no '[', the state is reset to (true, false).
	OutNewLine, OutEqualFound bool
}

// Result holds one LineInfo per line of the file, in order.
type Result struct {
	Lines []LineInfo
	// Passthrough is set for mydumper versions whose loader hands the file to
	// GLib unchanged (v0.19.1-x): no "= 1", no state leak.
	Passthrough bool
}

// Passthrough returns the pre-processing of a version without pre-processor:
// every line reaches GLib unchanged.
func Passthrough(f *source.File) *Result {
	r := &Result{Lines: make([]LineInfo, len(f.Lines)), Passthrough: true}
	for i := range r.Lines {
		r.Lines[i] = LineInfo{BracketAt: -1, OutNewLine: true}
	}
	return r
}

// state is the pre-processor's (new_line, equal_found) pair.
type state struct{ newLine, equalFound bool }

var clean = state{newLine: true}

// scan feeds bytes to the state machine, as the non-'[' branch does.
func (s state) scan(b []byte) state {
	if len(b) > 0 {
		s.newLine = false
		if bytes.IndexByte(b, '=') >= 0 {
			s.equalFound = true
		}
	}
	return s
}

// Run computes the per-line behavior of the real and ideal pre-processors.
func Run(f *source.File) *Result {
	r := &Result{Lines: make([]LineInfo, len(f.Lines))}
	in := clean
	origin := 0
	for i, l := range f.Lines {
		content := f.Content(l.Num)
		info := LineInfo{
			BracketAt:  bytes.IndexByte(content, '['),
			StartDirty: in != clean,
		}
		if info.StartDirty {
			info.LeakOrigin = origin
		}
		var out state
		if info.BracketAt >= 0 {
			// The rest of the line (and its '\n') is copied without touching the state.
			out = in.scan(content[:info.BracketAt])
			if !info.StartDirty {
				origin = l.Num // text before '[' dirties the state here
			}
		} else {
			mid := in.scan(content)
			if l.HasNewline {
				info.AppendsEqOne = !mid.equalFound && !mid.newLine
				info.IdealAppendsEqOne = len(content) > 0 && bytes.IndexByte(content, '=') < 0
			}
			out = clean
			if !l.HasNewline {
				out = mid
			}
		}
		info.OutNewLine, info.OutEqualFound = out.newLine, out.equalFound
		r.Lines[i] = info
		in = out
	}
	return r
}

// Apply returns the exact bytes mydumper hands to GLib for the file b.
//
// GLib's g_file_get_contents NUL-terminates the buffer, and the copy after a
// '[' reads that terminator when it reaches the end of the file, so a NUL byte
// is appended in that case. It is harmless for GKeyFile but kept for fidelity.
func Apply(b []byte) []byte {
	return apply(b, false)
}

// ApplyIdeal is Apply with the state reset after lines containing '['.
func ApplyIdeal(b []byte) []byte {
	return apply(b, true)
}

func apply(b []byte, ideal bool) []byte {
	out := make([]byte, 0, len(b)+len(b)/8)
	s := clean
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch c {
		case '[':
			for i < len(b) && b[i] != '\n' {
				out = append(out, b[i])
				i++
			}
			if i == len(b) {
				out = append(out, 0) // the buffer's NUL terminator
				return out
			}
			out = append(out, '\n') // copied without going through the '\n' branch
			if ideal {
				s = clean
			}
		case '\n':
			if !s.equalFound && !s.newLine {
				out = append(out, EqOne...)
			}
			s = clean
			out = append(out, c)
		default:
			if c == '=' {
				s.equalFound = true
			}
			s.newLine = false
			out = append(out, c)
		}
	}
	return out
}
