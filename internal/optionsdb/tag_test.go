package optionsdb

import (
	"slices"
	"testing"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		in   string
		want Tag
	}{
		{"v0.19.3-3", Tag{0, 19, 3, 3}},
		{"0.19.3-3", Tag{0, 19, 3, 3}},
		{"v1.0.10-12", Tag{1, 0, 10, 12}},
		{"v0.9.0-0", Tag{0, 9, 0, 0}},
	}
	for _, tt := range tests {
		got, err := ParseTag(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseTag(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{
		"", "v", "0.19", "0.19.3", "v0.19.3-", "0.19-1", "v0.19.3.4-1", "v0.19.3-3-1", "vv0.19.3-3",
		"v0.19.3-a", "v-1.0.0-1", "v0.19.+3-3", "v0.19.3 -3", " v0.19.3-3", "v0.19.3-3 ", "v1234567890.0.0-1",
	} {
		if _, err := ParseTag(bad); err == nil {
			t.Errorf("ParseTag(%q): no error", bad)
		}
	}
}

func TestTagStringAndOrder(t *testing.T) {
	if s := MustParseTag("0.19.3-3").String(); s != "v0.19.3-3" {
		t.Errorf("String = %s", s)
	}
	sorted := []string{"v0.9.9-9", "v0.19.1-1", "v0.19.1-3", "v0.19.3-1", "v0.19.10-1", "v0.20.1-1", "v1.0.0-1", "v1.0.8-1", "v10.0.0-1"}
	for i := range sorted {
		for j := range sorted {
			a, b := MustParseTag(sorted[i]), MustParseTag(sorted[j])
			want := 0
			switch {
			case i < j:
				want = -1
			case i > j:
				want = 1
			}
			if got := a.Compare(b); got != want {
				t.Errorf("%s vs %s = %d, want %d", a, b, got, want)
			}
			if a.Less(b) != (want < 0) {
				t.Errorf("%s.Less(%s) wrong", a, b)
			}
		}
	}
	shuffled := []Tag{MustParseTag("v1.0.0-1"), MustParseTag("v0.19.10-1"), MustParseTag("v0.19.3-1")}
	slices.SortFunc(shuffled, Tag.Compare)
	if shuffled[0].String() != "v0.19.3-1" || shuffled[2].String() != "v1.0.0-1" {
		t.Errorf("sorted = %v", shuffled)
	}
}

func TestMustParseTagPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	MustParseTag("latest")
}

func TestParseParts(t *testing.T) {
	tests := []struct {
		in       string
		n        int
		lowest   string
		matches  []string
		excludes []string
	}{
		{"0.19", 2, "v0.19.0-0", []string{"v0.19.1-1", "v0.19.3-3"}, []string{"v0.20.1-1", "v1.19.1-1"}},
		{"v0.19.3", 3, "v0.19.3-0", []string{"v0.19.3-1", "v0.19.3-3"}, []string{"v0.19.1-1"}},
		{"0.19.3-2", 4, "v0.19.3-2", []string{"v0.19.3-2"}, []string{"v0.19.3-3"}},
	}
	for _, tt := range tests {
		p, err := parseParts(tt.in)
		if err != nil {
			t.Fatalf("%s: %v", tt.in, err)
		}
		if p.n != tt.n || p.lowest().String() != tt.lowest {
			t.Errorf("%s: n=%d lowest=%s", tt.in, p.n, p.lowest())
		}
		for _, m := range tt.matches {
			if !p.matches(MustParseTag(m)) {
				t.Errorf("%s should match %s", tt.in, m)
			}
		}
		for _, m := range tt.excludes {
			if p.matches(MustParseTag(m)) {
				t.Errorf("%s should not match %s", tt.in, m)
			}
		}
	}
	for _, bad := range []string{"0", "v1", "0.19.3.4", "0.19-2", "a.b", "0..3", "0.19.3-"} {
		if _, err := parseParts(bad); err == nil {
			t.Errorf("parseParts(%q): no error", bad)
		}
	}
}
