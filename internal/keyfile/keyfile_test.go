package keyfile

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

func parse(s string) *Result {
	f := source.New("t.cnf", []byte(s))
	return Parse(f, preprocess.Run(f))
}

// oracleText renders a result exactly like tools/oracle/oracle.c's text mode.
func oracleText(r *Result, languages []string) string {
	if !r.Loadable {
		return "ERROR: " + r.FirstError.Message + "\n"
	}
	var b strings.Builder
	view := r.GLibView(languages)
	fmt.Fprintf(&b, "OK groups=%d\n", len(view))
	for _, g := range view {
		fmt.Fprintf(&b, "  [%s] keys=%d\n", g.Name, len(g.Entries))
		for _, e := range g.Entries {
			fmt.Fprintf(&b, "    <%s>=<%s>\n", e.Key, e.Value)
		}
	}
	return b.String()
}

// TestOracleConformance checks every oracle case against the output recorded
// with real GLib (testdata/oracle-cases/expected.txt).
func TestOracleConformance(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "oracle-cases")
	expected := readExpected(t, filepath.Join(dir, "expected.txt"))
	files, err := filepath.Glob(filepath.Join(dir, "*.cnf"))
	if err != nil || len(files) < 49 {
		t.Fatalf("found %d cases (err %v)", len(files), err)
	}
	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			want, ok := expected[name]
			if !ok {
				t.Fatalf("no expected output for %s", name)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			env := readEnv(t, strings.TrimSuffix(path, ".cnf")+".env")
			f := source.New(name, b)
			got := oracleText(Parse(f, preprocess.Run(f)), LanguageNames(func(k string) string { return env[k] }))
			if got != want {
				t.Errorf("mismatch\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func readExpected(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var name string
	var cur bytes.Buffer
	flush := func() {
		if name != "" {
			out[name] = cur.String()
		}
		cur.Reset()
	}
	for _, line := range bytes.SplitAfter(b, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("### ")) {
			flush()
			name = strings.TrimSpace(string(line[4:]))
			continue
		}
		cur.Write(line)
	}
	flush()
	return out
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	env := map[string]string{}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return env
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok {
			env[k] = v
		}
	}
	return env
}

