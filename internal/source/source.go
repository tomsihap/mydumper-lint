// Package source holds a configuration file as a lossless table of lines.
//
// Lines are split on '\n' only, exactly like mydumper's pre-processor and
// GLib's GKeyFile: a '\r' before '\n' is part of the line content. Offsets are
// bytes; positions shown to users count Unicode code points.
package source

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

// Line is one line of a file.
type Line struct {
	Num        int  // 1-based line number
	Start, End int  // content span; End is the offset of the '\n' (or of EOF)
	HasNewline bool // false only for a last line that is not terminated by '\n'
}

// File is a configuration file and its line table.
type File struct {
	Path  string
	Bytes []byte
	Lines []Line
}

// New builds the line table of b. The file keeps a reference to b.
func New(path string, b []byte) *File {
	f := &File{Path: path, Bytes: b}
	if len(b) > 0 {
		f.Lines = make([]Line, 0, bytes.Count(b, []byte{'\n'})+1)
	}
	start := 0
	for start < len(b) {
		i := bytes.IndexByte(b[start:], '\n')
		if i < 0 {
			f.Lines = append(f.Lines, Line{Num: len(f.Lines) + 1, Start: start, End: len(b)})
			break
		}
		f.Lines = append(f.Lines, Line{Num: len(f.Lines) + 1, Start: start, End: start + i, HasNewline: true})
		start += i + 1
	}
	return f
}

// Line returns line n (1-based).
func (f *File) Line(n int) Line { return f.Lines[n-1] }

// Content returns the bytes of line n (1-based), without the '\n'.
func (f *File) Content(n int) []byte {
	l := f.Lines[n-1]
	return f.Bytes[l.Start:l.End]
}

// EndsWithCR reports whether line n ends with a '\r' (a CRLF line ending, or
// a stray carriage return at the end of the last line).
func (f *File) EndsWithCR(n int) bool {
	c := f.Content(n)
	return len(c) > 0 && c[len(c)-1] == '\r'
}

// LineSpan returns the byte range of line n. With withNewline, the range
// includes the terminating '\n' when there is one.
func (f *File) LineSpan(n int, withNewline bool) (start, end int) {
	l := f.Lines[n-1]
	end = l.End
	if withNewline && l.HasNewline {
		end++
	}
	return l.Start, end
}

// LineOf returns the 1-based number of the line containing offset. An offset
// just after a final '\n' belongs to the (virtual) line after the last one.
func (f *File) LineOf(offset int) int {
	if len(f.Lines) == 0 {
		return 1
	}
	// First line whose end (including its '\n') is at or after offset.
	i := sort.Search(len(f.Lines), func(i int) bool { return f.Lines[i].End >= offset })
	if i == len(f.Lines) {
		last := f.Lines[len(f.Lines)-1]
		if last.HasNewline {
			return last.Num + 1
		}
		return last.Num
	}
	return i + 1
}

// Position is a user-facing location: 1-based line and column, the column
// counted in Unicode code points (an invalid UTF-8 byte counts as one).
type Position struct {
	Line, Col, Offset int
}

// Position converts a byte offset into a Position.
func (f *File) Position(offset int) Position {
	n := f.LineOf(offset)
	start := len(f.Bytes)
	if n <= len(f.Lines) {
		start = f.Lines[n-1].Start
	}
	return Position{Line: n, Col: 1 + utf8.RuneCount(f.Bytes[start:offset]), Offset: offset}
}
