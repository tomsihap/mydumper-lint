package goption

import (
	"slices"
	"testing"
)

func lookupCtx() *Context {
	return &Context{
		Main: &Group{Name: "main", Entries: []Entry{
			{Long: "threads", Short: 't', Arg: ArgInt},
			{Long: "big", Arg: ArgInt64},
			{Long: "ratio", Arg: ArgDouble},
			{Long: "where", Arg: ArgString},
			{Long: "tables", Arg: ArgStringArray},
			{Long: "files", Arg: ArgFilenameArray},
			{Long: "outputdir", Arg: ArgFilename},
			{Long: "routines", Arg: ArgNone},
		}},
		Groups: []*Group{
			{Name: "daemongroup", Entries: []Entry{{Long: "snapshot-count", Arg: ArgInt}, {Long: "secret", Arg: ArgInt, Flags: FlagNoAlias}}},
			{Name: "extra", Entries: []Entry{
				{Long: "compress", Arg: ArgCallback, Flags: FlagOptionalArg},
				{Long: "path", Arg: ArgCallback, Flags: FlagFilename},
				{Long: "toggle", Arg: ArgCallback, Flags: FlagNoArg},
				{Long: "regex", Arg: ArgCallback},
			}},
		},
	}
}

func TestLookup(t *testing.T) {
	c := lookupCtx()
	tests := []struct {
		name  string
		long  string
		alias bool
		ok    bool
	}{
		{"threads", "threads", false, true},
		{"threads=4", "threads", false, true},
		{"compress", "compress", false, true},
		{"daemon-snapshot-count", "snapshot-count", true, true},
		{"d-snapshot-count", "snapshot-count", true, true},
		{"daemon-secret", "", false, false}, // noalias
		{"main-threads", "", false, false},
		{"bogus", "", false, false},
		{"-threads", "", false, false},
	}
	for _, tt := range tests {
		r, alias, ok := c.Lookup(tt.name)
		if ok != tt.ok || alias != tt.alias || (ok && c.Entry(r).Long != tt.long) {
			t.Errorf("Lookup(%q) = %v, %v, %v", tt.name, r, alias, ok)
		}
	}
}

func TestCheck(t *testing.T) {
	c := lookupCtx()
	ref := func(name string) Ref {
		r, _, ok := c.Lookup(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		return r
	}
	tests := []struct {
		opt, value string
		cs         Charset
		fails      bool
	}{
		{"threads", "08", CharsetASCII, true},
		{"threads", "4", CharsetASCII, false},
		{"big", "99999999999", CharsetASCII, false},
		{"big", "1e3", CharsetASCII, true},
		{"ratio", "1.5", CharsetASCII, false},
		{"ratio", "x", CharsetASCII, true},
		{"where", "caf\xc3\xa9", CharsetASCII, true},
		{"where", "caf\xc3\xa9", CharsetUTF8, false},
		{"where", "\xff", CharsetUTF8, true},
		{"where", "\xff", CharsetOther, false},
		{"tables", "caf\xc3\xa9", CharsetASCII, true},
		{"outputdir", "caf\xc3\xa9", CharsetASCII, false},
		{"files", "\xff", CharsetASCII, false},
		{"routines", "anything", CharsetASCII, false},
		{"regex", "caf\xc3\xa9", CharsetASCII, true},
		{"compress", "caf\xc3\xa9", CharsetASCII, false}, // optional: receives NULL instead
		{"path", "caf\xc3\xa9", CharsetASCII, false},
		{"toggle", "caf\xc3\xa9", CharsetASCII, false},
	}
	for _, tt := range tests {
		c.Charset = tt.cs
		if got := c.Check(ref(tt.opt), "--"+tt.opt, tt.value) != ""; got != tt.fails {
			t.Errorf("Check(%s, %q, charset %d) fails=%v, want %v", tt.opt, tt.value, tt.cs, got, tt.fails)
		}
	}
	converted := map[string]bool{
		"where": true, "tables": true, "regex": true, "compress": true,
		"threads": false, "outputdir": false, "path": false, "toggle": false, "routines": false, "files": false,
	}
	for name, want := range converted {
		if got := c.Entry(ref(name)).Converted(); got != want {
			t.Errorf("%s.Converted() = %v", name, got)
		}
	}
}

func TestArrays(t *testing.T) {
	c := lookupCtx()
	tables, files := Ref{-1, 4}, Ref{-1, 5}
	init := map[Ref]Value{tables: {Strs: []string{"old"}}}
	r := c.Parse([]string{"g", "--tables", "a", "--tables", "b", "--files", "x"}, init)
	if !r.OK || !slices.Equal(r.Values[tables].Strs, []string{"a", "b"}) || !slices.Equal(r.Values[files].Strs, []string{"x"}) {
		t.Errorf("arrays: %+v", r.Values)
	}
	// A failed parse restores the array the variable held before.
	r = c.Parse([]string{"g", "--tables", "a", "--bogus"}, init)
	if r.OK || !slices.Equal(r.Values[tables].Strs, []string{"old"}) {
		t.Errorf("revert: %+v", r.Values[tables])
	}
	if r := c.Parse([]string{"g", "--tables", "\xff"}, nil); r.OK {
		t.Error("an invalid string in a string array must fail in the C locale")
	}
}

func TestShortClustersAndErrors(t *testing.T) {
	c := lookupCtx()
	c.IgnoreUnknown = true
	r := c.Parse([]string{"g", "-tz", "4"}, nil)
	if !r.OK || r.Values[Ref{-1, 0}].Int != 4 || !slices.Equal(r.Leftover, []string{"g", "-z"}) {
		t.Errorf("lenient cluster: %+v", r)
	}
	c.IgnoreUnknown = false
	if r := c.Parse([]string{"g", "-tt", "4", "5"}, nil); r.OK || r.Error != "Error parsing option -t" {
		t.Errorf("two value-taking letters: %+v", r)
	}
	if r := c.Parse([]string{"g", "-t"}, nil); r.OK || r.Error != "Missing argument for -t" {
		t.Errorf("missing short argument: %+v", r)
	}
	if r := c.Parse([]string{"g", "--compress"}, nil); !r.OK || len(r.Calls) != 1 || r.Calls[0].Value != nil {
		t.Errorf("optional value at the end: %+v", r)
	}
	if r := c.Parse([]string{"g", "--threads=4", "--routines=1", "--toggle=x"}, nil); !r.OK || r.Values[Ref{-1, 0}].Int != 4 {
		t.Errorf("inline values: %+v", r)
	}
}
