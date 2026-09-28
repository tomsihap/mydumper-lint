package preprocess

import (
	"bytes"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/source"
)

func TestApply(t *testing.T) {
	tests := []struct {
		name, in, real, ideal string
	}{
		{"empty file", "", "", ""},
		{"flag gets a value", "[mydumper]\nroutines\n", "[mydumper]\nroutines= 1\n", "[mydumper]\nroutines= 1\n"},
		{"key=value untouched", "[mydumper]\nthreads=4\n", "[mydumper]\nthreads=4\n", "[mydumper]\nthreads=4\n"},
		{"whitespace-only line breaks", "[mydumper]\nroutines=1\n  \n", "[mydumper]\nroutines=1\n  = 1\n", "[mydumper]\nroutines=1\n  = 1\n"},
		{"whitespace-only last line without newline", "[mydumper]\n\n  ", "[mydumper]\n\n  ", "[mydumper]\n\n  "},
		{"comment without equals", "#x\n", "#x= 1\n", "#x= 1\n"},
		{"crlf blank line", "a=1\r\n\r\n", "a=1\r\n\r= 1\n", "a=1\r\n\r= 1\n"},
		{"crlf flag", "routines\r\n", "routines\r= 1\n", "routines\r= 1\n"},
		{"bracket comment then blank (leak)", "# see [docs]\n\nk=1\n", "# see [docs]\n= 1\nk=1\n", "# see [docs]\n\nk=1\n"},
		{"indented header then blank (leak)", "  [mydumper]\n\nroutines=1\n", "  [mydumper]\n= 1\nroutines=1\n", "  [mydumper]\n\nroutines=1\n"},
		{"leak through a valid header", "# see [docs]\n[myloader]\n\nk=1\n", "# see [docs]\n[myloader]\n= 1\nk=1\n", "# see [docs]\n[myloader]\n\nk=1\n"},
		{"equals before bracket then flag (leak)", "regex=^(a[bc])\nroutines\n", "regex=^(a[bc])\nroutines\n", "regex=^(a[bc])\nroutines= 1\n"},
		{"equals before bracket then blank", "regex=^(a[bc])\n\n", "regex=^(a[bc])\n\n", "regex=^(a[bc])\n\n"},
		{"flag on last line without newline", "[mydumper]\nroutines", "[mydumper]\nroutines", "[mydumper]\nroutines"},
		// The copy after '[' reads the NUL terminator of GLib's buffer when it reaches EOF.
		{"bracket copy reaching EOF appends NUL", "[a]", "[a]\x00", "[a]\x00"},
		{"header at column 0 does not leak", "[a]\n\nk=1\n", "[a]\n\nk=1\n", "[a]\n\nk=1\n"},
		{"locale key leaks", "k[fr]=v\n\n", "k[fr]=v\n= 1\n", "k[fr]=v\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(Apply([]byte(tt.in))); got != tt.real {
				t.Errorf("Apply(%q)\n got %q\nwant %q", tt.in, got, tt.real)
			}
			if got := string(ApplyIdeal([]byte(tt.in))); got != tt.ideal {
				t.Errorf("ApplyIdeal(%q)\n got %q\nwant %q", tt.in, got, tt.ideal)
			}
		})
	}
}

func TestRunLeakChain(t *testing.T) {
	// Case 58: the state frozen by the comment crosses the valid header.
	f := source.New("x.cnf", []byte("# see [docs]\n[myloader]\n\nk=1\n"))
	r := Run(f)
	want := []LineInfo{
		{BracketAt: 6, OutNewLine: false, OutEqualFound: false},
		{BracketAt: 0, StartDirty: true, LeakOrigin: 1, OutNewLine: false, OutEqualFound: false},
		{BracketAt: -1, StartDirty: true, LeakOrigin: 1, AppendsEqOne: true, OutNewLine: true},
		{BracketAt: -1, OutNewLine: true},
	}
	for i := range want {
		if r.Lines[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i+1, r.Lines[i], want[i])
		}
	}
}

func TestRunEqualsLeak(t *testing.T) {
	// Case 12: '=' before '[' freezes equal_found, the next flag gets no "= 1".
	f := source.New("x.cnf", []byte("[mydumper]\nregex=^(a[bc]\\.)\nroutines\nevents=1\n"))
	r := Run(f)
	l := r.Lines[2]
	if !l.StartDirty || l.LeakOrigin != 2 || l.AppendsEqOne || !l.IdealAppendsEqOne {
		t.Errorf("flag line: got %+v", l)
	}
	if !r.Lines[1].OutEqualFound || r.Lines[1].OutNewLine {
		t.Errorf("regex line out state: %+v", r.Lines[1])
	}
	if r.Lines[3].StartDirty {
		t.Errorf("line after a normal line must start clean: %+v", r.Lines[3])
	}
}

func TestRunMatchesApply(t *testing.T) {
	// Rebuilding the pre-processed text from the per-line decisions must give
	// exactly what the byte-level algorithm produces.
	inputs := []string{
		"", "\n", "\n\n", "a", "a\n", "[a]", "[a]\n", "  \n", "  ", "\r\n\r\n",
		"# see [docs]\n[myloader]\n\nk=1\n", "regex=^(a[bc])\nroutines\n\n",
		"x=[\n\n", "[\n\n", "k[fr]=v\n\nroutines\n", "=\n", "\t\n#\n;x\n",
	}
	for _, in := range inputs {
		if got, want := rebuild([]byte(in)), Apply([]byte(in)); !bytes.Equal(got, want) {
			t.Errorf("rebuild(%q) = %q, Apply = %q", in, got, want)
		}
	}
}

// rebuild reconstructs the real pre-processor output from Run's per-line view.
func rebuild(b []byte) []byte {
	f := source.New("x", b)
	r := Run(f)
	var out []byte
	for i, l := range f.Lines {
		out = append(out, f.Content(l.Num)...)
		info := r.Lines[i]
		if info.AppendsEqOne {
			out = append(out, "= 1"...)
		}
		if l.HasNewline {
			out = append(out, '\n')
		} else if info.BracketAt >= 0 {
			out = append(out, 0)
		}
	}
	return out
}

func FuzzRunMatchesApply(f *testing.F) {
	for _, s := range []string{"[mydumper]\nroutines\n", "# [x]\n\n", "a=[\nb\n", "  \n\r\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if got, want := rebuild(b), Apply(b); !bytes.Equal(got, want) {
			t.Fatalf("rebuild(%q) = %q, Apply = %q", b, got, want)
		}
	})
}

func TestPassthrough(t *testing.T) {
	f := source.New("t.cnf", []byte("[g]\nroutines\n# see [x]\n\n"))
	r := Passthrough(f)
	if !r.Passthrough || len(r.Lines) != len(f.Lines) {
		t.Fatalf("passthrough: %+v", r)
	}
	for i, l := range r.Lines {
		if l.AppendsEqOne || l.IdealAppendsEqOne || l.BracketAt != -1 || l.StartDirty || !l.OutNewLine {
			t.Errorf("line %d: %+v", i+1, l)
		}
	}
}
