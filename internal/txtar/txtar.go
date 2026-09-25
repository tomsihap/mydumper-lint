// Package txtar reads and writes the txtar archive format used by the golden
// tests (the format of golang.org/x/tools/txtar, reimplemented to avoid a
// dependency).
//
// An archive is a comment followed by files:
//
//	comment lines
//	-- name --
//	content
//	-- other --
//	content
//
// A file whose name ends in ".esc" holds a single line of Go string escapes
// (without the surrounding quotes). It represents bytes a plain section
// cannot: carriage returns, NUL, a BOM, trailing spaces or a missing final
// newline. Get returns the decoded bytes for "name" whether the archive stores
// "name" or "name.esc".
package txtar

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// File is one file of an archive.
type File struct {
	Name string
	Data []byte
}

// Archive is a parsed txtar archive.
type Archive struct {
	Comment []byte
	Files   []File
}

// Parse parses a txtar archive. It never fails: text before the first marker
// is the comment.
func Parse(data []byte) *Archive {
	a := &Archive{}
	var cur *File
	for len(data) > 0 {
		line, rest := data, []byte(nil)
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, rest = data[:i+1], data[i+1:]
		}
		if name, ok := marker(line); ok {
			a.Files = append(a.Files, File{Name: name})
			cur = &a.Files[len(a.Files)-1]
		} else if cur == nil {
			a.Comment = append(a.Comment, line...)
		} else {
			cur.Data = append(cur.Data, line...)
		}
		data = rest
	}
	return a
}

func marker(line []byte) (string, bool) {
	s := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	if !strings.HasPrefix(s, "-- ") || !strings.HasSuffix(s, " --") || len(s) < 7 {
		return "", false
	}
	return strings.TrimSpace(s[3 : len(s)-3]), true
}

// Format serializes an archive. Every file content ends with a newline.
func Format(a *Archive) []byte {
	var b bytes.Buffer
	b.Write(fixNL(a.Comment))
	for _, f := range a.Files {
		fmt.Fprintf(&b, "-- %s --\n", f.Name)
		b.Write(fixNL(f.Data))
	}
	return b.Bytes()
}

func fixNL(data []byte) []byte {
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return data
	}
	return append(append([]byte{}, data...), '\n')
}

// Get returns the decoded content of the file called name (or name.esc).
func (a *Archive) Get(name string) ([]byte, bool, error) {
	for _, f := range a.Files {
		switch f.Name {
		case name:
			return f.Data, true, nil
		case name + ".esc":
			b, err := Unescape(f.Data)
			return b, true, err
		}
	}
	return nil, false, nil
}

// Set stores data under name, choosing name or name.esc with NeedsEscape,
// and replacing any existing version of the file.
func (a *Archive) Set(name string, data []byte) {
	f := File{Name: name, Data: data}
	if NeedsEscape(data) {
		f = File{Name: name + ".esc", Data: []byte(Escape(data) + "\n")}
	}
	for i := range a.Files {
		if a.Files[i].Name == name || a.Files[i].Name == name+".esc" {
			a.Files[i] = f
			return
		}
	}
	a.Files = append(a.Files, f)
}

// Delete removes name and name.esc.
func (a *Archive) Delete(name string) {
	out := a.Files[:0]
	for _, f := range a.Files {
		if f.Name != name && f.Name != name+".esc" {
			out = append(out, f)
		}
	}
	a.Files = out
}

// NeedsEscape reports whether data cannot be stored as a plain section:
// empty data, no final newline, CR, control characters other than tab and
// newline, a BOM, invalid UTF-8, trailing spaces or tabs on a line, or a line
// that looks like a marker.
func NeedsEscape(data []byte) bool {
	if len(data) == 0 || data[len(data)-1] != '\n' || !utf8.Valid(data) || bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return true
	}
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if _, ok := marker(line); ok {
			return true
		}
		body := bytes.TrimSuffix(line, []byte("\n"))
		if len(body) > 0 && (body[len(body)-1] == ' ' || body[len(body)-1] == '\t') {
			return true
		}
		for _, c := range body {
			if (c < 0x20 && c != '\t') || c == 0x7f {
				return true
			}
		}
	}
	return false
}

// Escape encodes data as a single line of Go string escapes.
func Escape(data []byte) string {
	q := strconv.Quote(string(data))
	return q[1 : len(q)-1]
}

// Unescape decodes an .esc section (one line; the final newline is ignored).
func Unescape(sec []byte) ([]byte, error) {
	s := strings.TrimSuffix(strings.TrimSuffix(string(sec), "\n"), "\r")
	u, err := strconv.Unquote(`"` + s + `"`)
	if err != nil {
		return nil, fmt.Errorf("invalid .esc section %q: %w", s, err)
	}
	return []byte(u), nil
}
