package optionsdb

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// testVersions is a synthetic history: stable and pre-release versions,
// a minor series without a stable release (1.1), and a gap (0.19.4).
var testVersions = []struct {
	tag        string
	prerelease bool
}{
	{"v0.19.1-1", false},
	{"v0.19.3-1", false},
	{"v0.19.3-3", false},
	{"v0.20.1-2", false},
	{"v0.20.2-2", true},
	{"v0.21.3-1", false},
	{"v0.21.3-2", true},
	{"v1.0.5-1", false},
	{"v1.1.0-1", true},
	{"v1.1.2-1", true},
}

func newTestDB() *DB {
	db := &DB{Schema: SchemaVersion}
	for i, v := range testVersions {
		db.Versions = append(db.Versions, Version{
			Tag: v.tag, Commit: strings.Repeat(fmt.Sprintf("%x", i%16), 40), Prerelease: v.prerelease,
			IgnoreUnknownOptions: i >= 7, LoaderFingerprint: strings.Repeat("a", 64),
		})
	}
	db.Options = []Option{
		{Name: "compress", Tool: "mydumper", Spans: []OptionSpan{
			{Range: Range{From: "v0.19.1-1", To: "v0.19.3-1"}, Short: "c", Arg: "callback", Flags: []string{"optional_arg"}, Group: "extra"},
			{Range: Range{From: "v0.19.3-3"}, Short: "c", Arg: "callback", Flags: []string{"optional_arg"}, Group: "extra",
				Values: []string{"gzip", "zstd"}, ValuesIgnoreCase: true},
		}},
		{Name: "outputdir", Tool: "mydumper", Spans: []OptionSpan{{Range: Range{From: "v0.19.1-1"}, Short: "o", Arg: "filename"}}},
		{Name: "ssl-mode", Tool: "mydumper", Spans: []OptionSpan{
			{Range: Range{From: "v0.19.1-1"}, Arg: "string", Group: "connectiongroup", Condition: "WITH_SSL && !LIBMARIADB", Values: []string{"DISABLED", "REQUIRED"}},
			{Range: Range{From: "v0.19.1-1"}, Arg: "string", Group: "connectiongroup", Condition: "WITH_SSL && LIBMARIADB", Values: []string{"REQUIRED"}},
		}},
		{Name: "threads", Tool: "mydumper", Spans: []OptionSpan{{Range: Range{From: "v0.19.1-1"}, Short: "t", Arg: "int"}}},
		{Name: "trx-tables", Tool: "mydumper", Spans: []OptionSpan{
			{Range: Range{From: "v0.19.1-1", To: "v0.19.3-3"}, Arg: "none", Group: "lock"},
			{Range: Range{From: "v0.21.3-1"}, Arg: "callback", Flags: []string{"optional_arg"}, Group: "lock"},
		}},
		{Name: "threads", Tool: "myloader", Spans: []OptionSpan{{Range: Range{From: "v0.19.1-1"}, Short: "t", Arg: "int"}}},
	}
	all := []Range{{From: "v0.19.1-1"}}
	db.TableKeys = []Fact{
		{Name: "columns_on_select", Spans: all},
		{Name: "columns_on_select_replace", Spans: []Range{{From: "v0.21.3-1", To: "v0.21.3-2"}}},
		{Name: "columns_on_select_replace", Prefix: true, Spans: []Range{{From: "v1.0.5-1"}}},
		{Name: "where", Spans: all},
	}
	db.MasqueradeFunctions = []Fact{
		{Name: "constant", Prefix: true, Spans: all},
		{Name: "null", Prefix: true, Spans: []Range{{From: "v1.0.5-1"}}},
	}
	db.Products = []Fact{{Name: "mysql", Spans: all}, {Name: "rds", Spans: []Range{{From: "v0.20.1-2"}}}}
	return db
}

// parseTestDB round-trips a DB through JSON and Parse.
func parseTestDB(t *testing.T, db *DB) *DB {
	t.Helper()
	b, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustView(t *testing.T, db *DB, tag string, b Build) *View {
	t.Helper()
	v, err := db.View(tag, b)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
