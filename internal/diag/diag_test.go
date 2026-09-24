package diag

import (
	"reflect"
	"testing"
)

func TestSeverityRoundTrip(t *testing.T) {
	for _, s := range []Severity{Off, Info, Warning, Error} {
		got, err := ParseSeverity(s.String())
		if err != nil || got != s {
			t.Errorf("ParseSeverity(%q) = %v, %v; want %v", s.String(), got, err, s)
		}
	}
	if _, err := ParseSeverity("fatal"); err == nil {
		t.Error("ParseSeverity(\"fatal\") should fail")
	}
}

func TestSeverityAtLeast(t *testing.T) {
	if !Error.AtLeast(Warning) || Warning.AtLeast(Error) || !Info.AtLeast(Info) {
		t.Error("AtLeast ordering is wrong")
	}
	if Off.AtLeast(Info) {
		t.Error("Off must never reach a threshold")
	}
}

func TestApplicabilityString(t *testing.T) {
	if Safe.String() != "safe" || Unsafe.String() != "unsafe" {
		t.Errorf("got %q %q", Safe, Unsafe)
	}
}

func TestSortOrdersBySpanThenRule(t *testing.T) {
	ds := []Diagnostic{
		{RuleID: "MDL302", Span: Span{10, 12}},
		{RuleID: "MDL102", Span: Span{10, 12}},
		{RuleID: "MDL101", Span: Span{0, 3}},
		{RuleID: "MDL310", Span: Span{10, 11}},
	}
	Sort(ds)
	var got []string
	for _, d := range ds {
		got = append(got, d.RuleID)
	}
	want := []string{"MDL101", "MDL310", "MDL102", "MDL302"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sort = %v, want %v", got, want)
	}
}

func TestSpanHelpers(t *testing.T) {
	s := Span{Start: 3, End: 7}
	if s.Len() != 4 || s.Empty() || !(Span{5, 5}).Empty() {
		t.Error("Len/Empty wrong")
	}
	if !s.Overlaps(Span{6, 9}) || s.Overlaps(Span{7, 9}) || !s.Touches(Span{7, 9}) || s.Touches(Span{8, 9}) {
		t.Error("Overlaps/Touches wrong")
	}
	// An insertion at an edge touches but does not overlap.
	if (Span{3, 3}).Overlaps(s) || !(Span{3, 3}).Touches(s) {
		t.Error("insertion at the edge must touch, not overlap")
	}
}
