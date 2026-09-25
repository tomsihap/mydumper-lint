package optionsdb

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	if len(db.Versions) != len(testVersions) || len(db.Options) != 6 {
		t.Errorf("parsed %d versions, %d options", len(db.Versions), len(db.Options))
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(db *DB)
		want   string
	}{
		{"schema", func(db *DB) { db.Schema = 2 }, "schema 2"},
		{"no version", func(db *DB) { db.Versions = nil }, "no version"},
		{"bad tag", func(db *DB) { db.Versions[0].Tag = "latest" }, "invalid mydumper version tag"},
		{"not canonical", func(db *DB) { db.Versions[0].Tag = "0.19.1-1" }, "canonical"},
		{"order", func(db *DB) { db.Versions[0], db.Versions[1] = db.Versions[1], db.Versions[0] }, "ascending"},
		{"commit", func(db *DB) { db.Versions[0].Commit = "abc" }, "invalid commit"},
		{"date", func(db *DB) { db.Versions[0].Date = "2026-1-2" }, "invalid date"},
		{"fingerprint", func(db *DB) { db.Versions[0].LoaderFingerprint = "x" }, "loader fingerprint"},
		{"tool", func(db *DB) { db.Options[0].Tool = "mysqldump" }, "unknown tool"},
		{"dashes", func(db *DB) { db.Options[0].Name = "--compress" }, "invalid option name"},
		{"option order", func(db *DB) { db.Options[0], db.Options[1] = db.Options[1], db.Options[0] }, "not sorted"},
		{"no span", func(db *DB) { db.Options[1].Spans = nil }, "no span"},
		{"unknown from", func(db *DB) { db.Options[1].Spans[0].From = "v9.9.9-9" }, "unknown version"},
		{"unknown to", func(db *DB) { db.Options[1].Spans[0].To = "v9.9.9-9" }, "unknown version"},
		{"reversed", func(db *DB) { db.Options[1].Spans[0].From, db.Options[1].Spans[0].To = "v0.20.1-2", "v0.19.3-1" }, "reversed"},
		{"explicit newest", func(db *DB) { db.Options[1].Spans[0].To = "v1.1.2-1" }, "must leave to empty"},
		{"short", func(db *DB) { db.Options[1].Spans[0].Short = "oo" }, "not one character"},
		{"arg", func(db *DB) { db.Options[1].Spans[0].Arg = "path" }, "unknown argument type"},
		{"flag", func(db *DB) { db.Options[1].Spans[0].Flags = []string{"sticky"} }, "unknown flag"},
		{"flag order", func(db *DB) { db.Options[1].Spans[0].Flags = []string{"reverse", "hidden"} }, "flags not sorted"},
		{"description", func(db *DB) { db.Options[1].Spans[0].Description = "text" }, "descriptions must stay empty"},
		{"ignore case alone", func(db *DB) { db.Options[1].Spans[0].ValuesIgnoreCase = true }, "values_ignore_case without values"},
		{"condition", func(db *DB) { db.Options[1].Spans[0].Condition = "WITH_TLS" }, "unknown build condition"},
		{"overlap", func(db *DB) {
			db.Options[3].Spans = append(db.Options[3].Spans, OptionSpan{Range: Range{From: "v1.0.5-1"}, Arg: "int", Condition: "WITH_SSL"})
		}, "2 spans apply"},
		{"fact order", func(db *DB) { db.TableKeys[0], db.TableKeys[3] = db.TableKeys[3], db.TableKeys[0] }, "not sorted"},
		{"fact prefix order", func(db *DB) { db.TableKeys[1], db.TableKeys[2] = db.TableKeys[2], db.TableKeys[1] }, "not sorted"},
		{"fact overlap", func(db *DB) {
			db.Products[1].Spans = []Range{{From: "v0.20.1-2", To: "v1.0.5-1"}, {From: "v1.0.5-1"}}
		}, "overlap"},
		{"fact no span", func(db *DB) { db.Products[0].Spans = nil }, "no span"},
		{"fact empty name", func(db *DB) { db.MasqueradeFunctions[0].Name = "" }, "empty masquerade function name"},
		{"fact range", func(db *DB) { db.Products[0].Spans[0].From = "v0.1.0-1" }, "unknown version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB()
			tt.mutate(db)
			b, err := json.Marshal(db)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(b); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsUnknownFieldsAndGarbage(t *testing.T) {
	b, _ := json.Marshal(newTestDB())
	withField := strings.Replace(string(b), `"schema":1`, `"schema":1,"extra":true`, 1)
	for name, data := range map[string]string{
		"unknown field": withField,
		"trailing":      string(b) + "{}",
		"not json":      "schema: 1",
		"empty":         "",
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
