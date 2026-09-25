package goption

import (
	"slices"
	"strings"
	"testing"
)

// mydumperLike is a small context shaped like mydumper's: a main group, an
// added group, a reverse flag and an optional-argument callback.
func mydumperLike(ignoreUnknown bool, cs Charset) *Context {
	return &Context{
		Main: &Group{Name: "main", Entries: []Entry{
			{Long: "threads", Short: 't', Arg: ArgInt},
			{Long: "routines", Short: 'R', Arg: ArgNone},
			{Long: "outputdir", Short: 'o', Arg: ArgFilename},
			{Long: "compress", Short: 'c', Arg: ArgCallback, Flags: FlagOptionalArg},
			{Long: "where", Arg: ArgString},
			{Long: "tz-utc", Arg: ArgNone, Flags: FlagReverse},
		}},
		Groups: []*Group{{Name: "daemongroup", Entries: []Entry{
			{Long: "daemon", Short: 'D', Arg: ArgNone},
			{Long: "snapshot-count", Short: 'X', Arg: ArgInt},
		}}},
		IgnoreUnknown: ignoreUnknown,
		Charset:       cs,
	}
}

var (
	threads  = Ref{-1, 0}
	routines = Ref{-1, 1}
	outdir   = Ref{-1, 2}
	tzUTC    = Ref{-1, 5}
	snapshot = Ref{0, 1}
)

func uses(r Result) string {
	var s []string
	for _, u := range r.Uses[1:] {
		s = append(s, u.String())
	}
	return strings.Join(s, " ")
}

