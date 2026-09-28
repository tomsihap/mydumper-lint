package lsp

import "testing"

func TestPositions(t *testing.T) {
	// é is 2 bytes, 1 UTF-16 unit; 😀 is 4 bytes, 2 UTF-16 units, 1 UTF-32 unit.
	text := "ab\r\né😀x\ry\n\xffz"
	d := newDocument("file:///t.cnf", "/t.cnf", 1, []byte(text))
	if len(d.starts) != 4 {
		t.Fatalf("lines %v, want 4 (CRLF, lone CR and LF end lines)", d.starts)
	}
	tests := []struct {
		off                int
		utf8, utf16, utf32 Position
	}{
		{0, Position{0, 0}, Position{0, 0}, Position{0, 0}},
		{2, Position{0, 2}, Position{0, 2}, Position{0, 2}},             // before \r\n
		{4, Position{1, 0}, Position{1, 0}, Position{1, 0}},             // é
		{6, Position{1, 2}, Position{1, 1}, Position{1, 1}},             // 😀
		{10, Position{1, 6}, Position{1, 3}, Position{1, 2}},            // x
		{12, Position{2, 0}, Position{2, 0}, Position{2, 0}},            // y
		{14, Position{3, 0}, Position{3, 0}, Position{3, 0}},            // \xff
		{15, Position{3, 1}, Position{3, 1}, Position{3, 1}},            // z
		{len(text), Position{3, 2}, Position{3, 2}, Position{3, 2}},     // end
		{len(text) + 5, Position{3, 2}, Position{3, 2}, Position{3, 2}}, // clamped
	}
	for _, tt := range tests {
		for enc, want := range map[Encoding]Position{UTF8: tt.utf8, UTF16: tt.utf16, UTF32: tt.utf32} {
			got := d.position(tt.off, enc)
			if got != want {
				t.Errorf("position(%d, %s) = %+v, want %+v", tt.off, enc, got, want)
			}
			if tt.off <= len(text) {
				if back := d.offset(got, enc); back != tt.off {
					t.Errorf("offset(%+v, %s) = %d, want %d", got, enc, back, tt.off)
				}
			}
		}
	}
	// Past the end of a line: its end, before the terminator. Past the last
	// line: the end of the document.
	if got := d.offset(Position{0, 99}, UTF16); got != 2 {
		t.Errorf("offset past the end of line 0 = %d, want 2", got)
	}
	if got := d.offset(Position{9, 0}, UTF16); got != len(text) {
		t.Errorf("offset past the last line = %d", got)
	}
	if got := d.offset(Position{-1, 0}, UTF16); got != 0 {
		t.Errorf("offset of a negative line = %d", got)
	}
}

func TestReplace(t *testing.T) {
	d := newDocument("u", "", 1, []byte("[mydumper]\nthreads=4\n"))
	d.replace(Range{Position{1, 8}, Position{1, 9}}, "8", UTF16)
	d.replace(Range{Position{2, 0}, Position{2, 0}}, "routines\n", UTF16)
	if got := string(d.text); got != "[mydumper]\nthreads=8\nroutines\n" {
		t.Errorf("text = %q", got)
	}
	if len(d.starts) != 4 {
		t.Errorf("line starts not updated: %v", d.starts)
	}
	d.replace(Range{Position{1, 5}, Position{1, 2}}, "", UTF16) // reversed range: nothing removed
	if got := string(d.text); got != "[mydumper]\nthreads=8\nroutines\n" {
		t.Errorf("reversed range changed the text: %q", got)
	}
}
