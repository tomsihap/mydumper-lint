package main

import (
	"strings"
	"testing"
)

// cfgOf builds a configuration from the names of the macros that are set.
func cfgOf(set ...string) config {
	c := config{}
	for _, m := range knownMacros {
		c[m] = false
	}
	for _, m := range set {
		c[m] = true
	}
	return c
}

// truthOf returns the condition of the code token with this text.
func truthOf(t *testing.T, u *unit, text string, cfg config) tri {
	t.Helper()
	for i, r := range u.code {
		if u.raw[r].text == text {
			return u.conds[i].truth(cfg)
		}
	}
	t.Fatalf("token %q not found", text)
	return triUnknown
}

const condSrc = `#ifndef SYNTH_H
#define SYNTH_H
int a;
#ifdef WITH_SSL
int b;
# ifdef LIBMARIADB
int c;
# else
int d;
# endif
#endif
#if SOME_CHECK(2, 68, 0)
int e;
#elif defined(HAVE_MY_BOOL)
int f;
#else
int g;
#endif
#if 0
int h;
#endif
#if !defined(WITH_SSL) || defined LIBMARIADB
int k;
#endif
#endif
`

func TestConditions(t *testing.T) {
	u, err := newUnit("synth.h", []byte(condSrc))
	if err != nil {
		t.Fatal(err)
	}
	T, F, U := triTrue, triFalse, triUnknown
	tests := []struct {
		tok  string
		cfg  config
		want tri
	}{
		{"a", cfgOf(), T}, // include guard is not a condition
		{"b", cfgOf("WITH_SSL"), T},
		{"b", cfgOf(), F},
		{"c", cfgOf("WITH_SSL", "LIBMARIADB"), T},
		{"c", cfgOf("WITH_SSL"), F},
		{"d", cfgOf("WITH_SSL"), T},
		{"d", cfgOf("WITH_SSL", "LIBMARIADB"), F},
		{"d", cfgOf("LIBMARIADB"), F},
		{"e", cfgOf("WITH_SSL"), U}, // function-like macro: undecidable
		{"f", cfgOf(), F},
		{"f", cfgOf("HAVE_MY_BOOL"), U},
		{"g", cfgOf(), U},
		{"g", cfgOf("HAVE_MY_BOOL"), F},
		{"h", cfgOf("WITH_SSL", "LIBMARIADB", "HAVE_MY_BOOL"), F},
		{"k", cfgOf(), T},
		{"k", cfgOf("WITH_SSL"), F},
		{"k", cfgOf("WITH_SSL", "LIBMARIADB"), T},
	}
	for _, tt := range tests {
		if got := truthOf(t, u, tt.tok, tt.cfg); got != tt.want {
			t.Errorf("%s under %s = %d, want %d", tt.tok, tt.cfg, got, tt.want)
		}
	}
}

func TestIncludeGuards(t *testing.T) {
	tests := []struct {
		name, src string
		guard     bool
	}{
		{"ifndef", "#ifndef X_H\n#define X_H\nint a;\n#endif\n", true},
		{"if not defined", "#if !defined(X_H)\n#define X_H\nint a;\n#endif\n", true},
		{"if not defined bare", "#if !defined X_H\n#define X_H\nint a;\n#endif\n", true},
		{"define of another name", "#ifndef X_H\n#define Y_H\nint a;\n#endif\n", false},
		{"known macro never a guard", "#ifndef WITH_SSL\n#define WITH_SSL\nint a;\n#endif\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := newUnit("x.h", []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			got := truthOf(t, u, "a", cfgOf("WITH_SSL")) == triTrue
			if tt.name == "known macro never a guard" {
				got = truthOf(t, u, "a", cfgOf()) == triTrue && truthOf(t, u, "a", cfgOf("WITH_SSL")) == triFalse
				if !got {
					t.Error("#ifndef WITH_SSL must stay a condition")
				}
				return
			}
			if got != tt.guard {
				t.Errorf("guard = %v, want %v", got, tt.guard)
			}
		})
	}
}

func TestDirectiveErrors(t *testing.T) {
	for _, src := range []string{
		"#endif\n",
		"#else\n",
		"#elif X\n",
		"#if X\nint a;\n",
		"#if X\n#else\n#else\n#endif\n",
		"#if\n#endif\n",
		"#ifdef\n#endif\n",
		"#if (A\n#endif\n",
		"#if defined(\n#endif\n",
	} {
		if _, err := newUnit("x.c", []byte(src)); err == nil {
			t.Errorf("newUnit(%q): no error", src)
		}
	}
}

func TestStringDefines(t *testing.T) {
	src := "#define WHERE \"where\"\n#define CAT \"a\" \"b\"\n#define NUM 5\n#define FN(x) \"x\"\n#ifdef WITH_SSL\n#define S \"s\"\n#endif\n"
	u, err := newUnit("x.h", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range u.strDefs {
		cond := "always"
		if d.cond != nil {
			cond = d.cond.expr.String()
		}
		got = append(got, d.name+"="+d.value+"@"+cond)
	}
	want := "WHERE=where@always CAT=ab@always S=s@defined(WITH_SSL)"
	if strings.Join(got, " ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, " "), want)
	}
}

func TestExprParser(t *testing.T) {
	tests := []struct{ src, want string }{
		{"defined(A) && !defined B || (C)", "((defined(A) && !(defined(B))) || C)"},
		{"A == 1", "A == 1"},
		{"X(1, (2)) && B", "(X ( 1 , ( 2 ) ) && B)"},
		{"1", "1"},
		{"0x0", "0"},
		{"A ? B : C", "A ? B : C"},
		{"-1 < A", "- 1 < A"},
	}
	for _, tt := range tests {
		ts, err := lex([]byte(tt.src))
		if err != nil {
			t.Fatal(err)
		}
		e, err := (&exprParser{toks: ts}).parse()
		if err != nil {
			t.Errorf("%q: %v", tt.src, err)
			continue
		}
		if e.String() != tt.want {
			t.Errorf("%q parsed as %s, want %s", tt.src, e, tt.want)
		}
	}
}
