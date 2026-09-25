package main

import (
	"slices"
	"strings"
	"testing"
)

func overlayEntries(t *testing.T, js string) *overlayFile {
	t.Helper()
	o, err := parseOverlay([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOverlayApply(t *testing.T) {
	xs := testExtractions()
	o := overlayEntries(t, `{"options": [
	  {"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.1-1", "is_regex": true, "evidence": "e1"},
	  {"tools": ["mydumper"], "name": "rows", "from": "v1.0.2-1", "to": "v1.0.2-1", "values": ["A", "b"], "values_ignore_case": true, "evidence": "e2"},
	  {"tools": ["myloader"], "name": "threads", "from": "v1.0.0-1", "to": "v1.1.0-1", "reviewed": true, "evidence": "e3"}
	]}`)
	warnings, err := o.apply(testTags, xs)
	if err != nil {
		t.Fatal(err)
	}
	if !xs[0].options["mydumper"]["rows"][0].isRegex || !xs[1].options["mydumper"]["rows"][0].isRegex {
		t.Error("is_regex not applied")
	}
	if v := xs[2].options["mydumper"]["rows"][0]; !slices.Equal(v.values, []string{"A", "b"}) || !v.valuesIgnoreCase || v.isRegex {
		t.Errorf("values not applied: %+v", v)
	}
	if v := xs[3].options["mydumper"]["rows"][0]; v.isRegex || v.values != nil {
		t.Errorf("applied outside its range: %+v", v)
	}
	if v := xs[0].options["myloader"]["threads"][0]; v.isRegex || v.values != nil {
		t.Errorf("reviewed entry set a fact: %+v", v)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "stops at v1.0.2-1") || !strings.Contains(warnings[0], "v1.1.0-1") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestOverlayErrors(t *testing.T) {
	tests := []struct{ name, js, want string }{
		{"unknown field", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "is_regex": true, "evidence": "e", "extra": 1}]}`, "unknown field"},
		{"no evidence", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "is_regex": true}]}`, "evidence are required"},
		{"no fact", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "evidence": "e"}]}`, "set is_regex or values"},
		{"reviewed with a fact", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "is_regex": true, "reviewed": true, "evidence": "e"}]}`, "set is_regex or values"},
		{"unsorted values", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "values": ["b", "a"], "evidence": "e"}]}`, "sorted"},
		{"ignore case alone", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "is_regex": true, "values_ignore_case": true, "evidence": "e"}]}`, "values_ignore_case without values"},
		{"unknown version", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v9.0.0-1", "to": "v1.0.0-1", "is_regex": true, "evidence": "e"}]}`, "embedded versions"},
		{"reversed", `{"options": [{"tools": ["mydumper"], "name": "rows", "from": "v1.0.1-1", "to": "v1.0.0-1", "is_regex": true, "evidence": "e"}]}`, "reversed"},
		{"unknown tool", `{"options": [{"tools": ["mysqldump"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.0-1", "is_regex": true, "evidence": "e"}]}`, "unknown or repeated tool"},
		{"missing option", `{"options": [{"tools": ["mydumper"], "name": "ssl", "from": "v1.0.0-1", "to": "v1.0.1-1", "is_regex": true, "evidence": "e"}]}`, "no option --ssl in v1.0.1-1"},
		{"overlap", `{"options": [
		  {"tools": ["mydumper"], "name": "rows", "from": "v1.0.0-1", "to": "v1.0.1-1", "is_regex": true, "evidence": "e"},
		  {"tools": ["mydumper"], "name": "rows", "from": "v1.0.1-1", "to": "v1.0.2-1", "is_regex": true, "evidence": "e"}]}`, "overlaps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseOverlay([]byte(tt.js))
			if err == nil {
				_, err = o.apply(testTags, testExtractions())
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestCommittedOverlay checks that overlay.json parses and follows the
// conventions; the generator run validates it against real versions.
func TestCommittedOverlay(t *testing.T) {
	o, err := loadOverlay("overlay.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Options) == 0 {
		t.Fatal("empty overlay")
	}
	for _, e := range o.Options {
		if e.Evidence == "" || e.From == "" || e.To == "" {
			t.Errorf("%s: incomplete entry", e)
		}
	}
}
