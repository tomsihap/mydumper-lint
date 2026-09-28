package lint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

// corpusFile is a realistic configuration of about size bytes: tool
// options, connection settings, and many table sections with filters and
// masked columns, with a few problems for the rules to report.
func corpusFile(size int) []byte {
	var b strings.Builder
	b.WriteString("[client]\nhost=db.internal\nuser=backup\n\n[mydumper]\nthreads=8\noutputdir=/backups/app\n" +
		"routines=1\nregex=^app\\.\nchunk-filesize=64\n# nightly dump\n\n")
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "[`app`.`table_%d`]\nwhere=id > %d AND status = 'active'\nrows=10000\n", i, i)
		fmt.Fprintf(&b, "`email`=random_string\n`name`=constant 'x'\n")
		if i%50 == 0 {
			b.WriteString("wehre=1\n  \n") // an unknown key and a whitespace-only line
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func benchLinter(b *testing.B) *Linter {
	db, err := optionsdb.Load()
	if err != nil {
		b.Fatal(err)
	}
	v, err := db.View("v0.19.3-3", optionsdb.DefaultBuild)
	if err != nil {
		b.Fatal(err)
	}
	l, err := New(Config{Selection: rules.Selection{Select: []string{"ALL"}}, Target: v, Version: "v0.19.3-3"})
	if err != nil {
		b.Fatal(err)
	}
	return l
}

// BenchmarkCheck lints a 100 KB file (design §11.9).
func BenchmarkCheck(b *testing.B) {
	l, src := benchLinter(b), corpusFile(100_000)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for b.Loop() {
		l.Check("bench.cnf", src)
	}
}

// BenchmarkFix fixes the same file, self-check included.
func BenchmarkFix(b *testing.B) {
	l, src := benchLinter(b), corpusFile(100_000)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := l.Fix("bench.cnf", src, fix.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
