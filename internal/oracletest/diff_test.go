package oracletest

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// emulate is what the Go emulation says about content, in the oracle's shape.
func emulate(content []byte) Verdict {
	f := source.New("fuzz.cnf", content)
	r := keyfile.Parse(f, preprocess.Run(f))
	if !r.Loadable {
		return Verdict{Error: r.FirstError.Message}
	}
	v := Verdict{Loadable: true}
	for _, g := range r.GLibView([]string{"C"}) {
		vg := Group{Name: g.Name}
		for _, e := range g.Entries {
			vg.Entries = append(vg.Entries, Entry{Key: e.Key, Value: e.Value})
		}
		v.Groups = append(v.Groups, vg)
	}
	return v
}

func render(v Verdict) string {
	if !v.Loadable {
		return "ERROR: " + v.Error
	}
	var b strings.Builder
	fmt.Fprintf(&b, "OK groups=%d\n", len(v.Groups))
	for _, g := range v.Groups {
		fmt.Fprintf(&b, "[%q]\n", g.Name)
		for _, e := range g.Entries {
			fmt.Fprintf(&b, "  %q=%q\n", e.Key, e.Value)
		}
	}
	return b.String()
}

func compare(t *testing.T, o *Oracle, content []byte) {
	t.Helper()
	want, err := o.Load(content)
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if got := emulate(content); render(got) != render(want) {
		t.Fatalf("divergence on %q (GLib %s)\nemulation:\n%s\nGLib:\n%s", content, want.GLib, render(got), render(want))
	}
}

// tokens are line fragments that reach the interesting states of the
// pre-processor and of GKeyFile (design §3).
var tokens = []string{
	"[mydumper]", "[myloader]", "[ a ]", "[x] # c", "[x]  ", "[x]\t", "[", "[]", "[a[b]", "[a]b]", "[`db`.`t`]",
	"# c", "# see [x]", "#", "#x=1", "routines", "k=v", "k = v ", "  k=v", "\tk=v", "=1", " =1", "; c", "; [x]",
	"k[fr]=1", "k[C]=1", "k[c]=1", "k[]=1", "k[fr_FR.UTF-8@euro]=1", "k[é]=1", "foo]=1", "foo[b c]=1", "a [fr]=1",
	"  ", "\t", "", "\r", " \r", "\r\r", "\x00", "\x00k=1", "k\x00=1", "k=a\x00b", "[a\x00]", "\xef\xbb\xbf[g]",
	"\v", "\vk=1", "\f", "\fk=1", "regex=^(a[bc])", "x=[", "a=b[c", "\xff", "k=\xff", "\xc3=1", "[\x80]", "[g]\x80",
	"[g]\x80 ", "`c`=constant x", "`c[0]`=x", "k=v # c", "==", "[a=b]", "[\x7f]", "[a\tb]", "  [mydumper]", "[a] x=1",
	"k=\"q\"", "k='q'", "\xe2\x82", "k=\xe2\x82",
}

// build turns fuzz bytes into a file made of tokens and raw bytes.
func build(seed []byte) []byte {
	var b []byte
	for i := 0; i < len(seed); i++ {
		c := seed[i]
		switch {
		case c < 0xb0:
			b = append(b, tokens[int(c)%len(tokens)]...)
		case c < 0xd0:
			b = append(b, '\n')
		case c < 0xe0:
			b = append(b, "\r\n"...)
		case i+1 < len(seed):
			i++
			b = append(b, seed[i]) // a raw byte
		}
	}
	return b
}

func startOrSkip(t testing.TB) *Oracle {
	o, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	if o == nil {
		t.Skip(EnvVar + " is not set; see tools/oracle/README.md")
	}
	t.Cleanup(func() { o.Close() })
	return o
}

// FuzzStructured feeds grammar-built files to both implementations.
func FuzzStructured(f *testing.F) {
	for _, s := range []string{"\x00\xb0\x0e\xb0", "\x2f\xb0\xc0\x10", "\x05\x05\xd0\x22", "\x40\xb5\x41"} {
		f.Add([]byte(s))
	}
	o := startOrSkip(f)
	f.Fuzz(func(t *testing.T, seed []byte) {
		compare(t, o, build(seed))
	})
}

// FuzzBytes feeds raw bytes to both implementations.
func FuzzBytes(f *testing.F) {
	for _, s := range []string{"[mydumper]\nroutines\n", "[g]\n  \n", "# [x]\n\n", "\xef\xbb\xbf[a]\n", "[g]\r\n\r\n"} {
		f.Add([]byte(s))
	}
	o := startOrSkip(f)
	f.Fuzz(func(t *testing.T, content []byte) {
		compare(t, o, content)
	})
}

// TestKnownCases runs a fixed corpus through both implementations, so a
// plain `go test` exercises the harness when an oracle is available.
func TestKnownCases(t *testing.T) {
	o := startOrSkip(t)
	rng := rand.New(rand.NewPCG(1, 2))
	for range 3000 {
		seed := make([]byte, 1+rng.IntN(24))
		for i := range seed {
			seed[i] = byte(rng.UintN(256))
		}
		compare(t, o, build(seed))
	}
}
