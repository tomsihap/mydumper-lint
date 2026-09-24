package source

import (
	"reflect"
	"testing"
)

func TestNewSplitsOnLFOnly(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Line
	}{
		{"empty", "", nil},
		{"single newline", "\n", []Line{{Num: 1, Start: 0, End: 0, HasNewline: true}}},
		{"no final newline", "a\nbc", []Line{
			{Num: 1, Start: 0, End: 1, HasNewline: true},
			{Num: 2, Start: 2, End: 4, HasNewline: false},
		}},
		{"final newline adds no empty line", "a\n\n", []Line{
			{Num: 1, Start: 0, End: 1, HasNewline: true},
			{Num: 2, Start: 2, End: 2, HasNewline: true},
		}},
		// '\r' is content: GLib and mydumper's pre-processor only split on '\n'.
		{"crlf keeps the carriage return", "a\r\n\r\n", []Line{
			{Num: 1, Start: 0, End: 2, HasNewline: true},
			{Num: 2, Start: 3, End: 4, HasNewline: true},
		}},
		{"lone carriage return", "a\rb", []Line{{Num: 1, Start: 0, End: 3, HasNewline: false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New("x.cnf", []byte(tt.in))
			if !reflect.DeepEqual(f.Lines, tt.want) {
				t.Errorf("Lines = %+v, want %+v", f.Lines, tt.want)
			}
		})
	}
}

func TestContentAndCR(t *testing.T) {
	f := New("x.cnf", []byte("[mydumper]\r\nk=v\n"))
	if got := string(f.Content(1)); got != "[mydumper]\r" {
		t.Errorf("Content(1) = %q", got)
	}
	if !f.EndsWithCR(1) || f.EndsWithCR(2) {
		t.Error("EndsWithCR wrong")
	}
	if got := string(f.Content(2)); got != "k=v" {
		t.Errorf("Content(2) = %q", got)
	}
}

func TestLineOf(t *testing.T) {
	f := New("x.cnf", []byte("ab\ncd\n"))
	cases := map[int]int{0: 1, 1: 1, 2: 1, 3: 2, 5: 2, 6: 3}
	for off, want := range cases {
		if got := f.LineOf(off); got != want {
			t.Errorf("LineOf(%d) = %d, want %d", off, got, want)
		}
	}
	g := New("y.cnf", []byte("ab"))
	if got := g.LineOf(2); got != 1 {
		t.Errorf("LineOf(EOF without newline) = %d, want 1", got)
	}
	if got := New("e", nil).LineOf(0); got != 1 {
		t.Errorf("LineOf on empty file = %d, want 1", got)
	}
}

func TestPositionCountsCodePoints(t *testing.T) {
	// "é" is 2 bytes, an invalid byte counts as one column.
	f := New("x.cnf", []byte("a=é\xffz\nnext"))
	tests := []struct {
		off  int
		want Position
	}{
		{0, Position{Line: 1, Col: 1, Offset: 0}},
		{2, Position{Line: 1, Col: 3, Offset: 2}},   // é
		{4, Position{Line: 1, Col: 4, Offset: 4}},   // \xff
		{5, Position{Line: 1, Col: 5, Offset: 5}},   // z
		{6, Position{Line: 1, Col: 6, Offset: 6}},   // the '\n'
		{7, Position{Line: 2, Col: 1, Offset: 7}},   // n
		{11, Position{Line: 2, Col: 5, Offset: 11}}, // EOF, no final newline
	}
	for _, tt := range tests {
		if got := f.Position(tt.off); got != tt.want {
			t.Errorf("Position(%d) = %+v, want %+v", tt.off, got, tt.want)
		}
	}
}

func TestPositionAfterFinalNewline(t *testing.T) {
	f := New("x.cnf", []byte("a\n"))
	if got, want := f.Position(2), (Position{Line: 2, Col: 1, Offset: 2}); got != want {
		t.Errorf("Position(EOF) = %+v, want %+v", got, want)
	}
}

func TestLineSpan(t *testing.T) {
	f := New("x.cnf", []byte("ab\ncd"))
	if s, e := f.LineSpan(1, true); s != 0 || e != 3 {
		t.Errorf("LineSpan(1, with newline) = %d,%d", s, e)
	}
	if s, e := f.LineSpan(2, true); s != 3 || e != 5 {
		t.Errorf("LineSpan(2, with newline) = %d,%d (no newline to include)", s, e)
	}
}
