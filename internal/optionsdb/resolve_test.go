package optionsdb

import (
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	tests := []struct {
		spec    string
		want    string
		exact   bool
		warning string // substring; "" means no warning
	}{
		{"v0.19.3-3", "v0.19.3-3", true, ""},
		{"0.19.3-3", "v0.19.3-3", true, ""},
		{" v0.19.3-1 ", "v0.19.3-1", true, ""},
		{"0.19.3", "v0.19.3-3", true, ""},           // latest build of 0.19.3
		{"v0.19.3", "v0.19.3-3", true, ""},          // leading v accepted
		{"0.21.3", "v0.21.3-2", true, ""},           // latest build, pre-release included
		{"0.19", "v0.19.3-3", true, ""},             // latest stable 0.19.x
		{"0.20", "v0.20.1-2", true, ""},             // v0.20.2-2 is a pre-release
		{"0.21", "v0.21.3-1", true, ""},             // v0.21.3-2 is a pre-release
		{"latest", "v1.0.5-1", true, ""},            // latest stable
		{"latest-prerelease", "v1.1.2-1", true, ""}, // newest tag
		{"1.1", "v1.1.2-1", false, "no stable mydumper 1.1 release"},
		{"v0.19.4-23", "v0.19.3-3", false, "nearest lower"}, // image build without a tag
		{"0.19.4", "v0.19.3-3", false, "nearest lower"},
		{"0.20.2-1", "v0.20.1-2", false, "nearest lower"}, // tag without release nor image
		{"0.19.2", "v0.19.1-1", false, "nearest lower"},
		{"0.22", "v0.21.3-2", false, "nearest lower"},
		{"1.0.6-1", "v1.0.5-1", false, "nearest lower"},
		{"v2.0.0-1", "v1.1.2-1", false, "may be outdated"},
		{"1.2", "v1.1.2-1", false, "may be outdated"},
		{"1.1.3", "v1.1.2-1", false, "may be outdated"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			r, err := db.Resolve(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			if r.Version.Tag != tt.want || r.Exact != tt.exact {
				t.Errorf("got %s exact=%v, want %s exact=%v", r.Version.Tag, r.Exact, tt.want, tt.exact)
			}
			if (tt.warning == "") != (r.Warning == "") || !strings.Contains(r.Warning, tt.warning) {
				t.Errorf("warning = %q, want %q", r.Warning, tt.warning)
			}
			if r.Version != &db.Versions[db.idx.pos[r.Version.Tag]] {
				t.Error("Version does not point into the DB")
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	db := parseTestDB(t, newTestDB())
	tests := []struct{ spec, want string }{
		{"v0.18.1-1", "older than v0.19.1-1"},
		{"0.18", "older than v0.19.1-1"},
		{"0.19.0", "older than v0.19.1-1"},
		{"0.19.1-0", "older than v0.19.1-1"},
		{"", "invalid mydumper version"},
		{"stable", "invalid mydumper version"},
		{"1", "invalid mydumper version"},
		{"0.19.3-", "invalid mydumper version"},
		{"latest-pre", "invalid mydumper version"},
	}
	for _, tt := range tests {
		if _, err := db.Resolve(tt.spec); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Resolve(%q) error = %v, want %q", tt.spec, err, tt.want)
		}
	}
}

func TestResolveWithoutStable(t *testing.T) {
	d := newTestDB()
	for i := range d.Versions {
		d.Versions[i].Prerelease = true
	}
	db := parseTestDB(t, d)
	r, err := db.Resolve("latest")
	if err != nil || r.Version.Tag != "v1.1.2-1" || r.Exact || !strings.Contains(r.Warning, "no stable") {
		t.Errorf("latest = %+v, %v", r, err)
	}
}
