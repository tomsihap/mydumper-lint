package lint

import (
	"bytes"
	"errors"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

func allRules(t testing.TB) *Linter {
	l, err := New(Config{Selection: rules.Selection{Select: []string{"ALL"}, Preview: true}})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

var seeds = []string{
	"[mydumper]\nthreads=4\n  \nroutines\n", "\xef\xbb\xbf[mydumper]\nk=v", "[g] # c\n# see [x]\n\n\n\nk=1\r\n\r\n",
	"k=1\n[mydumper]\nregex=^(a[bc])\nroutines\n=x\n", "  [mydumper]\n\n\tthreads=4  \n# c \n\x00x\n\vk=1\n",
	"[a[b]\nfoo]=1\n[`db`.`t`]\n`c[0]`=x\n\n", "[mydumper]\r\nk=v\r\n \r\n", "[mydumper]\nroutines",
}

// FuzzCheck: every input is linted without panicking, with valid, sorted spans.
func FuzzCheck(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	l := allRules(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		res := l.Check("fuzz.cnf", b)
		for i, d := range res.Diagnostics {
			if d.Span.Start < 0 || d.Span.End > len(b) || d.Span.End < d.Span.Start {
				t.Fatalf("%s: span %v out of range for %d bytes", d.RuleID, d.Span, len(b))
			}
			if i > 0 && res.Diagnostics[i-1].Span.Start > d.Span.Start {
				t.Fatalf("diagnostics are not sorted")
			}
			if d.Severity == diag.Off || d.Message == "" {
				t.Fatalf("%s: incomplete diagnostic %+v", d.RuleID, d)
			}
		}
	})
}

// FuzzFix: safe fixes never break the self-check invariant (design §7.3) and
// fixing is idempotent, on any input.
func FuzzFix(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	l := allRules(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, unsafe := range []bool{false, true} {
			res, err := l.Fix("fuzz.cnf", b, fix.Options{Unsafe: unsafe})
			var sce *fix.SelfCheckError
			if errors.As(err, &sce) {
				t.Fatalf("self-check failed (unsafe=%v) on %q: %v\noutput %q", unsafe, b, err, res.Output)
			}
			if err != nil {
				t.Fatalf("fix failed (unsafe=%v) on %q: %v", unsafe, b, err)
			}
			again, err := l.Fix("fuzz.cnf", res.Output, fix.Options{Unsafe: unsafe})
			if err != nil || !bytes.Equal(again.Output, res.Output) {
				t.Fatalf("not idempotent (unsafe=%v): %q -> %q -> %q (%v)", unsafe, b, res.Output, again.Output, err)
			}
			// A fixed file that GLib rejected must now load when only
			// loadability issues with safe fixes were present; at least it
			// must never load worse.
			_, before := l.Measure(b)
			_, after := l.Measure(res.Output)
			if after < before {
				t.Fatalf("health degraded from %v to %v on %q", before, after, b)
			}
			if before == model.OK && !l.Check("fuzz.cnf", res.Output).KF.Loadable {
				t.Fatalf("fixing a loadable file made it unloadable: %q -> %q", b, res.Output)
			}
		}
	})
}
