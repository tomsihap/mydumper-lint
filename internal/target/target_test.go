package target

import (
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/lint"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		spec, tag, notice string // notice: expected substring, "" for none
		pinned, preproc   bool
	}{
		{"", "", "no mydumper version configured", false, true},
		{"v0.19.1-3", "v0.19.1-3", "", true, false},
		{"0.19.1", "v0.19.1-3", "", true, false},
		{"v0.19.3-1", "v0.19.3-1", "", true, true},
		{"0.19.4-23", "v0.19.3-3", "nearest lower version", true, true},
		{"99.0", "", "newer than every version", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := Resolve(tt.spec, optionsdb.DefaultBuild)
			if err != nil {
				t.Fatal(err)
			}
			if tt.tag != "" && got.Version() != tt.tag {
				t.Errorf("version = %s, want %s", got.Version(), tt.tag)
			}
			if tt.notice == "" && got.Notice != "" || !strings.Contains(got.Notice, tt.notice) {
				t.Errorf("notice = %q, want it to contain %q", got.Notice, tt.notice)
			}
			if got.Pinned != tt.pinned {
				t.Errorf("pinned = %v, want %v", got.Pinned, tt.pinned)
			}
			var cfg lint.Config
			got.Configure(&cfg)
			if cfg.Target == nil || cfg.Version != got.Version() || cfg.NoPreprocessor == tt.preproc {
				t.Errorf("Configure: target %v, version %q, no pre-processor %v", cfg.Target, cfg.Version, cfg.NoPreprocessor)
			}
		})
	}
}

func TestResolveLatestIsStable(t *testing.T) {
	got, err := Resolve("", optionsdb.DefaultBuild)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.View.Version(); v.Prerelease {
		t.Errorf("default target %s is a pre-release", v.Tag)
	}
	if !strings.Contains(got.Notice, got.Version()) {
		t.Errorf("the notice must name the version: %q", got.Notice)
	}
}

func TestResolveErrors(t *testing.T) {
	for _, spec := range []string{"0.18", "v0.10.0-1", "banana"} {
		if _, err := Resolve(spec, optionsdb.DefaultBuild); err == nil {
			t.Errorf("Resolve(%q): want an error", spec)
		}
	}
	if _, err := Resolve("latest", optionsdb.Build{Client: "oracle"}); err == nil {
		t.Error("an unknown client library must be an error")
	}
}
