package lint

import (
	"bytes"
	"errors"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

// allRulesWith builds a linter with every rule, for a mydumper version with
// or without (v0.19.1-x) the pre-processor.
func allRulesWith(t testing.TB, noPreprocessor bool) *Linter {
	l, err := New(Config{Selection: rules.Selection{Select: []string{"ALL"}, Preview: true}, NoPreprocessor: noPreprocessor})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// fuzzLinters covers both loaders without a target, and targets with the
// three behaviors that matter: no pre-processor (v0.19.1-3), unknown options
// fatal (v0.19.3-3) and ignored (v1.0.5-1).
func fuzzLinters(t testing.TB) []*Linter {
	out := []*Linter{allRulesWith(t, false), allRulesWith(t, true)}
	db, err := optionsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"v0.19.1-3", "v0.19.3-3", "v1.0.5-1"} {
		v, err := db.View(tag, optionsdb.DefaultBuild)
		if err != nil {
			t.Fatal(err)
		}
		l, err := New(Config{
			Selection: rules.Selection{Select: []string{"ALL"}, Preview: true},
			Target:    v, Version: tag, NoPreprocessor: !v.Version().Preprocessor,
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

var seeds = []string{
	"[`a`.`b`]\nwehre=1\n`c`=radnom_string\n`d`=apply 'x\n`e=constant y\nwhere=(a\ncolumns_on_select=a,b\ncolumns_on_insert=a\n",
	"[mydumper]\n# see [x]\n# mydumper-lint: disable-next-line=MDL402,MDL407\n\nroutines=0\n# mydumper-lint: disable-file=MDL999\n",
	"[mydumper]\nroutines=-t\nthreads=4\n", "[mydumper]\nroutines=--\nthreads=08\nChunk_Filesize=1 \n",
	"[myloader]\noutputdir=/x\ncompress=-G\ndatabase=caf\xc3\xa9\n", "[mydumper_mysql]\nt=4\nroutines=0\nregex=^(a\n",
	"[mydumper]\nthreads=4\n  \nroutines\n", "\xef\xbb\xbf[mydumper]\nk=v", "[g] # c\n# see [x]\n\n\n\nk=1\r\n\r\n",
	"k=1\n[mydumper]\nregex=^(a[bc])\nroutines\n=x\n", "  [mydumper]\n\n\tthreads=4  \n# c \n\x00x\n\vk=1\n",
	"[a[b]\nfoo]=1\n[`db`.`t`]\n`c[0]`=x\n\n", "[mydumper]\r\nk=v\r\n \r\n", "[mydumper]\nroutines",
}

// FuzzCheck: every input is linted without panicking, with valid, sorted spans.
func FuzzCheck(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	linters := fuzzLinters(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, l := range linters {
			checkDiagnostics(t, b, l.Check("fuzz.cnf", b))
		}
	})
}

func checkDiagnostics(t *testing.T, b []byte, res *Result) {
	t.Helper()
	{
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
	}
}

// FuzzFix: safe fixes never break the self-check invariant (design §7.3) and
// fixing is idempotent, on any input.
func FuzzFix(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	linters := fuzzLinters(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, l := range linters {
			fixOnce(t, l, b)
		}
	})
}

func fixOnce(t *testing.T, l *Linter, b []byte) {
	t.Helper()
	{
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
	}
}

func TestNewRejectsUnknownRules(t *testing.T) {
	if _, err := New(Config{Selection: rules.Selection{Select: []string{"MDL999"}}}); err == nil {
		t.Error("an unknown rule must be an error")
	}
	l := allRulesWith(t, false)
	if len(l.Enabled()) == 0 {
		t.Error("no rule enabled")
	}
}

func TestCheckSet(t *testing.T) {
	l, err := New(Config{Selection: rules.Selection{Select: []string{"MDL603", "MDL509"}, ExtendSelect: []string{"MDL509"}}})
	if err != nil {
		t.Fatal(err)
	}
	defaults := l.Check("defaults.cnf", []byte("[client]\nhost=db\n[mydumper]\nregex=^app\\.users$\n"))
	extra := l.Check("extra.cnf", []byte("[mydumper]\nthreads=4\n[`app`.`orders`]\nwhere=1\n"))
	onDefaults, onExtra := l.CheckSet(defaults, extra)
	var ids []string
	for _, d := range onExtra {
		ids = append(ids, d.RuleID)
	}
	if len(onDefaults) != 0 || len(ids) != 2 || ids[0] != "MDL603" || ids[1] != "MDL509" {
		t.Errorf("defaults %v, extra %v", onDefaults, ids)
	}
}

func TestFixReportsNoFixpoint(t *testing.T) {
	l := allRulesWith(t, false)
	if _, err := l.Fix("x.cnf", []byte("[mydumper]\nthreads=4\n"), fix.Options{MaxPasses: -1}); err != nil {
		t.Errorf("a clean file: %v", err)
	}
}
