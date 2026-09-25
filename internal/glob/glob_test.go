package glob

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.cnf", "a.cnf", true},
		{"**/*.cnf", "x/y/a.cnf", true},
		{"**/*.cnf", "x/y/a.cnf.bak", false},
		{"*.cnf", "deep/dir/a.cnf", true}, // no slash: base name at any depth
		{"defaults-file.cnf", "etc/defaults-file.cnf", true},
		{"data-*.cnf", "etc/data-app.cnf", true},
		{"legacy/*.cnf", "legacy/a.cnf", true},
		{"legacy/*.cnf", "x/legacy/a.cnf", false},
		{"**/legacy/*.cnf", "x/legacy/a.cnf", true},
		{"a/**", "a/b/c", true},
		{"a/**/c", "a/c", true},
		{"a/**/**/c", "a/x/y/c", true},
		{"./a/*.cnf", "a/b.cnf", true},
		{"a/*.cnf", "./a/b.cnf", true},
		{"[", "x", false}, // invalid pattern
		{"a/[/b", "a/x/b", false},
	}
	for _, tt := range tests {
		if got := Match(tt.pattern, tt.name); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestValidate(t *testing.T) {
	if Validate("**/*.cnf") != nil || Validate("a/[b-c]/**") != nil {
		t.Error("valid patterns rejected")
	}
	if Validate("a/[") == nil {
		t.Error("invalid pattern accepted")
	}
}

func TestMatchAny(t *testing.T) {
	if !MatchAny([]string{"*.bak", "**/*.cnf"}, "x/a.cnf") || MatchAny(nil, "a") {
		t.Error("MatchAny wrong")
	}
}
