package main

import (
	"strings"
	"testing"
)

func texts(ts []token) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.text
	}
	return strings.Join(parts, " ")
}

func TestLexTokens(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"idents and puncts", "int a=b->c;", "int a = b -> c ;"},
		{"longest punct", "a<<=b>>=c...d", "a <<= b >>= c ... d"},
		{"block comment is space", "a/* x\ny */b", "a b"},
		{"line comment", "a // b c\nd", "a d"},
		{"comment markers in strings", `"a/*b" "c//d"`, `"a/*b" "c//d"`},
		{"escaped quote", `"a\"b" 'x' '\''`, `"a\"b" 'x' '\''`},
		{"prefixed literals", `L"w" u8"x" U'y'`, `L"w" u8"x" U'y'`},
		{"numbers", "0x1F 1e+5 .5 10ul", "0x1F 1e+5 .5 10ul"},
		{"line splice joins tokens", "ab\\\ncd", "abcd"},
		{"crlf splice", "ab\\\r\ncd", "abcd"},
		{"raw include", "#include <glib/gstdio.h> // c\nx", "# include <glib/gstdio.h> x"},
		{"raw error keeps apostrophe", "#error don't\nx", "# error don't x"},
		{"stray quote in define", "#define A don't\nx", "# define A don 't x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, err := lex([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if got := texts(ts); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLexLinesAndBOL(t *testing.T) {
	src := "a /* one\ntwo */ # b\n  #define X \\\n  1\ny"
	ts, err := lex([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	type tl struct {
		text string
		line int
		bol  bool
	}
	var got []tl
	for _, tk := range ts {
		got = append(got, tl{tk.text, tk.line, tk.bol})
	}
	want := []tl{
		{"a", 1, true},
		{"#", 2, false}, // after a multi-line comment on the same logical line: not a directive
		{"b", 2, false},
		{"#", 3, true},
		{"define", 3, false},
		{"X", 3, false},
		{"1", 4, false}, // spliced line: still the directive's line
		{"y", 5, true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLexErrors(t *testing.T) {
	for _, src := range []string{"/* open", `"open`, "x = 'a", "\"a\nb\""} {
		if _, err := lex([]byte(src)); err == nil {
			t.Errorf("lex(%q): no error", src)
		}
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct{ in, want string }{
		{`"abc"`, "abc"},
		{`"a\tb\n"`, "a\tb\n"},
		{`"\x41\101\0"`, "AA\x00"},
		{`'\''`, "'"},
		{`'?'`, "?"},
		{`L"w"`, "w"},
		{`"\\"`, `\`},
	}
	for _, tt := range tests {
		got, err := unquote(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("unquote(%s) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{`abc`, `"abc`, `"\q"`, `"\x"`, `"a\"`} {
		if _, err := unquote(bad); err == nil {
			t.Errorf("unquote(%s): no error", bad)
		}
	}
}
