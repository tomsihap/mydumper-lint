package lsp

import (
	"sort"
	"unicode/utf8"
)

// Encoding is the unit LSP positions count characters in, negotiated at
// initialization: UTF-16 code units unless the client offers another one.
type Encoding string

// Position encodings.
const (
	UTF8  Encoding = "utf-8"
	UTF16 Encoding = "utf-16"
	UTF32 Encoding = "utf-32"
)

// document is an open text document: its bytes, and where its lines start.
// LSP ends a line at "\n", "\r\n" or "\r"; the linter works in byte offsets.
type document struct {
	uri     string
	path    string // "" when the URI is not a file
	version int32
	text    []byte
	starts  []int // byte offset of each line
}

func newDocument(uri, path string, version int32, text []byte) *document {
	d := &document{uri: uri, path: path, version: version}
	d.setText(text)
	return d
}

func (d *document) setText(text []byte) {
	d.text = text
	d.starts = d.starts[:0]
	d.starts = append(d.starts, 0)
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			d.starts = append(d.starts, i+1)
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			d.starts = append(d.starts, i+1)
		}
	}
}

// lineEnd returns the offset where line's content ends, before its
// terminator.
func (d *document) lineEnd(line int) int {
	if line+1 >= len(d.starts) {
		return len(d.text)
	}
	end := d.starts[line+1]
	if end > 0 && d.text[end-1] == '\n' {
		end--
	}
	if end > d.starts[line] && d.text[end-1] == '\r' {
		end--
	}
	return end
}

// units returns how many units of enc the bytes b count for.
func units(b []byte, enc Encoding) int {
	if enc == UTF8 {
		return len(b)
	}
	n := 0
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		if enc == UTF16 && r >= 0x10000 && size == 4 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// position converts a byte offset to a position.
func (d *document) position(off int, enc Encoding) Position {
	off = max(0, min(off, len(d.text)))
	line := sort.Search(len(d.starts), func(i int) bool { return d.starts[i] > off }) - 1
	start := d.starts[line]
	return Position{Line: line, Character: units(d.text[start:off], enc)}
}

func (d *document) rangeOf(start, end int, enc Encoding) Range {
	return Range{Start: d.position(start, enc), End: d.position(end, enc)}
}

// offset converts a position to a byte offset. A character past the end of
// its line means the end of the line, and a line past the end of the
// document means the end of the document, as LSP asks.
func (d *document) offset(p Position, enc Encoding) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(d.starts) {
		return len(d.text)
	}
	i, end := d.starts[p.Line], d.lineEnd(p.Line)
	for n := 0; i < end && n < p.Character; {
		_, size := utf8.DecodeRune(d.text[i:end])
		n += units(d.text[i:i+size], enc)
		i += size
	}
	return i
}

// replace applies an incremental change.
func (d *document) replace(r Range, text string, enc Encoding) {
	start, end := d.offset(r.Start, enc), d.offset(r.End, enc)
	end = max(start, end)
	out := make([]byte, 0, len(d.text)-(end-start)+len(text))
	out = append(out, d.text[:start]...)
	out = append(out, text...)
	out = append(out, d.text[end:]...)
	d.setText(out)
}
