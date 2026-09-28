package model

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// fakeTarget is a tiny knowledge base: both tools share the same options.
type fakeTarget struct {
	ignore bool
	opts   map[string]optionsdb.OptionSpan
}

func newFake(ignore bool) fakeTarget {
	return fakeTarget{ignore: ignore, opts: map[string]optionsdb.OptionSpan{
		"threads":        {Short: "t", Arg: "int"},
		"routines":       {Arg: "none"},
		"where":          {Arg: "string"},
		"outputdir":      {Short: "o", Arg: "filename"},
		"compress":       {Short: "c", Arg: "callback", Flags: []string{"optional_arg"}},
		"snapshot-count": {Short: "X", Arg: "int", Group: "daemongroup"},
	}}
}

func (f fakeTarget) IgnoreUnknownOptions() bool { return f.ignore }
func (f fakeTarget) Option(_, name string) (optionsdb.OptionSpan, bool) {
	s, ok := f.opts[name]
	return s, ok
}

func (f fakeTarget) OptionNames(string) []string {
	var names []string
	for n := range f.opts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func (f fakeTarget) TableKey(k string) bool        { return k == "where" }
func (f fakeTarget) MasqueradeFunctions() []string { return []string{"constant", "random_string"} }
func (f fakeTarget) Products() []string            { return nil }

func buildWith(s string, t Target, cs goption.Charset) *Model {
	f := source.New("t.cnf", []byte(s))
	return Build(keyfile.Parse(f, preprocess.Run(f)), Options{Target: t, Charset: cs})
}

func reasons(m *Model) string {
	var out []string
	for _, g := range m.Groups {
		for _, e := range g.Entries {
			out = append(out, g.Name+"."+e.Key+":"+e.Reason.String())
		}
	}
	return strings.Join(out, " ")
}

func TestGOptionSemantics(t *testing.T) {
	tests := []struct {
		name    string
		lenient bool
		charset goption.Charset
		in      string
		health  Health
		fatal   string
		reasons string
	}{
		{
			name: "unknown option, strict", in: "[mydumper]\nthreads=4\nroutines=0\nbogus=1\nhost=h\n",
			health: FatalAtStartup, fatal: "mydumper: option parsing failed: Unknown option --bogus, try --help",
			reasons: "mydumper.threads:effective mydumper.routines:effective mydumper.bogus:fatal-at-startup mydumper.host:connection-key",
		},
		{
			name: "unknown option, lenient", lenient: true, in: "[mydumper]\nthreads=4\nbogus=1\n", health: OK,
			reasons: "mydumper.threads:effective mydumper.bogus:unknown-option",
		},
		{
			name: "G8: a value parsed as an option swallows the next key", in: "[mydumper]\nroutines=-t\nthreads=4\n",
			health: FatalAtStartup, fatal: "mydumper: option parsing failed: Cannot parse integer value “--threads” for -t, try --help",
			reasons: "mydumper.routines:fatal-at-startup mydumper.threads:consumed-as-option-value",
		},
		{
			name: "G9: a -- value drops the following keys", in: "[mydumper]\nroutines=--\nthreads=4\nwhere=x\n", health: OK,
			reasons: "mydumper.routines:effective mydumper.threads:after-end-of-options mydumper.where:after-end-of-options",
		},
		{
			name: "G5: an invalid integer", in: "[mydumper]\nthreads=10 \n", health: FatalAtStartup,
			fatal:   "mydumper: option parsing failed: Cannot parse integer value “10 ” for --threads, try --help",
			reasons: "mydumper.threads:fatal-at-startup",
		},
		{
			name: "non-ASCII string in the C locale", in: "[mydumper]\nwhere=caf\xc3\xa9\n", health: FatalAtStartup,
			fatal:   "mydumper: option parsing failed: Invalid byte sequence in conversion input, try --help",
			reasons: "mydumper.where:fatal-at-startup",
		},
		{
			name: "non-ASCII string in a UTF-8 locale", charset: goption.CharsetUTF8, in: "[mydumper]\nwhere=caf\xc3\xa9\n", health: OK,
			reasons: "mydumper.where:effective",
		},
		{
			name: "group alias", in: "[mydumper]\ndaemon-snapshot-count=5\n", health: OK,
			reasons: "mydumper.daemon-snapshot-count:effective",
		},
		{
			name: "product group", in: "[mydumper_mysql]\nbogus=1\n", health: FatalAtStartup,
			fatal:   "mydumper: option parsing failed: Unknown option --bogus, try --help",
			reasons: "mydumper_mysql.bogus:fatal-at-startup",
		},
		{
			name: "the tool group is parsed before a product group", in: "[mydumper_mysql]\nbad=1\n[mydumper]\nbogus=1\n",
			health: FatalAtStartup, fatal: "mydumper: option parsing failed: Unknown option --bogus, try --help",
			reasons: "mydumper_mysql.bad:fatal-at-startup mydumper.bogus:fatal-at-startup",
		},
		{
			name: "duplicate keys are both parsed", in: "[mydumper]\nthreads=4\nthreads=8\n", health: OK,
			reasons: "mydumper.threads:shadowed-by-duplicate mydumper.threads:effective",
		},
		{
			name: "non-option groups are not parsed", in: "[client]\nbogus=1\n[`db`.`t`]\nwhere=a\n", health: OK,
			reasons: "client.bogus:effective `db`.`t`.where:effective",
		},
		{
			name: "table keys and masking", in: "[`db`.`t`]\nwhere=a\nwehre=b\n`c`=constant x\n`d`=radnom_string\n`e`=\n", health: OK,
			reasons: "`db`.`t`.where:effective `db`.`t`.wehre:unknown-table-key `db`.`t`.`c`:effective " +
				"`db`.`t`.`d`:masquerade-fallback-identity `db`.`t`.`e`:masquerade-fallback-identity",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := buildWith(tt.in, newFake(tt.lenient), tt.charset)
			if m.Health != tt.health || m.Fatal != tt.fatal {
				t.Errorf("health=%v fatal=%q; want %v %q", m.Health, m.Fatal, tt.health, tt.fatal)
			}
			if got := reasons(m); got != tt.reasons {
				t.Errorf("reasons:\n got  %s\n want %s", got, tt.reasons)
			}
		})
	}
}

