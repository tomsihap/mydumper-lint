package main

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	var profile strings.Builder
	profile.WriteString("mode: atomic\n")
	for p := range thresholds {
		profile.WriteString(module + p + "/a.go:1.1,2.1 19 1\n" + module + p + "/a.go:3.1,4.1 1 0\n")
	}
	var out strings.Builder
	ok, err := check(strings.NewReader(profile.String()), &out)
	if err != nil || !ok {
		t.Fatalf("95%% everywhere must pass: %v\n%s", err, out.String())
	}
	low := profile.String() + module + "internal/fix/b.go:1.1,2.1 10 0\n"
	if ok, _ := check(strings.NewReader(low), &out); ok {
		t.Error("internal/fix below 95% must fail")
	}
	if _, err := check(strings.NewReader("x y\n"), &out); err == nil {
		t.Error("a malformed profile must be an error")
	}
}
