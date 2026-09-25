package optionsdb

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// Tag is a parsed mydumper version tag, vMAJOR.MINOR.PATCH-REV. Upstream
// bumps REV for rebuilds of the same PATCH (v0.19.3-1, v0.19.3-2, …).
type Tag struct {
	Major, Minor, Patch, Rev int
}

// ParseTag parses "v0.19.3-3" or "0.19.3-3".
func ParseTag(s string) (Tag, error) {
	p, err := parseParts(s)
	if err != nil || p.n != 4 {
		return Tag{}, fmt.Errorf("invalid mydumper version tag %q (want vMAJOR.MINOR.PATCH-REV)", s)
	}
	return p.tag, nil
}

// MustParseTag is ParseTag for constants; it panics on error.
func MustParseTag(s string) Tag {
	t, err := ParseTag(s)
	if err != nil {
		panic(err)
	}
	return t
}

// String returns the canonical form, with the leading "v".
func (t Tag) String() string {
	return fmt.Sprintf("v%d.%d.%d-%d", t.Major, t.Minor, t.Patch, t.Rev)
}

// Compare returns -1, 0 or +1 depending on whether t sorts before, equal to
// or after u.
func (t Tag) Compare(u Tag) int {
	return cmp.Or(cmp.Compare(t.Major, u.Major), cmp.Compare(t.Minor, u.Minor),
		cmp.Compare(t.Patch, u.Patch), cmp.Compare(t.Rev, u.Rev))
}

// Less reports whether t sorts before u.
func (t Tag) Less(u Tag) bool { return t.Compare(u) < 0 }

// parts is a possibly partial version: MAJOR.MINOR, MAJOR.MINOR.PATCH or
// MAJOR.MINOR.PATCH-REV (n = 2, 3 or 4 components).
type parts struct {
	tag Tag
	n   int
}

func parseParts(s string) (parts, error) {
	bad := fmt.Errorf("invalid mydumper version %q (want vMAJOR.MINOR.PATCH-REV, MAJOR.MINOR.PATCH or MAJOR.MINOR)", s)
	body := strings.TrimPrefix(s, "v")
	rev := ""
	if i := strings.IndexByte(body, '-'); i >= 0 {
		body, rev = body[:i], body[i+1:]
		if rev == "" {
			return parts{}, bad
		}
	}
	fields := strings.Split(body, ".")
	if len(fields) < 2 || len(fields) > 3 || (rev != "" && len(fields) != 3) {
		return parts{}, bad
	}
	nums := make([]int, 0, 4)
	for _, f := range append(fields, rev) {
		if f == "" && len(nums) == len(fields) {
			break // no revision
		}
		n, err := parseNumber(f)
		if err != nil {
			return parts{}, bad
		}
		nums = append(nums, n)
	}
	p := parts{n: len(nums)}
	dst := []*int{&p.tag.Major, &p.tag.Minor, &p.tag.Patch, &p.tag.Rev}
	for i, n := range nums {
		*dst[i] = n
	}
	return p, nil
}

// parseNumber accepts decimal digits only (no sign, no spaces), at most 9.
func parseNumber(s string) (int, error) {
	if s == "" || len(s) > 9 {
		return 0, fmt.Errorf("bad number %q", s)
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad number %q", s)
		}
	}
	return strconv.Atoi(s)
}

// matches reports whether t belongs to the series p denotes.
func (p parts) matches(t Tag) bool {
	switch p.n {
	case 2:
		return t.Major == p.tag.Major && t.Minor == p.tag.Minor
	case 3:
		return t.Major == p.tag.Major && t.Minor == p.tag.Minor && t.Patch == p.tag.Patch
	}
	return t == p.tag
}

// lowest is the smallest tag of the series.
func (p parts) lowest() Tag {
	t := p.tag
	switch p.n {
	case 2:
		t.Patch, t.Rev = 0, 0
	case 3:
		t.Rev = 0
	}
	return t
}

func (p parts) String() string {
	switch p.n {
	case 2:
		return fmt.Sprintf("%d.%d", p.tag.Major, p.tag.Minor)
	case 3:
		return fmt.Sprintf("%d.%d.%d", p.tag.Major, p.tag.Minor, p.tag.Patch)
	}
	return p.tag.String()
}
