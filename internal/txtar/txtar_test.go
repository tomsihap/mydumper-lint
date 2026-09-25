package txtar

import (
	"bytes"
	"testing"
)

func TestParseFormatRoundTrip(t *testing.T) {
	in := []byte("comment line\n-- a.cnf --\n[mydumper]\n-- b.esc --\n[g]\\n  \n-- empty --\n")
	a := Parse(in)
	if string(a.Comment) != "comment line\n" || len(a.Files) != 3 {
		t.Fatalf("parsed %+v", a)
	}
	if got := Format(a); !bytes.Equal(got, in) {
		t.Errorf("round trip:\n%q\n%q", got, in)
	}
	b, ok, err := a.Get("b")
	if !ok || err != nil || string(b) != "[g]\n  " {
		t.Errorf("Get(b) = %q, %v, %v", b, ok, err)
	}
	if _, ok, _ := a.Get("missing"); ok {
		t.Error("Get(missing) must fail")
	}
}

func TestSetChoosesEscapeWhenNeeded(t *testing.T) {
	a := &Archive{}
	a.Set("plain.cnf", []byte("[g]\nk=v\n"))
	a.Set("crlf.cnf", []byte("[g]\r\n"))
	a.Set("plain.cnf", []byte("[g]\nk=2\n")) // replace
	if len(a.Files) != 2 || a.Files[0].Name != "plain.cnf" || a.Files[1].Name != "crlf.cnf.esc" {
		t.Fatalf("files %+v", a.Files)
	}
	a.Set("crlf.cnf", []byte("[g]\n")) // now plain: replaces the .esc version
	if a.Files[1].Name != "crlf.cnf" {
		t.Errorf("got %q", a.Files[1].Name)
	}
	a.Delete("plain.cnf")
	if len(a.Files) != 1 {
		t.Errorf("Delete left %+v", a.Files)
	}
}

func TestNeedsEscape(t *testing.T) {
	cases := map[string]bool{
		"a\n":                false,
		"a\tb\n":             false,
		"":                   true,
		"a":                  true,
		"a \n":               true,
		"a\t\n":              true,
		"a\r\n":              true,
		"\x00\n":             true,
		"\xef\xbb\xbfa\n":    true,
		"\xff\n":             true,
		"-- looks like --\n": true,
		"é\n":                false,
	}
	for in, want := range cases {
		if got := NeedsEscape([]byte(in)); got != want {
			t.Errorf("NeedsEscape(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEscapeRoundTrip(t *testing.T) {
	for _, s := range []string{"", "[g]\r\n  ", "\x00\xff\xef\xbb\xbf\v\"\\", "é"} {
		got, err := Unescape([]byte(Escape([]byte(s)) + "\n"))
		if err != nil || string(got) != s {
			t.Errorf("round trip of %q gave %q, %v", s, got, err)
		}
	}
	if _, err := Unescape([]byte(`\q`)); err == nil {
		t.Error("invalid escape must fail")
	}
}
