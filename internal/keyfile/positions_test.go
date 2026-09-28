package keyfile

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// TestPositionsAndIndexes pins what the rules rely on beyond GLib's view:
// group name spans, the offset of '=', and the indexes each line carries.
func TestPositionsAndIndexes(t *testing.T) {
	src := "# c\n[g1]\n  k = v\nflag\n  [ g2 ]  \nx=1\n"
	r := parse(src)
	at := func(s string) int { return strings.Index(src, s) }
	wantGroups := []Group{
		{Name: "g1", Line: 2, Valid: true, NameSpan: diag.Span{Start: at("g1"), End: at("g1") + 2}},
		{Name: " g2 ", Line: 5, Valid: true, NameSpan: diag.Span{Start: at(" g2 "), End: at(" g2 ") + 4}},
	}
	if !reflect.DeepEqual(r.Groups, wantGroups) {
		t.Errorf("groups %+v\nwant %+v", r.Groups, wantGroups)
	}
	var eqs []int
	for _, e := range r.Entries {
		eqs = append(eqs, e.EqOffset)
	}
	if want := []int{at("="), -1, strings.LastIndex(src, "=")}; !reflect.DeepEqual(eqs, want) {
		t.Errorf("EqOffset = %v, want %v", eqs, want)
	}
	var lines []string
	for _, lc := range r.Lines {
		lines = append(lines, fmt.Sprintf("%s g%d h%d e%d", lc.Kind, lc.Group, lc.Header, lc.Entry))
	}
	want := []string{"comment g-1 h-1 e-1", "group g0 h0 e-1", "entry g0 h-1 e0", "entry g0 h-1 e1", "group g1 h1 e-1", "entry g1 h-1 e2"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines %v\nwant  %v", lines, want)
	}
}

// Rejected header-like lines get a placeholder group (never valid), named as
// the author meant, so that the lines below are attributed to it.
func TestRejectedHeaderPlaceholders(t *testing.T) {
	tests := []struct {
		in, name string
		start    int
	}{
		{"[]x\n", "", 1},
		{"[\x00a]\n", "", 1},
		{"  [g] x\n", "g", 3},
		{"\xef\xbb\xbf[g]\n", "g", 4},
	}
	for _, tt := range tests {
		r := parse(tt.in)
		if r.Loadable || len(r.Groups) != 1 {
			t.Errorf("%q: loadable %v, groups %+v", tt.in, r.Loadable, r.Groups)
			continue
		}
		g := r.Groups[0]
		if g.Valid || g.Name != tt.name || g.NameSpan != (diag.Span{Start: tt.start, End: tt.start + len(tt.name)}) || r.Lines[0].Header != 0 {
			t.Errorf("%q: placeholder %+v, header %d", tt.in, g, r.Lines[0].Header)
		}
	}
}

func TestValueAndKeyEdges(t *testing.T) {
	// K17: a NUL right after the '=' leaves an empty value.
	if r := parse("[g]\nk=\x00x\n"); !r.Loadable || r.Entries[0].Value != "" {
		t.Errorf("NUL-led value: %+v", r)
	}
	// K19: an unterminated locale suffix is an invalid key name.
	r := parse("[g]\nk[abc=1\n")
	if r.Loadable || r.FirstError.Message != "Invalid key name: k[abc" || r.Lines[1].Cause != CauseInvalidKeyName {
		t.Errorf("unterminated locale: %+v %+v", r.FirstError, r.Lines[1])
	}
	// A '[' copy that reaches the end of the file reads the buffer's NUL
	// terminator, which GLib's message shows as U+FFFD (checked with the
	// oracle on GLib 2.68 and 2.90).
	r = parse("[g]\n[abc")
	if want := "Key file contains line “[abc\uFFFD” which is not a key-value pair, group, or comment"; r.FirstError.Message != want {
		t.Errorf("'[' line at EOF: %q", r.FirstError.Message)
	}
	if got := (CauseUnknown + 1).String(); !strings.HasPrefix(got, "Cause(") {
		t.Errorf("out-of-range cause = %q", got)
	}
	if got := (KindRejected + 1).String(); !strings.HasPrefix(got, "Kind(") {
		t.Errorf("out-of-range kind = %q", got)
	}
}

func TestLocaleEdges(t *testing.T) {
	for _, p := range [][2]string{{"A", "a"}, {"a", "A"}, {"Z", "z"}, {"z", "Z"}, {"fr_fr", "FR_FR"}} {
		if !IsInterestingLocale(p[0], []string{p[1]}) {
			t.Errorf("%s must match %s", p[0], p[1])
		}
	}
	for _, p := range [][2]string{{"D", "C"}, {"@", "`"}, {"[", "{"}} {
		if IsInterestingLocale(p[0], []string{p[1]}) {
			t.Errorf("%s must not match %s", p[0], p[1])
		}
	}
	// GLib's explode_locale when a component starts the name.
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "LANG" {
				return v
			}
			return ""
		}
	}
	tests := map[string][]string{
		"@euro":    {"@euro", "", "C"},
		".UTF-8":   {".UTF-8", "", "C"},
		"_FR":      {"_FR", "", "C"},
		"_FR.x@m":  {"_FR.x@m", "_FR@m", ".x@m", "@m", "_FR.x", "_FR", ".x", "", "C"},
		"fr_.UTF8": {"fr_.UTF8", "fr_", "fr.UTF8", "fr", "C"},
	}
	for lang, want := range tests {
		if got := LanguageNames(env(lang)); !reflect.DeepEqual(got, want) {
			t.Errorf("LANG=%s: %q, want %q", lang, got, want)
		}
	}
}

// recoveryEdit removes a whole run of empty lines after a bracket leak in one
// edit, up to the end of the file.
func TestRecoveryEditLeakRun(t *testing.T) {
	for in, n := range map[string]int{"[g]\n# see [docs]\n\n\n": 2, "[g]\n# see [docs]\n\n\nk=v\n": 2, "[g]\n# see [docs]\n\nk=v\n": 1} {
		f := source.New("t.cnf", []byte(in))
		start := strings.Index(in, "\n\n") + 1
		e := recoveryEdit(f, f.Line(3), CauseBracketLeakBlank, false)
		if e.Start != start || e.End != start+n || e.New != "" {
			t.Errorf("%q: edit %+v, want [%d, %d)", in, e, start, start+n)
		}
	}
	// An indented header followed by a comment keeps the header.
	if got := string(Recover([]byte("  [g] # main\nk=v\n"))); got != "  [g]\nk=v\n" {
		t.Errorf("indented header: %q", got)
	}
}