func TestParseFacts(t *testing.T) {
	tests := []struct {
		name    string
		lenient bool
		argv    []string
		ok      bool
		err     string
		errAt   int
		uses    string
		check   func(t *testing.T, r Result)
	}{
		{
			name: "G1 a flag ignores its value", argv: []string{"--routines", "0"}, ok: true, errAt: -1,
			uses: "option leftover",
			check: func(t *testing.T, r Result) {
				if !r.Values[routines].Bool {
					t.Error("routines=0 must enable routines")
				}
			},
		},
		{
			name: "G12 a reverse flag is disabled by any value", argv: []string{"--tz-utc", "1"}, ok: true, errAt: -1,
			uses: "option leftover",
			check: func(t *testing.T, r Result) {
				if r.Values[tzUTC].Bool {
					t.Error("tz-utc=1 must disable the option (reverse)")
				}
			},
		},
		{
			name: "G7 octal integers", argv: []string{"--threads", "010"}, ok: true, errAt: -1, uses: "option value",
			check: func(t *testing.T, r Result) {
				if r.Values[threads].Int != 8 {
					t.Errorf("threads = %d, want 8", r.Values[threads].Int)
				}
			},
		},
		{
			name: "G5 trailing space in an integer", argv: []string{"--threads", "10 "}, err: "Cannot parse integer value “10 ” for --threads",
			errAt: 1, uses: "option value",
		},
		{
			name: "G7 out of range", argv: []string{"--threads", "99999999999"}, err: "Integer value “99999999999” for --threads out of range",
			errAt: 1, uses: "option value",
		},
		{
			name: "G8 a value parsed as a short option swallows the next key", argv: []string{"--routines", "-t", "--outputdir", "/x"},
			err: "Cannot parse integer value “--outputdir” for -t", errAt: 2, uses: "option option value not-reached",
		},
		{
			name: "G8 a value parsed as a long option", argv: []string{"--routines", "--threads=4", "--where", "a"}, ok: true, errAt: -1,
			uses: "option option option value",
			check: func(t *testing.T, r Result) {
				if r.Values[threads].Int != 4 {
					t.Errorf("threads = %d, want 4", r.Values[threads].Int)
				}
			},
		},
		{
			name: "G9 a -- value ends option parsing", argv: []string{"--routines", "--", "--threads", "4"}, ok: true, errAt: -1,
			uses: "option separator after-separator after-separator",
			check: func(t *testing.T, r Result) {
				if r.Values[threads].Int != 0 {
					t.Error("threads after -- must not be parsed")
				}
				if !slices.Equal(r.Leftover, []string{"g", "--", "--threads", "4"}) {
					t.Errorf("leftover = %q", r.Leftover)
				}
			},
		},
		{
			name: "G10 a required value is taken verbatim", argv: []string{"--outputdir", "-x"}, ok: true, errAt: -1, uses: "option value",
			check: func(t *testing.T, r Result) {
				if v := r.Values[outdir].Str; v == nil || *v != "-x" {
					t.Errorf("outputdir = %v", v)
				}
			},
		},
		{
			name: "G11 a repeated option is applied twice", argv: []string{"--threads", "4", "--threads", "6"}, ok: true, errAt: -1,
			uses: "option value option value",
			check: func(t *testing.T, r Result) {
				if len(r.Applied) != 2 || r.Values[threads].Int != 6 {
					t.Errorf("applied %d times, threads = %d", len(r.Applied), r.Values[threads].Int)
				}
			},
		},
		{
			name: "G13 unknown option, strict", argv: []string{"--threads", "4", "--bogus", "1"}, err: "Unknown option --bogus",
			errAt: 3, uses: "option value unknown not-reached",
		},
		{
			name: "G13 unknown option, lenient", lenient: true, argv: []string{"--threads", "4", "--bogus", "1"}, ok: true, errAt: -1,
			uses: "option value unknown leftover",
		},
		{
			name: "group alias", argv: []string{"--daemon-snapshot-count", "5"}, ok: true, errAt: -1, uses: "option value",
			check: func(t *testing.T, r Result) {
				if r.Values[snapshot].Int != 5 || !r.Applied[0].Alias {
					t.Errorf("snapshot-count = %d, alias %v", r.Values[snapshot].Int, r.Applied[0].Alias)
				}
			},
		},
		{
			name: "group prefix alias", argv: []string{"--d-snapshot-count", "5"}, ok: true, errAt: -1, uses: "option value",
		},
		{
			name: "no alias for the main group", argv: []string{"--main-threads", "4"}, err: "Unknown option --main-threads",
			errAt: 1, uses: "unknown not-reached",
		},
		{
			name: "optional value that looks like an option", lenient: true, argv: []string{"--compress", "-x"}, ok: true, errAt: -1,
			uses: "option unknown",
			check: func(t *testing.T, r Result) {
				if len(r.Calls) != 1 || r.Calls[0].Value != nil {
					t.Errorf("compress must be called without a value: %+v", r.Calls)
				}
				if !slices.Equal(r.Leftover, []string{"g", "-x"}) {
					t.Errorf("leftover = %q", r.Leftover)
				}
			},
		},
		{
			name: "missing argument at the end", argv: []string{"--routines", "--threads"}, err: "Missing argument for --threads",
			errAt: 2, uses: "option option",
		},
		{
			name: "non-ASCII string in the C locale", argv: []string{"--where", "caf\xc3\xa9"}, err: "Invalid byte sequence in conversion input",
			errAt: 1, uses: "option value",
		},
		{
			name: "non-ASCII filename in the C locale", argv: []string{"--outputdir", "/caf\xc3\xa9"}, ok: true, errAt: -1, uses: "option value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mydumperLike(tt.lenient, CharsetASCII).Parse(append([]string{"g"}, tt.argv...), nil)
			if r.OK != tt.ok || r.Error != tt.err || r.ErrorAt != tt.errAt {
				t.Errorf("ok=%v err=%q at %d; want ok=%v err=%q at %d", r.OK, r.Error, r.ErrorAt, tt.ok, tt.err, tt.errAt)
			}
			if got := uses(r); got != tt.uses {
				t.Errorf("uses = %q, want %q", got, tt.uses)
			}
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}

func TestCharsets(t *testing.T) {
	argv := []string{"g", "--where", "caf\xc3\xa9", "--compress", "\xff"}
	for cs, want := range map[Charset]string{
		CharsetASCII: "Invalid byte sequence in conversion input",
		CharsetUTF8:  "", // café is valid UTF-8; the optional value \xff leaks an error but parsing succeeds
		CharsetOther: "",
	} {
		r := mydumperLike(false, cs).Parse(argv, nil)
		if cs == CharsetUTF8 {
			if !r.OK || r.Error != "Invalid byte sequence in conversion input" || r.Calls[0].Value != nil {
				t.Errorf("UTF-8: ok=%v err=%q calls=%+v", r.OK, r.Error, r.Calls)
			}
			continue
		}
		if r.Error != want || r.OK != (want == "") {
			t.Errorf("charset %d: ok=%v err=%q, want %q", cs, r.OK, r.Error, want)
		}
	}
}

func TestFailureRevertsLikeGLib(t *testing.T) {
	init := map[Ref]Value{routines: {Bool: true}, threads: {Int: 2}}
	r := mydumperLike(false, CharsetASCII).Parse([]string{"g", "--routines", "--threads", "3", "--threads", "4", "--bogus"}, init)
	if r.OK {
		t.Fatal("want a failure")
	}
	// A flag's previous value is never saved: it becomes false. A number
	// gets the value it had before its last assignment.
	if r.Values[routines].Bool || r.Values[threads].Int != 3 {
		t.Errorf("routines=%v threads=%d, want false and 3", r.Values[routines].Bool, r.Values[threads].Int)
	}
	if !slices.Equal(r.Leftover, []string{"g", "--routines", "--threads", "3", "--threads", "4", "--bogus"}) {
		t.Errorf("leftover must be unchanged on failure: %q", r.Leftover)
	}
}

func TestParseArgAndFlagNames(t *testing.T) {
	for _, name := range argNames {
		if a, ok := ParseArg(name); !ok || a.String() != name {
			t.Errorf("ParseArg(%q) = %v, %v", name, a, ok)
		}
	}
	if _, ok := ParseArg("bogus"); ok {
		t.Error("ParseArg accepted an unknown type")
	}
	for name, f := range flagNames {
		if got, ok := ParseFlag(name); !ok || got != f {
			t.Errorf("ParseFlag(%q) = %v, %v", name, got, ok)
		}
	}
	if Arg(99).String() != "Arg(?)" || Use(99).String() != "Use(?)" {
		t.Error("out-of-range names")
	}
}

func TestNumbers(t *testing.T) {
	ints := []struct {
		s    string
		want int64
	}{{"0", 0}, {"010", 8}, {"0x1F", 31}, {"+5", 5}, {"-1", -1}, {" \v\t5", 5}, {"-9223372036854775808", -9223372036854775808}}
	for _, tt := range ints {
		s, want := tt.s, tt.want
		if v, msg := parseInt(s, "--n", 64); msg != "" || v != want {
			t.Errorf("parseInt(%q) = %d, %q; want %d", s, v, msg, want)
		}
	}
	for _, s := range []string{"", "08", "0x", "0xg", "1e3", "5 ", "-", "+", " ", "9223372036854775808"} {
		if _, msg := parseInt(s, "--n", 64); msg == "" {
			t.Errorf("parseInt(%q) must fail", s)
		}
	}
	if _, msg := parseInt("2147483648", "--n", 32); !strings.Contains(msg, "out of range") {
		t.Errorf("2147483648 must be out of range for an int: %q", msg)
	}
	doubles := map[string]float64{"1.5": 1.5, "-2.5e3": -2500, "0x1p3": 8, ".5": 0.5, "5.": 5, "  7": 7}
	for s, want := range doubles {
		if v, msg := parseDouble(s, "--d"); msg != "" || v != want {
			t.Errorf("parseDouble(%q) = %v, %q; want %v", s, v, msg, want)
		}
	}
	for _, s := range []string{"", "1e400", "1e-400", "abc", "1.5x", "0x"} {
		if _, msg := parseDouble(s, "--d"); msg == "" {
			t.Errorf("parseDouble(%q) must fail", s)
		}
	}
	for _, s := range []string{"inf", "-Infinity", "nan", "nan(123)"} {
		if _, msg := parseDouble(s, "--d"); msg != "" {
			t.Errorf("parseDouble(%q): %s", s, msg)
		}
	}
}

// FuzzParse: any vector parses without panicking, with one use per element,
// and a successful parse only removes or shortens elements.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"--routines\x000", "--threads\x00-t\x00--where\x00x", "-tX\x0010\x0020", "--\x00--threads\x004", "-\x00--compress\x00-x"} {
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, s string, lenient bool) {
		argv := append([]string{"g"}, strings.Split(s, "\x00")...)
		r := mydumperLike(lenient, CharsetUTF8).Parse(argv, nil)
		if len(r.Uses) != len(argv) {
			t.Fatalf("%d uses for %d elements", len(r.Uses), len(argv))
		}
		if !r.OK && (r.Error == "" || r.ErrorAt < 1 || r.ErrorAt >= len(argv)) {
			t.Fatalf("failure without a located error: %+v", r)
		}
		if r.OK && len(r.Leftover) > len(argv) {
			t.Fatalf("leftover grew: %q -> %q", argv, r.Leftover)
		}
	})
}
