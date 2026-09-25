package main

import (
	"slices"
	"testing"
)

func TestCMakeExecutables(t *testing.T) {
	src := `cmake_minimum_required(VERSION 3.5) # comment (with parens)
SET( COMMON a.c b.c )
set(ONE_SRCS one.c ${COMMON})
list(APPEND ONE_SRCS extra.c)
if (SOMETHING AND (OTHER))
  message(STATUS "value = ${COMMON}")
endif()
add_executable(one ${ONE_SRCS})
add_executable(two WIN32 two.c "quoted file.c")
target_sources(two PRIVATE late.c)
`
	got, err := cmakeExecutables(src)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one.c", "a.c", "b.c", "extra.c"}; !slices.Equal(got["one"], want) {
		t.Errorf("one = %q, want %q", got["one"], want)
	}
	if want := []string{"two.c", "quoted file.c", "late.c"}; !slices.Equal(got["two"], want) {
		t.Errorf("two = %q, want %q", got["two"], want)
	}
}

func TestCMakeErrors(t *testing.T) {
	for _, src := range []string{"set(A b", "set A b)", `set(A "open)`, "(x)"} {
		if _, err := cmakeExecutables(src); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
}