func TestClassification(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		line  int
		kind  Kind
		cause Cause
		msg   string
	}{
		{"blank", "[g]\n\n", 2, KindBlank, NoCause, ""},
		{"comment", "[g]\n# c\n", 2, KindComment, NoCause, ""},
		{"nul first is a comment", "[g]\n\x00routines=1\n", 2, KindComment, NoCause, ""},
		{"whitespace-only last line is a comment", "[g]\n  ", 2, KindComment, NoCause, ""},
		{"group", "[g]\n", 1, KindGroup, NoCause, ""},
		{"group with trailing spaces", "[g] \t\n", 1, KindGroup, NoCause, ""},
		{"group with stray continuation bytes after bracket", "[g]\x80\x80\n", 1, KindGroup, NoCause, ""},
		{"entry", "[g]\nk=v\n", 2, KindEntry, NoCause, ""},
		{"flag entry", "[g]\nroutines\n", 2, KindEntry, NoCause, ""},
		{"vertical tab is not whitespace", "[g]\n\vk=1\n", 2, KindEntry, NoCause, ""},
		{"semicolon is a key", "[g]\n; c\n", 2, KindEntry, NoCause, ""},
		{"key[] is a plain key", "[g]\nk[]=1\n", 2, KindEntry, NoCause, ""},

		{"bom", "\xef\xbb\xbf[g]\n", 1, KindRejected, CauseBOM, "Key file contains line “\ufeff[g]” which is not a key-value pair, group, or comment"},
		{"whitespace-only", "[g]\n  \n", 2, KindRejected, CauseWhitespaceOnly, "Key file contains line “  = 1” which is not a key-value pair, group, or comment"},
		{"tab-only", "[g]\n\t\n", 2, KindRejected, CauseWhitespaceOnly, "Key file contains line “\t= 1” which is not a key-value pair, group, or comment"},
		{"crlf empty line", "[g]\r\nk=1\r\n\r\n", 3, KindRejected, CauseCarriageReturn, "Key file contains line “\r= 1” which is not a key-value pair, group, or comment"},
		{"leak blank", "[g]\n# see [docs]\n\nk=1\n", 3, KindRejected, CauseBracketLeakBlank, "Key file contains line “= 1” which is not a key-value pair, group, or comment"},
		{"leak flag", "[g]\nregex=^(a[bc])\nroutines\n", 3, KindRejected, CauseBracketLeakFlag, "Key file contains line “routines” which is not a key-value pair, group, or comment"},
		{"flag on last line without newline", "[g]\nroutines", 2, KindRejected, CauseMissingFinalNewline, "Key file contains line “routines” which is not a key-value pair, group, or comment"},
		{"text after header", "[g] # main\n", 1, KindRejected, CauseInvalidGroupLine, "Key file contains line “[g] # main” which is not a key-value pair, group, or comment"},
		{"header then key", "[g]\n[h] x=1\n", 2, KindRejected, CauseInvalidGroupLine, "Invalid key name: [h] x"},
		{"empty group name", "[]\n", 1, KindRejected, CauseInvalidGroupLine, "Invalid group name: "},
		{"bracket in group name", "[a[b]\n", 1, KindRejected, CauseInvalidGroupLine, "Invalid group name: a[b"},
		{"tab in group name", "[a\tb]\n", 1, KindRejected, CauseInvalidGroupLine, "Invalid group name: a\tb"},
		{"del in group name", "[a\x7fb]\n", 1, KindRejected, CauseInvalidGroupLine, "Invalid group name: a\x7fb"},
		{"missing bracket", "[g]\n[\n", 2, KindRejected, CauseInvalidGroupLine, "Key file contains line “[” which is not a key-value pair, group, or comment"},
		{"empty key", "[g]\n=1\n", 2, KindRejected, CauseEmptyKey, "Key file contains line “=1” which is not a key-value pair, group, or comment"},
		{"key before group", "k=1\n", 1, KindRejected, CauseKeyBeforeGroup, "Key file does not start with a group"},
		{"close bracket in key", "[g]\nfoo]=1\n", 2, KindRejected, CauseInvalidKeyName, "Invalid key name: foo]"},
		{"bad locale", "[g]\nfoo[bar baz]=1\n", 2, KindRejected, CauseInvalidKeyName, "Invalid key name: foo[bar baz]"},
		{"space before locale", "[g]\nfoo [fr]=1\n", 2, KindRejected, CauseInvalidKeyName, "Invalid key name: foo [fr]"},
		{"nul in key", "[g]\nrout\x00ines=1\n", 2, KindRejected, CauseNulByte, "Key file contains line “rout\ufffdines=1” which is not a key-value pair, group, or comment"},
		{"nul in header", "[my\x00g]\n", 1, KindRejected, CauseNulByte, "Key file contains line “[my\ufffdg]” which is not a key-value pair, group, or comment"},
		{"invalid utf-8 key is accepted", "[g]\n\xff\n", 2, KindEntry, NoCause, ""},
		{"invalid utf-8 in message becomes U+FFFD", "[g]\n=\xff\xe2\x82\n", 2, KindRejected, CauseEmptyKey, "Key file contains line “=\ufffd\ufffd\ufffd” which is not a key-value pair, group, or comment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parse(tt.in)
			lc := r.Lines[tt.line-1]
			if lc.Kind != tt.kind || lc.Cause != tt.cause || lc.Message != tt.msg {
				t.Errorf("line %d: got kind=%v cause=%v msg=%q; want kind=%v cause=%v msg=%q",
					tt.line, lc.Kind, lc.Cause, lc.Message, tt.kind, tt.cause, tt.msg)
			}
		})
	}
}

func TestFirstErrorIsWhatMydumperSees(t *testing.T) {
	r := parse("[g]\n=1\nfoo]=1\n")
	if r.Loadable || r.FirstError == nil || r.FirstError.Line != 2 {
		t.Fatalf("got %+v", r.FirstError)
	}
	// Classification continues after the first error.
	if r.Lines[2].Cause != CauseInvalidKeyName {
		t.Errorf("line 3 cause = %v", r.Lines[2].Cause)
	}
}

