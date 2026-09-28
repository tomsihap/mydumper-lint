package optionsdb

import (
	"slices"
	"strings"
	"testing"
)

// target mirrors the interface internal/model consumes; *View must satisfy
// it exactly.
type target interface {
	IgnoreUnknownOptions() bool
	Option(tool, name string) (OptionSpan, bool)
	TableKey(key string) bool
	MasqueradeFunctions() []string
	Products() []string
	ProductOptionGroups() []string
	TableSectionsIgnored() []string
}

var _ target = (*View)(nil)

func TestViewOptions(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	old := mustView(t, db, "v0.19.3-1", DefaultBuild)
	cur := mustView(t, db, "v1.0.5-1", DefaultBuild)

	if s, ok := old.Option("mydumper", "compress"); !ok || s.Values != nil || s.Group != "extra" || s.To != "v0.19.3-1" {
		t.Errorf("v0.19.3-1 compress = %+v, %v", s, ok)
	}
	if s, ok := cur.Option("mydumper", "compress"); !ok || !slices.Equal(s.Values, []string{"gzip", "zstd"}) || !s.ValuesIgnoreCase {
		t.Errorf("v1.0.5-1 compress = %+v, %v", s, ok)
	}
	if _, ok := cur.Option("myloader", "compress"); ok {
		t.Error("compress exists for myloader")
	}
	if _, ok := cur.Option("mydumper", "no-such-option"); ok {
		t.Error("unknown option found")
	}
	// trx-tables: absent between v0.19.3-3 and v0.21.3-1
	for tag, want := range map[string]string{"v0.19.3-3": "none", "v0.20.1-2": "", "v0.21.3-1": "callback", "v1.1.2-1": "callback"} {
		s, ok := mustView(t, db, tag, DefaultBuild).Option("mydumper", "trx-tables")
		if ok != (want != "") || s.Arg != want {
			t.Errorf("%s trx-tables = %+v, %v; want arg %q", tag, s, ok, want)
		}
	}
	if name, ok := cur.OptionByShort("mydumper", "t"); !ok || name != "threads" {
		t.Errorf("short t = %q, %v", name, ok)
	}
	if name, ok := cur.OptionByShort("mydumper", "o"); !ok || name != "outputdir" {
		t.Errorf("short o = %q, %v", name, ok)
	}
	if _, ok := cur.OptionByShort("mydumper", "z"); ok {
		t.Error("unknown short name found")
	}
	if got := cur.OptionNames("mydumper"); !slices.Equal(got, []string{"compress", "outputdir", "ssl-mode", "threads", "trx-tables"}) {
		t.Errorf("OptionNames = %q", got)
	}
	if got := cur.OptionNames("myloader"); !slices.Equal(got, []string{"threads"}) {
		t.Errorf("myloader OptionNames = %q", got)
	}
	if cur.Version().Tag != "v1.0.5-1" || cur.Build() != DefaultBuild {
		t.Errorf("Version/Build = %s %s", cur.Version().Tag, cur.Build())
	}
	if old.IgnoreUnknownOptions() || !cur.IgnoreUnknownOptions() {
		t.Error("IgnoreUnknownOptions wrong")
	}
}

func TestViewBuilds(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	tests := []struct {
		build  Build
		values []string // nil: option absent
	}{
		{Build{ClientMySQL, true}, []string{"DISABLED", "REQUIRED"}},
		{Build{ClientMariaDB, true}, []string{"REQUIRED"}},
		{Build{ClientMySQL, false}, nil},
		{Build{ClientMariaDB, false}, nil},
	}
	for _, tt := range tests {
		s, ok := mustView(t, db, "v0.20.1-2", tt.build).Option("mydumper", "ssl-mode")
		if ok != (tt.values != nil) || !slices.Equal(s.Values, tt.values) {
			t.Errorf("%s: ssl-mode = %+v, %v", tt.build, s, ok)
		}
	}
	if _, err := db.View("v0.20.1-2", Build{Client: "percona"}); err == nil {
		t.Error("unknown client: no error")
	}
	if _, err := db.View("v0.20.2-1", DefaultBuild); err == nil || !strings.Contains(err.Error(), "not an embedded version") {
		t.Errorf("unknown tag: %v", err)
	}
}

