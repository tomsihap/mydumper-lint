package playground

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type checkDoc struct {
	Files []struct {
		Path        string `json:"path"`
		Loadable    bool   `json:"loadable"`
		Version     string `json:"mydumper_version"`
		Diagnostics []struct {
			ID  string `json:"id"`
			Fix *struct {
				Applicability string `json:"applicability"`
			} `json:"fix"`
		} `json:"diagnostics"`
	} `json:"files"`
}

func ids(t *testing.T, raw json.RawMessage) (checkDoc, []string) {
	t.Helper()
	var d checkDoc
	if err := json.Unmarshal(raw, &d); err != nil || len(d.Files) != 1 {
		t.Fatalf("check JSON: %v\n%s", err, raw)
	}
	var out []string
	for _, x := range d.Files[0].Diagnostics {
		out = append(out, x.ID)
	}
	return d, out
}

func TestAnalyze(t *testing.T) {
	src := []byte("[mydumper]\nthreads=2\n  \nroutines=0\n[`app`.`users`]\n`email`=random_string\n")
	r := Analyze(Request{Source: src, Name: "backup.cnf", Version: "v1.0.5-1"})
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	d, got := ids(t, r.Check)
	if d.Files[0].Path != "backup.cnf" || d.Files[0].Loadable || d.Files[0].Version != "v1.0.5-1" {
		t.Errorf("file: %+v", d.Files[0])
	}
	if !strings.Contains(strings.Join(got, " "), "MDL102") || !strings.Contains(strings.Join(got, " "), "MDL402") {
		t.Errorf("diagnostics %v, want MDL102 and MDL402", got)
	}
	if r.Command != "mydumper-lint check --mydumper-version v1.0.5-1 backup.cnf" {
		t.Errorf("command %q", r.Command)
	}
	// The safe fix removes the whitespace; only the unsafe one touches routines=0.
	if want := []byte("[mydumper]\nthreads=2\n\nroutines=0\n[`app`.`users`]\n`email`=random_string\n"); !bytes.Equal(r.Safe.Output, want) {
		t.Errorf("safe fix:\n%q\nwant\n%q", r.Safe.Output, want)
	}
	if !strings.Contains(r.Safe.Diff, "--- backup.cnf\n+++ backup.cnf\n") || r.Safe.Failure != "" {
		t.Errorf("safe diff %q, failure %q", r.Safe.Diff, r.Safe.Failure)
	}
	if bytes.Equal(r.Unsafe.Output, r.Safe.Output) || !strings.Contains(r.Unsafe.Diff, "routines") {
		t.Errorf("unsafe fix:\n%q\n%s", r.Unsafe.Output, r.Unsafe.Diff)
	}
	var inspect struct {
		Loadable bool   `json:"loadable"`
		Health   string `json:"health"`
	}
	if err := json.Unmarshal(r.Inspect, &inspect); err != nil || inspect.Loadable || inspect.Health != "rejected" {
		t.Errorf("inspect: %v %+v", err, inspect)
	}
}

func TestAnalyzeCleanFileAndDefaults(t *testing.T) {
	src := []byte("[mydumper]\nthreads=2\n")
	r := Analyze(Request{Source: src})
	d, got := ids(t, r.Check)
	if r.Error != "" || len(got) != 0 || d.Files[0].Path != DefaultName {
		t.Errorf("clean file: %q %v %+v", r.Error, got, d.Files[0])
	}
	if !bytes.Equal(r.Safe.Output, src) || r.Safe.Diff != "" || r.Unsafe.Diff != "" || len(r.Safe.Notes) != 0 {
		t.Errorf("nothing to fix: %+v", r.Safe)
	}
	if !strings.Contains(r.Command, "--mydumper-version latest") {
		t.Errorf("command %q", r.Command)
	}
}

// Exact bytes survive the round trip: CRLF, a BOM, invalid UTF-8.
func TestAnalyzeKeepsBytes(t *testing.T) {
	src := []byte("\xef\xbb\xbf[mydumper]\r\nthreads=2\r\n\r\nwhere=\xff\r\n")
	r := Analyze(Request{Source: src, Version: "v0.19.3-3"})
	_, got := ids(t, r.Check)
	if !strings.Contains(strings.Join(got, " "), "MDL101") {
		t.Errorf("diagnostics %v, want MDL101 (BOM)", got)
	}
	// []byte travels as base64 in JSON: every byte survives.
	b, _ := json.Marshal(r)
	var back Result
	if err := json.Unmarshal(b, &back); err != nil || !bytes.Equal(back.Safe.Output, r.Safe.Output) {
		t.Fatalf("JSON round trip: %v", err)
	}
	// The BOM goes, the invalid byte stays (it is a value, not a problem).
	if !bytes.HasPrefix(r.Safe.Output, []byte("[mydumper]")) || !bytes.Contains(r.Safe.Output, []byte("where=\xff")) {
		t.Errorf("fixed bytes: %q", r.Safe.Output)
	}
}

func TestAnalyzeErrors(t *testing.T) {
	r := Analyze(Request{Source: []byte("[mydumper]\n"), Version: "bogus"})
	if !strings.HasPrefix(r.Error, "--mydumper-version: invalid mydumper version") || r.Check != nil {
		t.Errorf("an invalid version must fail: %q", r.Error)
	}
	r = Analyze(Request{Source: []byte("[mydumper]\n"), Version: "v9.9.9-9"})
	if r.Error != "" || len(r.Notices) != 1 || !strings.Contains(r.Notices[0], "newer than every version") {
		t.Errorf("a future version is checked against the newest one, with a notice: %q %q", r.Error, r.Notices)
	}
	if got := shellQuote("my file.cnf"); got != `'my file.cnf'` {
		t.Errorf("shellQuote = %s", got)
	}
	if got := shellQuote("it's.cnf"); got != `'it'\''s.cnf'` {
		t.Errorf("shellQuote = %s", got)
	}
	if got := firstMessage(nil); got == "" {
		t.Error("firstMessage must never be empty")
	}
	if got := firstMessage([]byte("boom\n")); got != "boom" {
		t.Errorf("firstMessage = %q", got)
	}
}

func TestVersionsRulesBuild(t *testing.T) {
	var vs []map[string]any
	if err := json.Unmarshal(Versions(), &vs); err != nil || len(vs) < 28 {
		t.Errorf("versions: %v (%d)", err, len(vs))
	}
	var rs []map[string]any
	if err := json.Unmarshal(Rules(), &rs); err != nil || len(rs) < 60 {
		t.Errorf("rules: %v (%d)", err, len(rs))
	}
	if !strings.HasPrefix(Build(), "mydumper-lint ") {
		t.Errorf("build %q", Build())
	}
}
