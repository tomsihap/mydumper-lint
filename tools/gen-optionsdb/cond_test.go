package main

import "testing"

func TestMinimalCondition(t *testing.T) {
	// bit 0 = WITH_SSL, bit 1 = LIBMARIADB, bit 2 = HAVE_MY_BOOL
	tests := []struct {
		name     string
		minterms []int
		want     string
	}{
		{"always", []int{0, 1, 2, 3, 4, 5, 6, 7}, ""},
		{"ssl", []int{1, 3, 5, 7}, "WITH_SSL"},
		{"no ssl", []int{0, 2, 4, 6}, "!WITH_SSL"},
		{"ssl and mysql client", []int{1, 5}, "WITH_SSL && !LIBMARIADB"},
		{"either", []int{1, 2, 3, 5, 6, 7}, "LIBMARIADB || WITH_SSL"},
		{"xor", []int{1, 2, 5, 6}, "!WITH_SSL && LIBMARIADB || WITH_SSL && !LIBMARIADB"},
		{"one configuration", []int{7}, "WITH_SSL && LIBMARIADB && HAVE_MY_BOOL"},
		{"unordered input", []int{7, 5, 3, 1}, "WITH_SSL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimalCondition(tt.minterms, len(knownMacros)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAllConfigsOrder(t *testing.T) {
	cs := allConfigs()
	if len(cs) != 1<<len(knownMacros) {
		t.Fatalf("%d configs", len(cs))
	}
	for i, c := range cs {
		for b, m := range knownMacros {
			if c[m] != (i&(1<<b) != 0) {
				t.Fatalf("config %d: %s = %v", i, m, c[m])
			}
		}
	}
	off := cs[officialConfigIndex()]
	if !off["WITH_SSL"] || off["LIBMARIADB"] || off["HAVE_MY_BOOL"] {
		t.Error("official configuration is not WITH_SSL && !LIBMARIADB && !HAVE_MY_BOOL")
	}
}
