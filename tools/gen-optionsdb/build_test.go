package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

var testTags = []string{"v1.0.0-1", "v1.0.1-1", "v1.0.2-1", "v1.1.0-1"}

func testReleases() []release {
	var out []release
	for i, tag := range testTags {
		out = append(out, release{
			tag: tag, parsed: optionsdb.MustParseTag(tag), commit: strings.Repeat(string(rune('a'+i)), 40),
			hasRelease: true, prerelease: i%2 == 1, date: "2026-01-0" + string(rune('1'+i)),
		})
	}
	return out
}

func variant(short, arg, group, cond string, flags ...string) optVariant {
	return optVariant{def: optDef{Short: short, Arg: arg, Flags: flags, Group: group}, cond: cond, source: "src/x.c:1"}
}

// testExtractions builds one extraction per test tag.
func testExtractions() []*extraction {
	var xs []*extraction
	for i, tag := range testTags {
		x := &extraction{
			tag: tag, options: map[string]map[string][]optVariant{"mydumper": {}, "myloader": {}},
			fingerprint: strings.Repeat("0", 63) + string(rune('0'+i)),
			funcPrints:  map[string]string{"load_config_file": "same", "parse_key_file_group": string(rune('0' + i/2))},
		}
		// retyped in the third version
		if i < 2 {
			x.options["mydumper"]["rows"] = []optVariant{variant("r", "int", "", "")}
		} else {
			x.options["mydumper"]["rows"] = []optVariant{variant("r", "callback", "job", "")}
		}
		// missing from the second version
		if i != 1 {
			x.options["mydumper"]["ssl"] = []optVariant{variant("", "none", "conn", "WITH_SSL")}
		}
		x.options["myloader"]["threads"] = []optVariant{variant("t", "int", "", "")}
		x.ignoreUnknown = i >= 3
		x.tableKeys = []factName{{name: "where"}}
		if i != 2 {
			x.tableKeys = append(x.tableKeys, factName{name: "limit"})
		}
		if i < 2 {
			x.tableKeys = append(x.tableKeys, factName{name: "replace"})
		} else {
			x.tableKeys = append(x.tableKeys, factName{name: "replace", prefix: true})
		}
		x.tableKeys = mergeFacts(x.tableKeys)
		x.masquerade = []factName{{name: "constant", prefix: true}}
		x.products = []string{"mysql"}
		xs = append(xs, x)
	}
	return xs
}

func TestBuildDB(t *testing.T) {
	db, err := buildDB(testReleases(), testExtractions(), map[string]bool{"v1.0.0-1": true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := encodeDB(db)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := optionsdb.Parse(data)
	if err != nil {
		t.Fatalf("generated data does not validate: %v\n%s", err, data)
	}
	if !bytes.HasSuffix(data, []byte("}\n")) || !bytes.Contains(data, []byte("\n  \"versions\": [\n")) {
		t.Error("not indented with two spaces and a final newline")
	}
	spans := func(tool, name string) string {
		var parts []string
		for _, s := range parsed.History(tool, name) {
			parts = append(parts, s.From+".."+s.To+":"+s.Arg+"/"+s.Group+"/"+s.Condition)
		}
		return strings.Join(parts, " ")
	}
	tests := []struct{ tool, name, want string }{
		{"mydumper", "rows", "v1.0.0-1..v1.0.1-1:int// v1.0.2-1..:callback/job/"},
		{"mydumper", "ssl", "v1.0.0-1..v1.0.0-1:none/conn/WITH_SSL v1.0.2-1..:none/conn/WITH_SSL"},
		{"myloader", "threads", "v1.0.0-1..:int//"},
	}
	for _, tt := range tests {
		if got := spans(tt.tool, tt.name); got != tt.want {
			t.Errorf("%s %s spans = %q, want %q", tt.tool, tt.name, got, tt.want)
		}
	}
	var keys []string
	for _, f := range parsed.TableKeys {
		var rs []string
		for _, r := range f.Spans {
			rs = append(rs, r.From+".."+r.To)
		}
		keys = append(keys, f.Name+map[bool]string{true: "*", false: ""}[f.Prefix]+"="+strings.Join(rs, ","))
	}
	if got, want := strings.Join(keys, " "), "limit=v1.0.0-1..v1.0.1-1,v1.1.0-1.. replace=v1.0.0-1..v1.0.1-1 replace*=v1.0.2-1.. where=v1.0.0-1.."; got != want {
		t.Errorf("table keys = %q, want %q", got, want)
	}
	v := parsed.Versions
	if !v[0].ImageVerified || v[1].ImageVerified || !v[1].Prerelease || v[0].Date != "2026-01-01" || !v[3].IgnoreUnknownOptions {
		t.Errorf("versions = %+v", v)
	}
	again, _ := buildDB(testReleases(), testExtractions(), map[string]bool{"v1.0.0-1": true})
	data2, _ := encodeDB(again)
	if !bytes.Equal(data, data2) {
		t.Error("two builds differ")
	}
}

func TestChanges(t *testing.T) {
	got := strings.Join(changes(testReleases(), testExtractions()), "\n")
	for _, want := range []string{
		"v1.0.1-1: mydumper: removed ssl",
		"v1.0.2-1: mydumper: added ssl; changed rows",
		"v1.0.2-1: loader fingerprint changed: parse_key_file_group",
		"v1.1.0-1: ignore_unknown_options false -> true",
		"v1.0.2-1: table keys: replace* where",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("changes lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "load_config_file") {
		t.Errorf("unchanged function reported:\n%s", got)
	}
}

func TestVariantKeyIgnoresSource(t *testing.T) {
	a := variant("t", "int", "", "")
	b := a
	b.source = "src/y.c:9"
	if variantKey(a) != variantKey(b) {
		t.Error("source is part of the span identity")
	}
	b.values = []string{"x"}
	if variantKey(a) == variantKey(b) {
		t.Error("values are not part of the span identity")
	}
	if _, err := json.Marshal(a.def); err != nil {
		t.Fatal(err)
	}
}
