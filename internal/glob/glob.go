// Package glob matches slash-separated paths against patterns with "**".
package glob

import (
	"path"
	"path/filepath"
	"strings"
)

// Match reports whether name matches pattern.
//
//   - "**" matches any number of path segments, including none;
//   - other segments use path.Match syntax (*, ?, [...]);
//   - a pattern without "/" matches the base name at any depth, like
//     .gitignore ("*.cnf", "defaults-file.cnf").
//
// Both are relative; the pattern is slash-separated, the name may use the
// OS separator. A leading "./" is ignored. An invalid pattern matches nothing
// (see Validate).
func Match(pattern, name string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(name))
		return err == nil && ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(p, n []string) bool {
	for len(p) > 0 {
		if p[0] == "**" {
			for len(p) > 0 && p[0] == "**" {
				p = p[1:]
			}
			if len(p) == 0 {
				return true
			}
			for i := 0; i <= len(n); i++ {
				if matchSegments(p, n[i:]) {
					return true
				}
			}
			return false
		}
		if len(n) == 0 {
			return false
		}
		if ok, err := path.Match(p[0], n[0]); err != nil || !ok {
			return false
		}
		p, n = p[1:], n[1:]
	}
	return len(n) == 0
}

// Validate reports a syntax error in pattern.
func Validate(pattern string) error {
	for _, seg := range strings.Split(strings.TrimPrefix(pattern, "./"), "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return err
		}
	}
	return nil
}

// MatchAny reports whether name matches one of the patterns.
func MatchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if Match(p, name) {
			return true
		}
	}
	return false
}