func TestGOptionVector(t *testing.T) {
	m := buildWith("[mydumper]\nthreads=4\nhost=h\nthreads=8\nk[fr]=x\nk[C]=y\n", newFake(true), goption.CharsetASCII)
	run := m.Groups[0].GOption
	want := []string{"mydumper", "--threads", "8", "--threads", "8", "--k[C]", "y"}
	if run == nil || !slices.Equal(run.Argv, want) {
		t.Fatalf("argv = %q, want %q", run.Argv, want)
	}
	var elements []int
	for _, e := range m.Groups[0].Entries {
		elements = append(elements, e.Element)
	}
	if !slices.Equal(elements, []int{1, 0, 3, 0, 5}) {
		t.Errorf("elements = %v", elements)
	}
	if m.Projection() != "[mydumper]\n\"host\"=\"h\"\n\"threads\"=\"8\"\n" {
		t.Errorf("projection = %q", m.Projection())
	}
}

func TestOptionContext(t *testing.T) {
	ctx := OptionContext(newFake(false), "mydumper", goption.CharsetUTF8)
	if ctx.IgnoreUnknown || ctx.Charset != goption.CharsetUTF8 || len(ctx.Groups) != 1 || ctx.Groups[0].Name != "daemongroup" {
		t.Fatalf("context: %+v", ctx)
	}
	r, alias, ok := ctx.Lookup("d-snapshot-count")
	if !ok || !alias || ctx.Entry(r).Long != "snapshot-count" {
		t.Errorf("lookup: %v %v %v", r, alias, ok)
	}
	if _, _, ok := ctx.Lookup("main-threads"); ok {
		t.Error("the main group has no alias")
	}
	if e := ctx.Entry(Ref(t, ctx, "compress")); !e.OptionalArg() || e.Short != 'c' {
		t.Errorf("compress: %+v", e)
	}
}

// Ref resolves a long name in a context, failing the test when absent.
func Ref(t *testing.T, ctx *goption.Context, name string) goption.Ref {
	t.Helper()
	r, _, ok := ctx.Lookup(name)
	if !ok {
		t.Fatalf("%s not found", name)
	}
	return r
}

func TestStringsOutOfRange(t *testing.T) {
	if Health(9).String() != "Health(9)" || GroupKind(99).String() != "GroupKind(99)" || Reason(99).String() != "Reason(99)" {
		t.Error("out-of-range names")
	}
}

// oddTarget declares an option of a type GOption does not know and one
// OptionNames lists without a definition: both are skipped.
type oddTarget struct{ fakeTarget }

func (o oddTarget) OptionNames(string) []string { return []string{"ghost", "weird", "threads"} }
func (o oddTarget) Option(tool, name string) (optionsdb.OptionSpan, bool) {
	switch name {
	case "weird":
		return optionsdb.OptionSpan{Arg: "pointer"}, true
	case "ghost":
		return optionsdb.OptionSpan{}, false
	}
	return o.fakeTarget.Option(tool, name)
}

func TestOptionContextSkipsWhatItCannotEmulate(t *testing.T) {
	ctx := OptionContext(oddTarget{newFake(false)}, "mydumper", goption.CharsetASCII)
	if len(ctx.Main.Entries) != 1 || ctx.Main.Entries[0].Long != "threads" {
		t.Errorf("entries: %+v", ctx.Main.Entries)
	}
}

func TestFatalAttributionAndRejectedFiles(t *testing.T) {
	// A rejected file is parsed for the rules, but applies nothing.
	m := buildWith("[mydumper]\nthreads=08\n  \n", newFake(false), goption.CharsetASCII)
	if m.Health != Rejected || m.Groups[0].GOption == nil || m.Groups[0].Entries[0].Reason != ReasonFileRejected {
		t.Errorf("rejected file: health=%v run=%v", m.Health, m.Groups[0].GOption)
	}
	var run GOptionRun
	g := Group{Entries: []Entry{{Element: 1}}}
	if run.entryAt(&g, 5) != -1 || run.entryAt(&g, 2) != 0 {
		t.Error("entryAt")
	}
}
