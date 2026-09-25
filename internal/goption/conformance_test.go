package goption

import (
	"bufio"
	"encoding/json"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const casesDir = "../../testdata/goption-cases"

// TestConformance runs the recorded GOption cases (design Appendix B and
// beyond) and compares the emulation, byte for byte, with what GLib printed
// (testdata/goption-cases/expected.jsonl, produced by the oracle).
func TestConformance(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(casesDir, "*.cases"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no case files (%v)", err)
	}
	sort.Strings(files)
	want := map[string]string{}
	f, err := os.Open(filepath.Join(casesDir, "expected.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var head struct{ Name string }
		if err := json.Unmarshal(sc.Bytes(), &head); err != nil {
			t.Fatal(err)
		}
		want[decodeBytes(head.Name)] = sc.Text()
	}
	n := 0
	for _, path := range files {
		cases, err := parseCaseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			n++
			exp, ok := want[c.name]
			if !ok {
				t.Errorf("%s: no expected result", c.name)
				continue
			}
			if got := c.renderJSON(c.ctx.Parse(c.argv, c.init)); got != exp {
				t.Errorf("%s:\n got  %s\n want %s", c.name, got, exp)
			}
		}
	}
	if n != len(want) {
		t.Errorf("%d cases for %d expected results", n, len(want))
	}
}

// decodeBytes maps each code point of an oracle string back to one byte.
func decodeBytes(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r)) // the oracle only writes U+0000-U+00FF
	}
	return string(b)
}

// TestOracleRandom compares the emulation with real GLib on random option
// tables and vectors, when MYDUMPER_LINT_ORACLE names an oracle (see
// tools/oracle/README.md). MYDUMPER_LINT_GOPTION_CASES sets the number of
// cases (default 3000). The UTF-8 run uses the locale named by
// MYDUMPER_LINT_UTF8_LOCALE (default C.UTF-8; en_US.UTF-8 on macOS).
func TestOracleRandom(t *testing.T) {
	spec := strings.Fields(os.Getenv("MYDUMPER_LINT_ORACLE"))
	if len(spec) == 0 {
		t.Skip("MYDUMPER_LINT_ORACLE is not set; see tools/oracle/README.md")
	}
	count := 3000
	if s := os.Getenv("MYDUMPER_LINT_GOPTION_CASES"); s != "" {
		var err error
		if count, err = strconv.Atoi(s); err != nil {
			t.Fatal(err)
		}
	}
	utf8Locale := os.Getenv("MYDUMPER_LINT_UTF8_LOCALE")
	if utf8Locale == "" {
		utf8Locale = "C.UTF-8"
	}
	for _, run := range []struct {
		name    string
		charset Charset
		env     []string
	}{
		{"C", CharsetASCII, nil},
		{utf8Locale, CharsetUTF8, []string{"--setenv", "LC_ALL=" + utf8Locale}},
	} {
		t.Run(run.name, func(t *testing.T) {
			rng := rand.New(rand.NewPCG(7, uint64(run.charset)))
			cases := make([]*gcase, count)
			var in strings.Builder
			for i := range cases {
				cases[i] = randomCase(rng, i)
				cases[i].ctx.Charset = run.charset
				in.WriteString(cases[i].format())
			}
			var args []string
			for _, v := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG", "LC_CTYPE", "CHARSET"} {
				args = append(args, "--unsetenv", v)
			}
			args = append(append(args, run.env...), "--goption-cases", "-")
			cmd := exec.Command(spec[0], append(spec[1:], args...)...)
			cmd.Stdin = strings.NewReader(in.String())
			cmd.Stderr = os.Stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("oracle: %v", err)
			}
			lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
			if len(lines) != len(cases) {
				t.Fatalf("%d results for %d cases", len(lines), len(cases))
			}
			if run.charset == CharsetUTF8 && !strings.Contains(lines[0], `"ok":`) {
				t.Skipf("locale %s unavailable", utf8Locale)
			}
			fails := 0
			for i, c := range cases {
				if got := c.renderJSON(c.ctx.Parse(c.argv, c.init)); got != lines[i] {
					fails++
					if fails <= 5 {
						t.Errorf("case %d:\n%s got  %s\n want %s", i, c.format(), got, lines[i])
					}
				}
			}
			if fails > 0 {
				t.Errorf("%d of %d cases differ", fails, len(cases))
			}
		})
	}
}

