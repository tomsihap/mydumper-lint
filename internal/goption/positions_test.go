package goption

import (
	"fmt"
	"strings"
	"testing"
)

// TestAppliedPositions pins where each option and its value sit in the
// vector: the model attributes keys swallowed as values with them.
func TestAppliedPositions(t *testing.T) {
	argv := []string{
		"g",
		"--threads", "4", // 1, 2
		"--where=x", // 3
		"-t", "5",   // 4, 5
		"-X", "3", // 6, 7
		"--compress", "-c", // 8, 9: optional values that look like options stay unconsumed
		"--daemongroup-snapshot-count", "7", // 10, 11
		"--d-snapshot-count=8", // 12
	}
	r := mydumperLike(false, CharsetASCII).Parse(argv, nil)
	if !r.OK {
		t.Fatalf("parse: %s", r.Error)
	}
	var got []string
	for _, a := range r.Applied {
		v := "nil"
		if a.Value != nil {
			v = *a.Value
		}
		got = append(got, fmt.Sprintf("%s@%d value=%s@%d alias=%v", a.Name, a.Element, v, a.ValueAt, a.Alias))
	}
	want := []string{
		"--threads@1 value=4@2 alias=false",
		"--where@3 value=x@-1 alias=false",
		"-t@4 value=5@5 alias=false",
		"-X@6 value=3@7 alias=false",
		"--compress@8 value=nil@-1 alias=false",
		"-c@9 value=nil@-1 alias=false",
		"--snapshot-count@10 value=7@11 alias=true",
		"--snapshot-count@12 value=8@-1 alias=true",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("applied:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEdgeCases(t *testing.T) {
	if !strings.HasPrefix(Arg(len(argNames)).String(), "Arg(") || !strings.HasPrefix(Use(len(useNames)).String(), "Use(") {
		t.Error("the first value past each list must be out of range")
	}
	// GLib's alias test needs a non-empty group prefix: "---snapshot-count"
	// is not --<prefix>-snapshot-count.
	c := mydumperLike(false, CharsetASCII)
	if _, _, ok := c.Lookup("-snapshot-count"); ok {
		t.Error("Lookup(-snapshot-count) must fail")
	}
	if r := c.Parse([]string{"g", "---snapshot-count", "3"}, nil); r.OK || r.Error != "Unknown option ---snapshot-count" {
		t.Errorf("---snapshot-count: %+v", r)
	}
	// The C locale refuses every byte from 0x80.
	if Converts("\x80", CharsetASCII) || !Converts("\x7f", CharsetASCII) {
		t.Error("Converts: the C locale boundary is 0x80")
	}
	ints := map[string]int64{"2147483647": 2147483647, "-2147483648": -2147483648, "0xff": 255, "0XaB": 171, "0x7fffffff": 2147483647}
	for s, want := range ints {
		if v, msg := parseInt(s, "--n", 32); msg != "" || v != want {
			t.Errorf("parseInt(%q, 32) = %d, %q", s, v, msg)
		}
	}
	for _, s := range []string{"-2147483649", "0x80000000"} {
		if _, msg := parseInt(s, "--n", 32); !strings.Contains(msg, "out of range") {
			t.Errorf("parseInt(%q, 32) must be out of range: %q", s, msg)
		}
	}
	doubles := map[string]float64{"1e0": 1, "1e9": 1e9, "1e+5": 1e5, "2E-2": 0.02, "1e90": 1e90, "0.0": 0, "0.000": 0, "-0x1.8p1": -3, "2.2250738585072014e-308": 0x1p-1022}
	for s, want := range doubles {
		if v, msg := parseDouble(s, "--d"); msg != "" || v != want {
			t.Errorf("parseDouble(%q) = %v, %q; want %v", s, v, msg, want)
		}
	}
	for _, s := range []string{"1e", "1e+", "nan(abc", "1e-310", "4.9e-324"} {
		if _, msg := parseDouble(s, "--d"); msg == "" {
			t.Errorf("parseDouble(%q) must fail", s)
		}
	}
	if v, _ := parseDouble("-inf", "--d"); v > 0 {
		t.Errorf("-inf = %v", v)
	}
	if v, _ := parseDouble("+Infinity", "--d"); v < 0 {
		t.Errorf("+Infinity = %v", v)
	}
}
