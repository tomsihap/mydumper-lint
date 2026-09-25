package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// candidate is an upstream tag at or after the minimum version.
type candidate struct {
	release
	embedded bool
	reason   string // why a tag is skipped
}

// imageChecker tells whether an official image exists for a tag.
type imageChecker func(tag string) (bool, error)

// selectReleases lists the tags at or after minTag, attaches their GitHub
// release status, and decides which ones to embed: every tag with a GitHub
// release object or an official image (design D3). It also returns the tags
// whose pre-release status disagrees with upstream's own rule ("an even
// PATCH is a pre-release").
func selectReleases(gh *githubCache, minTag optionsdb.Tag, hasImage imageChecker) ([]candidate, []string, error) {
	releases := map[string]ghRelease{}
	for _, r := range gh.Releases {
		if !r.Draft {
			releases[r.Tag] = r
		}
	}
	var cands []candidate
	seen := map[string]bool{}
	for _, t := range gh.Tags {
		parsed, err := optionsdb.ParseTag(t.Name)
		if err != nil || parsed.String() != t.Name || parsed.Less(minTag) {
			continue // not a release tag, or too old
		}
		if seen[t.Name] {
			return nil, nil, fmt.Errorf("tag %s listed twice", t.Name)
		}
		seen[t.Name] = true
		c := candidate{release: release{tag: t.Name, parsed: parsed, commit: t.Commit, prerelease: true}}
		if r, ok := releases[t.Name]; ok {
			c.hasRelease = true
			c.prerelease = r.Prerelease
			if len(r.PublishedAt) >= 10 {
				c.date = r.PublishedAt[:10]
			}
		}
		cands = append(cands, c)
	}
	slices.SortFunc(cands, func(a, b candidate) int { return a.parsed.Compare(b.parsed) })

	var parity []string
	for i := range cands {
		c := &cands[i]
		if !c.hasRelease {
			ok, err := hasImage(c.tag)
			if err != nil {
				return nil, nil, err
			}
			c.image = ok
		}
		c.embedded = c.hasRelease || c.image
		if !c.embedded {
			c.reason = "no GitHub release and no official image"
		}
		if !c.embedded {
			continue
		}
		byParity := c.parsed.Patch%2 == 0
		if byParity != c.prerelease {
			what := "GitHub release flagged pre-release"
			if !c.hasRelease {
				what = "no GitHub release (pre-release)"
			} else if !c.prerelease {
				what = "stable GitHub release"
			}
			rule := "stable"
			if byParity {
				rule = "pre-release"
			}
			parity = append(parity, fmt.Sprintf("%s: %s; upstream's even-PATCH rule says %s (the GitHub status wins)", c.tag, what, rule))
		}
	}
	return cands, parity, nil
}

// describe renders a candidate for the version table.
func (c candidate) describe(verified bool) string {
	status := "stable"
	if c.prerelease {
		status = "pre-release"
	}
	var src []string
	if c.hasRelease {
		src = append(src, "release "+c.date)
	} else {
		src = append(src, "no release")
	}
	if !c.hasRelease {
		if c.image {
			src = append(src, "image")
		} else {
			src = append(src, "no image")
		}
	}
	state := "embedded"
	if !c.embedded {
		state = "SKIPPED: " + c.reason
	} else if verified {
		state += ", image verified"
	} else {
		state += ", image not verified"
	}
	return fmt.Sprintf("%-11s %-12s %-24s %s", c.tag, status, strings.Join(src, ", "), state)
}
