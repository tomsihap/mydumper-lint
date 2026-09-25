package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

func TestSelectReleases(t *testing.T) {
	gh := &githubCache{
		Tags: []ghTag{
			{"v0.9.0-1", "c0"}, {"v1.0.1-1", "c1"}, {"v1.0.0-1", "c2"}, {"v1.0.2-1", "c3"},
			{"v1.0.3-2", "c4"}, {"v1.0.4-1", "c5"}, {"v1.0.5-1", "c6"}, {"not-a-version", "c7"}, {"v1.0.6-1", "c8"},
		},
		Releases: []ghRelease{
			{Tag: "v1.0.1-1", PublishedAt: "2026-01-02T10:00:00Z"},
			{Tag: "v1.0.2-1", Prerelease: true, PublishedAt: "2026-01-03T10:00:00Z"},
			{Tag: "v1.0.3-2", Prerelease: true, PublishedAt: "2026-01-04T10:00:00Z"},
			{Tag: "v1.0.6-1", Draft: true},
		},
	}
	images := map[string]bool{"v1.0.0-1": true, "v1.0.5-1": true}
	var asked []string
	cands, parity, err := selectReleases(gh, optionsdb.MustParseTag("v1.0.0-1"), func(tag string) (bool, error) {
		asked = append(asked, tag)
		return images[tag], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cands {
		got = append(got, c.describe(false))
	}
	want := []string{
		"v1.0.0-1    pre-release  no release, image        embedded, image not verified",
		"v1.0.1-1    stable       release 2026-01-02       embedded, image not verified",
		"v1.0.2-1    pre-release  release 2026-01-03       embedded, image not verified",
		"v1.0.3-2    pre-release  release 2026-01-04       embedded, image not verified",
		"v1.0.4-1    pre-release  no release, no image     SKIPPED: no GitHub release and no official image",
		"v1.0.5-1    pre-release  no release, image        embedded, image not verified",
		"v1.0.6-1    pre-release  no release, no image     SKIPPED: no GitHub release and no official image",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("candidates:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Docker Hub is only asked about tags without a (non-draft) release.
	if strings.Join(asked, " ") != "v1.0.0-1 v1.0.4-1 v1.0.5-1 v1.0.6-1" {
		t.Errorf("asked Docker Hub about %v", asked)
	}
	// v1.0.3-2 has an odd PATCH but is flagged pre-release; v1.0.5-1 has no
	// release. v1.0.0-1 and v1.0.2-1 agree with the even-PATCH rule.
	if len(parity) != 2 || !strings.HasPrefix(parity[0], "v1.0.3-2: GitHub release flagged pre-release") ||
		!strings.HasPrefix(parity[1], "v1.0.5-1: no GitHub release") {
		t.Errorf("parity = %q", parity)
	}
	if cands[1].commit != "c1" || cands[1].date != "2026-01-02" {
		t.Errorf("v1.0.1-1 = %+v", cands[1])
	}
}

func TestSelectReleasesErrors(t *testing.T) {
	dup := &githubCache{Tags: []ghTag{{"v1.0.0-1", "a"}, {"v1.0.0-1", "b"}}}
	if _, _, err := selectReleases(dup, optionsdb.MustParseTag("v1.0.0-1"), func(string) (bool, error) { return true, nil }); err == nil {
		t.Error("duplicate tag: no error")
	}
	one := &githubCache{Tags: []ghTag{{"v1.0.0-1", "a"}}}
	boom := errors.New("boom")
	if _, _, err := selectReleases(one, optionsdb.MustParseTag("v1.0.0-1"), func(string) (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Errorf("image check error lost: %v", err)
	}
}