func TestEntryDetails(t *testing.T) {
	r := parse("[g]\n  key \t=  va lue \t\nflag\r\nk[fr]=x\nw=a\x00b\n")
	var got []string
	for _, e := range r.Entries {
		got = append(got, fmt.Sprintf("%q=%q loc=%q synth=%v key=%v val=%v", e.Key, e.Value, e.Locale, e.Synthesized, e.KeySpan, e.ValueSpan))
	}
	want := []string{
		`"key"="va lue \t" loc="" synth=false key={6 9} val={14 22}`,
		`"flag"="1" loc="" synth=true key={23 27} val={27 27}`,
		`"k[fr]"="x" loc="fr" synth=false key={29 34} val={35 36}`,
		`"w"="a" loc="" synth=false key={37 38} val={39 40}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestGLibViewMergesAndShadows(t *testing.T) {
	r := parse("[a]\nx=1\n[b]\ny=2\n[a]\nx=3\nz=4\nt[fr]=9\nt[C]=8\n")
	view := r.GLibView([]string{"C"})
	got := fmt.Sprint(view)
	want := "[{a [{x 3} {x 3} {z 4} {t[C] 8}]} {b [{y 2}]}]"
	if got != want {
		t.Errorf("view = %s, want %s", got, want)
	}
	if v := parse("k=1\n").GLibView([]string{"C"}); v != nil {
		t.Errorf("an unloadable file has no view, got %v", v)
	}
}

func TestLanguageNames(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		env  map[string]string
		want []string
	}{
		{nil, []string{"C", "C"}},
		{map[string]string{"LANG": "fr_FR.UTF-8"}, []string{"fr_FR.UTF-8", "fr_FR", "fr.UTF-8", "fr", "C"}},
		{map[string]string{"LANG": "de_DE@euro", "LC_ALL": "en_US"}, []string{"en_US", "en", "C"}},
		{map[string]string{"LANGUAGE": "pt_BR:fr", "LANG": "C"}, []string{"pt_BR", "pt", "fr", "C"}},
		{map[string]string{"LC_MESSAGES": "sr_RS.UTF-8@latin"}, []string{
			"sr_RS.UTF-8@latin", "sr_RS@latin", "sr.UTF-8@latin", "sr@latin",
			"sr_RS.UTF-8", "sr_RS", "sr.UTF-8", "sr", "C",
		}},
	}
	for _, tt := range tests {
		if got := LanguageNames(env(tt.env)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("LanguageNames(%v) = %v, want %v", tt.env, got, tt.want)
		}
	}
	if !IsInterestingLocale("c", []string{"C"}) || IsInterestingLocale("fr", []string{"fr_FR", "C"}) {
		t.Error("IsInterestingLocale must compare whole names, ASCII case-insensitively")
	}
}

func TestRecover(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"loadable is unchanged", "[g]\nk=v\n", "[g]\nk=v\n"},
		{"bom", "\xef\xbb\xbf[g]\nk=v\n", "[g]\nk=v\n"},
		{"whitespace line", "[g]\n  \nk=v\n", "[g]\n\nk=v\n"},
		{"crlf empty line", "[g]\r\n\r\nk=v\r\n", "[g]\r\n\nk=v\r\n"},
		{"leak blank", "[g]\n# see [docs]\n\nk=v\n", "[g]\n# see [docs]\nk=v\n"},
		{"whitespace then leak (two passes)", "[g]\n# see [docs]\n  \nk=v\n", "[g]\n# see [docs]\nk=v\n"},
		{"leak flag", "[g]\nregex=^(a[bc])\nroutines\n", "[g]\nregex=^(a[bc])\nroutines=1\n"},
		{"leak flag crlf", "[g]\r\nregex=^(a[bc])\r\nroutines\r\n", "[g]\r\nregex=^(a[bc])\r\nroutines=1\r\n"},
		{"final flag", "[g]\nroutines", "[g]\nroutines\n"},
		{"trailing comment on header", "[g] # main\nk=v\n", "[g]\nk=v\n"},
		{"unrecoverable lines are neutralized", "k=1\n[g]\n=x\nfoo]=1\nk=v\n", "#\n[g]\n#\n#\nk=v\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(Recover([]byte(tt.in)))
			if got != tt.want {
				t.Errorf("Recover(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
			if r := parse(got); !r.Loadable {
				t.Errorf("recovered file must load, got %v", r.FirstError)
			}
		})
	}
}

func FuzzRecoverAlwaysLoads(f *testing.F) {
	for _, s := range []string{"[g]\n  \n", "\xef\xbb\xbf[a]\n\n", "# [x]\n\n[y] z\n=1\n", "k\n[g]\nr\r\n\r\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		out := Recover(b)
		if r := parse(string(out)); !r.Loadable {
			t.Fatalf("Recover(%q) = %q does not load: %+v", b, out, r.FirstError)
		}
		// Recovering twice changes nothing.
		if again := Recover(out); !bytes.Equal(again, out) {
			t.Fatalf("Recover is not idempotent on %q: %q then %q", b, out, again)
		}
	})
}

func TestKindAndCauseStrings(t *testing.T) {
	var names []string
	for c := NoCause; c <= CauseUnknown; c++ {
		names = append(names, c.String())
	}
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Errorf("duplicate cause name %q", names[i])
		}
	}
	for k := KindBlank; k <= KindRejected; k++ {
		if strings.HasPrefix(k.String(), "Kind(") {
			t.Errorf("kind %d has no name", k)
		}
	}
}
