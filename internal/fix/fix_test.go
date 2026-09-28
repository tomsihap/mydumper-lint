package fix

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

func d(rule string, a diag.Applicability, edits ...diag.Edit) diag.Diagnostic {
	return diag.Diagnostic{RuleID: rule, Fix: &diag.Fix{Applicability: a, Edits: edits}}
}

func TestApplyResolvesConflictsByRuleID(t *testing.T) {
	src := []byte("0123456789")
	cands := []diag.Diagnostic{
		d("MDL310", diag.Safe, diag.Edit{Start: 2, End: 4, New: "x"}), // touches MDL104's edit
		d("MDL104", diag.Safe, diag.Edit{Start: 4, End: 4, New: "y"}),
		d("MDL306", diag.Safe, diag.Edit{Start: 8, End: 9}),
		d("MDL102", diag.Safe, diag.Edit{Start: 0, End: 1, New: "A"}, diag.Edit{Start: 6, End: 6, New: "B"}),
	}
	out, n := Apply(src, cands)
	// MDL102 first, then MDL104; MDL306 does not touch anything; MDL310 touches MDL104.
	if string(out) != "A123y45B67"+"9" || n != 3 {
		t.Errorf("Apply = %q, %d", out, n)
	}
}

func TestApplyRejectsSelfOverlappingFix(t *testing.T) {
	out, n := Apply([]byte("abc"), []diag.Diagnostic{d("MDL1", diag.Safe, diag.Edit{Start: 0, End: 2}, diag.Edit{Start: 1, End: 3})})
	if n != 0 || string(out) != "abc" {
		t.Errorf("a fix with overlapping edits must be skipped, got %q, %d", out, n)
	}
}

// lintFunc reports one safe fix per "x" (replaced by "y"), and one unsafe
// fix turning "u" into "v".
func lintFunc(src []byte) []diag.Diagnostic {
	var ds []diag.Diagnostic
	for i, c := range src {
		switch c {
		case 'x':
			ds = append(ds, d("MDL200", diag.Safe, diag.Edit{Start: i, End: i + 1, New: "y"}))
		case 'u':
			ds = append(ds, d("MDL300", diag.Unsafe, diag.Edit{Start: i, End: i + 1, New: "v"}))
		case 'z':
			ds = append(ds, diag.Diagnostic{RuleID: "MDL400"}) // no fix
		}
	}
	return ds
}

func TestFixpoint(t *testing.T) {
	res, err := Fixpoint([]byte("xuxxz"), lintFunc, Options{})
	if err != nil || string(res.Output) != "yuyyz" || res.Applied != 3 || res.UnsafeApplied {
		t.Fatalf("safe only: %+v, %v", res, err)
	}
	if len(res.Remaining) != 2 {
		t.Errorf("remaining = %d", len(res.Remaining))
	}
	res, err = Fixpoint([]byte("xuxxz"), lintFunc, Options{Unsafe: true})
	if err != nil || string(res.Output) != "yvyyz" || !res.UnsafeApplied {
		t.Fatalf("with unsafe: %+v, %v", res, err)
	}
	res, err = Fixpoint([]byte("u"), lintFunc, Options{ExtendSafe: map[string]bool{"MDL300": true}})
	if err != nil || string(res.Output) != "v" || !res.UnsafeApplied {
		t.Fatalf("extend-safe: %+v, %v", res, err)
	}
}

func TestFixpointGivesUpAfterMaxPasses(t *testing.T) {
	// A fix that always re-creates what it fixes never converges.
	flip := func(_ []byte) []diag.Diagnostic {
		return []diag.Diagnostic{d("MDL1", diag.Safe, diag.Edit{Start: 0, End: 0, New: "a"})}
	}
	_, err := Fixpoint([]byte(""), flip, Options{MaxPasses: 3})
	if !errors.Is(err, ErrNoFixpoint) {
		t.Errorf("err = %v", err)
	}
}

func TestSelfCheck(t *testing.T) {
	measure := func(src []byte) (string, model.Health) {
		s := string(src)
		h := model.OK
		if strings.Contains(s, "BAD") {
			h = model.Rejected
		}
		return strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "BAD", ""), h
	}
	if err := SelfCheck([]byte("a b"), []byte("ab"), false, measure); err != nil {
		t.Errorf("formatting-only change must pass: %v", err)
	}
	if err := SelfCheck([]byte("ab"), []byte("ac"), false, measure); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("model change by safe fixes must fail, got %v", err)
	}
	if err := SelfCheck([]byte("ab"), []byte("ac"), true, measure); err != nil {
		t.Errorf("unsafe fixes may change the model: %v", err)
	}
	if err := SelfCheck([]byte("ab"), []byte("abBAD"), true, measure); err == nil || !strings.Contains(err.Error(), "health") {
		t.Errorf("health must never degrade, got %v", err)
	}
	var sce *SelfCheckError
	if err := SelfCheck([]byte("ab"), []byte("ac"), false, measure); !errors.As(err, &sce) || sce.Diff == "" {
		t.Errorf("error must carry a diff: %#v", err)
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.cnf")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(b) != "new" {
		t.Errorf("content = %q", b)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestWriteAtomicFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.cnf")
	link := filepath.Join(dir, "link.cnf")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link must stay a link: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("target content = %q", b)
	}
}

func TestWriteAtomicMissingFile(t *testing.T) {
	if err := WriteAtomic(filepath.Join(t.TempDir(), "nope", "a.cnf"), []byte("x")); err == nil {
		t.Error("writing into a missing directory must fail")
	}
}

// TestFixpointHealthGuard: two unsafe fixes, harmless alone, harmful together.
// The guard keeps the first (lowest rule ID) and drops the second for good.
func TestFixpointHealthGuard(t *testing.T) {
	lint := func(src []byte) []diag.Diagnostic {
		var ds []diag.Diagnostic
		for i, c := range src {
			if c == 'a' || c == 'b' {
				ds = append(ds, diag.Diagnostic{
					RuleID: map[byte]string{'a': "MDL401", 'b': "MDL402"}[c], Message: "lower",
					Span: diag.Span{Start: i, End: i + 1},
					Fix: &diag.Fix{
						Applicability: diag.Unsafe, Description: "upper",
						Edits: []diag.Edit{{Start: i, End: i + 1, New: string(c - 32)}},
					},
				})
			}
		}
		return ds
	}
	health := func(src []byte) model.Health {
		if string(src) == "AB" {
			return model.FatalAtStartup
		}
		return model.OK
	}
	res, err := Fixpoint([]byte("ab"), lint, Options{Unsafe: true, Health: health})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != "Ab" || res.Applied != 1 || len(res.Dropped) != 1 || res.Dropped[0].RuleID != "MDL402" {
		t.Errorf("output %q, applied %d, dropped %+v", res.Output, res.Applied, res.Dropped)
	}
}
