// Package target resolves the mydumper version and build that files are
// linted against (design §5.5, §5.6) from the embedded knowledge base.
package target

import (
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/lint"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// Target is a resolved mydumper version and build.
type Target struct {
	View *optionsdb.View
	// Notice is one line for the user, or "": why another version than the
	// requested one is used, or a reminder to pin the version.
	Notice string
	// Pinned is false when no version was configured.
	Pinned bool
}

// Resolve resolves a --mydumper-version value for a build. An empty spec
// means the latest stable release, with a notice recommending to pin the
// version: behavior differs sharply between versions (F6, F10).
func Resolve(spec string, b optionsdb.Build) (Target, error) {
	db, err := optionsdb.Load()
	if err != nil {
		return Target{}, err
	}
	t := Target{Pinned: spec != ""}
	if !t.Pinned {
		spec = "latest"
	}
	res, err := db.Resolve(spec)
	if err != nil {
		return Target{}, err
	}
	if t.View, err = db.View(res.Version.Tag, b); err != nil {
		return Target{}, err
	}
	t.Notice = res.Warning
	if !t.Pinned && t.Notice == "" {
		t.Notice = fmt.Sprintf("no mydumper version configured: checking against %s, the latest stable release "+
			"mydumper-lint knows; pin yours with --mydumper-version or `mydumper-version:` in .mydumper-lint.yaml",
			res.Version.Tag)
	}
	return t, nil
}

// Version returns the resolved tag, e.g. "v0.19.3-3".
func (t Target) Version() string { return t.View.Version().Tag }

// Configure sets the fields of cfg that depend on the target.
func (t Target) Configure(cfg *lint.Config) {
	v := t.View.Version()
	cfg.Target, cfg.Version, cfg.NoPreprocessor = t.View, v.Tag, !v.Preprocessor
}
