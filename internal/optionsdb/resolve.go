package optionsdb

import (
	"fmt"
	"strings"
)

// Resolution is the outcome of Resolve.
type Resolution struct {
	// Version is the embedded version to lint against.
	Version *Version
	// Exact is true when Version satisfies the request itself: the tag
	// asked for, or the version a partial spec or alias designates. It is
	// false when Resolve had to substitute another version.
	Exact bool
	// Warning explains a substitution; empty when Exact.
	Warning string
}

// Resolve maps a --mydumper-version value to an embedded version (design
// §5.6):
//
//	v0.19.3-3, 0.19.3-3   that tag
//	0.19.3                the latest build of 0.19.3 (highest revision)
//	0.19                  the latest stable 0.19.x release
//	latest                the latest stable release
//	latest-prerelease     the newest tag, pre-release or not
//
// A version missing from the knowledge base resolves to the nearest lower
// embedded version, one newer than every embedded version to the newest,
// both with a warning. A version older than the oldest embedded one is an
// error.
func (db *DB) Resolve(spec string) (Resolution, error) {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "latest":
		for i := len(db.Versions) - 1; i >= 0; i-- {
			if !db.Versions[i].Prerelease {
				return Resolution{Version: &db.Versions[i], Exact: true}, nil
			}
		}
		n := db.Newest()
		return Resolution{Version: n, Warning: fmt.Sprintf("no stable mydumper release is embedded; using pre-release %s", n.Tag)}, nil
	case "latest-prerelease":
		return Resolution{Version: db.Newest(), Exact: true}, nil
	}
	p, err := parseParts(spec)
	if err != nil {
		return Resolution{}, fmt.Errorf("%w, latest or latest-prerelease", err)
	}

	// embedded versions of the requested series
	best, bestStable := -1, -1
	for i, t := range db.idx.tags {
		if !p.matches(t) {
			continue
		}
		best = i
		if !db.Versions[i].Prerelease {
			bestStable = i
		}
	}
	switch {
	case p.n == 2 && bestStable >= 0:
		return Resolution{Version: &db.Versions[bestStable], Exact: true}, nil
	case p.n == 2 && best >= 0:
		v := &db.Versions[best]
		return Resolution{Version: v, Warning: fmt.Sprintf("no stable mydumper %s release is embedded; using pre-release %s", p, v.Tag)}, nil
	case best >= 0:
		return Resolution{Version: &db.Versions[best], Exact: true}, nil
	}

	lo := p.lowest()
	if newest := db.idx.tags[len(db.idx.tags)-1]; newest.Less(lo) {
		n := db.Newest()
		return Resolution{Version: n, Warning: fmt.Sprintf(
			"mydumper %s is newer than every version mydumper-lint knows; using %s (mydumper-lint may be outdated)", p, n.Tag)}, nil
	}
	for i := len(db.idx.tags) - 1; i >= 0; i-- {
		if db.idx.tags[i].Less(lo) {
			v := &db.Versions[i]
			return Resolution{Version: v, Warning: fmt.Sprintf(
				"mydumper %s is not in the knowledge base; using %s, the nearest lower version", p, v.Tag)}, nil
		}
	}
	return Resolution{}, fmt.Errorf("mydumper %s is older than %s, the oldest version mydumper-lint knows", p, db.Oldest().Tag)
}