// Pools for randomCase: names, short letters, values that reach the corner
// cases of design §3.4 (G1-G15).
var (
	longPool  = []string{"threads", "routines", "outputdir", "compress", "nodata", "snapshot-count", "where", "rows", "t-x", "no-data", "a", "ab", "daemon"}
	shortPool = []byte("txcoDXa")
	groupPool = []string{"daemongroup", "conn", "filter", "extra", "d"}
	valuePool = []string{
		"", "0", "1", "10", "010", "08", "0x10", "0X1f", "0x", "0xg", "+5", "-1", " 5", "\v5", "\t7", "5 ", "1e3",
		"99999999999", "2147483647", "2147483648", "-2147483648", "-2147483649", "9223372036854775807",
		"9223372036854775808", "-9223372036854775808", "-9223372036854775809", "1.5", "-2.5e3", "0x1p3", "inf",
		"-infinity", "nan", "1e400", "1e-400", ".5", "5.", "ZSTD", "gzip", "false", "--", "-", "-t", "-x", "-tx",
		"-xt", "-ta", "--threads", "--threads=4", "--routines=0", "--compress", "--nope", "--daemon-snapshot-count",
		"--d-snapshot-count", "--conn-where", "--main-threads", "caf\xc3\xa9", "\xff", "\xef\xbf\xbe", "\xed\xa0\x80",
		"a=b", "x y", "=",
	}
	argTypes = []Arg{ArgNone, ArgNone, ArgString, ArgFilename, ArgInt, ArgInt64, ArgDouble, ArgCallback, ArgCallback}
)

// randomCase builds a valid option table (unique names, flags allowed for
// their type) and a vector mixing mydumper-style "--key value" pairs with
// raw elements.
func randomCase(rng *rand.Rand, i int) *gcase {
	c := &gcase{name: "r" + strconv.Itoa(i), init: map[Ref]Value{}, ctx: Context{Main: &Group{Name: "main"}}}
	c.strict = rng.IntN(2) == 0
	c.ctx.IgnoreUnknown = !c.strict
	names := append([]string(nil), longPool...)
	rng.Shuffle(len(names), func(a, b int) { names[a], names[b] = names[b], names[a] })
	shorts := append([]byte(nil), shortPool...)
	rng.Shuffle(len(shorts), func(a, b int) { shorts[a], shorts[b] = shorts[b], shorts[a] })
	groups := append([]string(nil), groupPool...)
	rng.Shuffle(len(groups), func(a, b int) { groups[a], groups[b] = groups[b], groups[a] })
	nGroups := rng.IntN(3)
	for g := range nGroups {
		c.ctx.Groups = append(c.ctx.Groups, &Group{Name: groups[g]})
	}
	var all []string
	for k := range 2 + rng.IntN(6) {
		g := rng.IntN(nGroups+1) - 1
		e := Entry{Long: names[k], Arg: argTypes[rng.IntN(len(argTypes))]}
		if rng.IntN(2) == 0 && len(shorts) > 0 {
			e.Short, shorts = shorts[0], shorts[1:]
		}
		switch {
		case e.Arg == ArgNone && rng.IntN(4) == 0:
			e.Flags |= FlagReverse
		case e.Arg == ArgCallback && rng.IntN(2) == 0:
			e.Flags |= FlagOptionalArg
		case e.Arg == ArgCallback && rng.IntN(4) == 0:
			e.Flags |= FlagNoArg
		}
		grp := c.ctx.Main
		if g >= 0 {
			grp = c.ctx.Groups[g]
		}
		grp.Entries = append(grp.Entries, e)
		r := Ref{g, len(grp.Entries) - 1}
		c.order = append(c.order, r)
		if rng.IntN(3) == 0 {
			switch e.Arg {
			case ArgNone:
				c.init[r] = Value{Bool: true}
			case ArgInt, ArgInt64:
				c.init[r] = Value{Int: int64(rng.IntN(5)) - 1}
			case ArgString, ArgFilename:
				s := "init"
				c.init[r] = Value{Str: &s}
			case ArgDouble, ArgCallback, ArgStringArray, ArgFilenameArray:
			}
		}
		all = append(all, e.Long)
	}
	// Values are reported in file order: main entries, then each group's.
	c.order = c.order[:0]
	for e := range c.ctx.Main.Entries {
		c.order = append(c.order, Ref{-1, e})
	}
	for g, grp := range c.ctx.Groups {
		for e := range grp.Entries {
			c.order = append(c.order, Ref{g, e})
		}
	}
	c.argv = []string{"mydumper"}
	for range rng.IntN(5) {
		if rng.IntN(3) > 0 {
			key := all[rng.IntN(len(all))]
			if rng.IntN(6) == 0 {
				key = names[len(names)-1] // likely unknown
			}
			c.argv = append(c.argv, "--"+key, valuePool[rng.IntN(len(valuePool))])
		} else {
			c.argv = append(c.argv, valuePool[rng.IntN(len(valuePool))])
		}
	}
	return c
}
