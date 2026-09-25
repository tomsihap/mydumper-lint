package textdiff

import "testing"

func TestDiff(t *testing.T) {
	if d := Diff("a", []byte("x\n"), "b", []byte("x\n")); d != nil {
		t.Errorf("identical inputs must give no diff, got %q", d)
	}
	got := string(Diff("x.cnf", []byte("[g]\n  \nk=v\n"), "x.cnf", []byte("[g]\n\nk=v\n")))
	want := "diff x.cnf x.cnf\n--- x.cnf\n+++ x.cnf\n@@ -1,3 +1,3 @@\n [g]\n-  \n+\n k=v\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// A missing final newline is shown explicitly.
	got = string(Diff("x.cnf", []byte("[g]\nk=v"), "x.cnf", []byte("[g]\nk=v\n")))
	want = "diff x.cnf x.cnf\n--- x.cnf\n+++ x.cnf\n@@ -1,2 +1,2 @@\n [g]\n-k=v\n\\ No newline at end of file\n+k=v\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}
