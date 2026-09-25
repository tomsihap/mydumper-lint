package keyfile

import (
	"bytes"
	"slices"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

const (
	maxRecoverPasses = 10
	// From this pass on, every rejected line is neutralized into a comment,
	// which guarantees termination.
	neutralizeFromPass = 5
)

// Recover returns the file the author meant: a loadable file where every
// rejected line has been repaired by the recovery rule of its cause (design
// §7.2) or, when there is none, neutralized into a comment. A loadable file
// is returned unchanged.
//
// Recover is deliberately independent of the rules' fixes: the fixer checks
// that safe fixes preserve the model of the recovered file, so the two
// implementations validate each other.
func Recover(b []byte) []byte {
	for pass := 0; pass < maxRecoverPasses; pass++ {
		f := source.New("", b)
		r := Parse(f, preprocess.Run(f))
		if r.Loadable {
			return b
		}
		var edits []diag.Edit
		for i, lc := range r.Lines {
			if lc.Kind == KindRejected {
				edits = append(edits, recoveryEdit(f, f.Lines[i], lc.Cause, pass >= neutralizeFromPass))
			}
		}
		b = applyEdits(b, edits)
	}
	return b
}

func recoveryEdit(f *source.File, l source.Line, c Cause, neutralize bool) diag.Edit {
	content := f.Content(l.Num)
	if !neutralize {
		switch c {
		case CauseBOM:
			return diag.Edit{Start: 0, End: len(bom)}
		case CauseWhitespaceOnly, CauseCarriageReturn:
			return diag.Edit{Start: l.Start, End: l.End}
		case CauseBracketLeakBlank:
			return diag.Edit{Start: l.Start, End: l.End + 1}
		case CauseBracketLeakFlag:
			at := l.Start + len(content) - crLen(content)
			return diag.Edit{Start: at, End: at, New: "=1"}
		case CauseMissingFinalNewline:
			return diag.Edit{Start: l.End, End: l.End, New: "\n"}
		case CauseInvalidGroupLine:
			if cut, ok := headerCommentCut(content); ok {
				return diag.Edit{Start: l.Start + cut, End: l.Start + len(content) - crLen(content)}
			}
		case NoCause, CauseEmptyKey, CauseKeyBeforeGroup, CauseInvalidKeyName, CauseNulByte,
			CauseBracketNoValue, CauseUnknown:
			// No recovery rule: the line is neutralized below.
		}
	}
	return diag.Edit{Start: l.Start, End: l.End, New: "#"}
}

// headerCommentCut returns the offset just after the ']' of a header followed
// by a '#' comment ("[g]  # main"), whose recovery drops the comment.
func headerCommentCut(content []byte) (int, bool) {
	ls := 0
	for ls < len(content) && isSpace(content[ls]) {
		ls++
	}
	if ls >= len(content) || content[ls] != '[' {
		return 0, false
	}
	end := bytes.IndexByte(content[ls:], ']')
	if end < 0 {
		return 0, false
	}
	cut := ls + end + 1
	rest := bytes.TrimLeft(content[cut:], " \t")
	return cut, len(rest) > 0 && rest[0] == '#'
}

func crLen(content []byte) int {
	if len(content) > 0 && content[len(content)-1] == '\r' {
		return 1
	}
	return 0
}

// applyEdits applies non-overlapping edits to b.
func applyEdits(b []byte, edits []diag.Edit) []byte {
	slices.SortFunc(edits, func(x, y diag.Edit) int { return x.Start - y.Start })
	out := make([]byte, 0, len(b))
	prev := 0
	for _, e := range edits {
		out = append(out, b[prev:e.Start]...)
		out = append(out, e.New...)
		prev = e.End
	}
	return append(out, b[prev:]...)
}