func TestViewFacts(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	tests := []struct {
		tag      string
		keys     []string
		accepted []string
		rejected []string
		masq     []string
		products []string
	}{
		{
			"v0.19.3-3",
			[]string{"columns_on_select", "where"},
			[]string{"where", "columns_on_select"},
			[]string{"limit", "columns_on_select_replace", "Where", "where "},
			[]string{"constant"},
			[]string{"mysql"},
		},
		{
			"v0.21.3-1",
			[]string{"columns_on_select", "columns_on_select_replace", "where"},
			[]string{"columns_on_select_replace"},
			[]string{"columns_on_select_replace_`c`"},
			[]string{"constant"},
			[]string{"mysql", "rds"},
		},
		{
			"v1.0.5-1",
			[]string{"columns_on_select", "columns_on_select_replace", "where"},
			[]string{"columns_on_select_replace", "columns_on_select_replace_`c`"},
			[]string{"columns_on_selec"},
			[]string{"constant", "null"},
			[]string{"mysql", "rds"},
		},
	}
	for _, tt := range tests {
		v := mustView(t, db, tt.tag, DefaultBuild)
		if got := v.TableKeys(); !slices.Equal(got, tt.keys) {
			t.Errorf("%s TableKeys = %q, want %q", tt.tag, got, tt.keys)
		}
		for _, k := range tt.accepted {
			if !v.TableKey(k) {
				t.Errorf("%s: TableKey(%q) = false", tt.tag, k)
			}
		}
		for _, k := range tt.rejected {
			if v.TableKey(k) {
				t.Errorf("%s: TableKey(%q) = true", tt.tag, k)
			}
		}
		if got := v.MasqueradeFunctions(); !slices.Equal(got, tt.masq) {
			t.Errorf("%s masquerade = %q", tt.tag, got)
		}
		if got := v.Products(); !slices.Equal(got, tt.products) {
			t.Errorf("%s products = %q", tt.tag, got)
		}
	}
}

func TestViewReturnsCopies(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	v := mustView(t, db, "v1.0.5-1", DefaultBuild)
	s, _ := v.Option("mydumper", "compress")
	s.Values[0] = "mutated"
	s.Flags[0] = "mutated"
	v.MasqueradeFunctions()[0] = "mutated"
	v.Products()[0] = "mutated"
	again, _ := v.Option("mydumper", "compress")
	if again.Values[0] != "gzip" || again.Flags[0] != "optional_arg" || v.MasqueradeFunctions()[0] != "constant" || v.Products()[0] != "mysql" {
		t.Error("callers can mutate the knowledge base")
	}
}

func TestHistory(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	h := db.History("mydumper", "trx-tables")
	if len(h) != 2 || h[0].From != "v0.19.1-1" || h[0].To != "v0.19.3-3" || h[1].From != "v0.21.3-1" || h[1].To != "" {
		t.Errorf("History = %+v", h)
	}
	if h := db.History("mydumper", "ssl-mode"); len(h) != 2 || h[0].Condition == h[1].Condition {
		t.Errorf("ssl-mode history = %+v", h)
	}
	if h := db.History("myloader", "outputdir"); h != nil {
		t.Errorf("unknown option history = %+v", h)
	}
	h[0].Arg = "mutated"
	if db.History("mydumper", "trx-tables")[0].Arg != "none" {
		t.Error("History exposes the DB")
	}
	if v, ok := db.Lookup("v0.20.2-2"); !ok || !v.Prerelease {
		t.Error("Lookup failed")
	}
	if _, ok := db.Lookup("v0.20.2-1"); ok {
		t.Error("Lookup found a missing tag")
	}
	if db.Oldest().Tag != "v0.19.1-1" || db.Newest().Tag != "v1.1.2-1" {
		t.Error("Oldest/Newest wrong")
	}
}
