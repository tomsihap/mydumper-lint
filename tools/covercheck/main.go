// Command covercheck enforces the coverage thresholds of design §11.9 on a
// Go coverage profile: 95 % of statements on the core packages, 90 % on the
// rules, 85 % overall. "Overall" is the product: the packages under
// internal/, except internal/oracletest, the client of the GLib oracle that
// only runs when an oracle is available. Developer tools (tools/) have their
// own tests but no threshold.
//
//	go test -coverprofile=coverage.out ./...
//	go run ./tools/covercheck coverage.out
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

const module = "github.com/tomsihap/mydumper-lint/"

// thresholds are minimum statement coverages, by package.
var thresholds = map[string]float64{
	"internal/preprocess": 95,
	"internal/keyfile":    95,
	"internal/goption":    95,
	"internal/model":      95,
	"internal/fix":        95,
	"internal/rules":      90,
}

const overall = 85.0

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: covercheck coverage.out")
		return 2
	}
	f, err := os.Open(args[0]) //nolint:gosec // G304/G703: the developer's own coverage profile
	if err != nil {
		fmt.Fprintln(stderr, "covercheck:", err)
		return 2
	}
	ok, err := check(f, stdout)
	_ = f.Close() // read-only
	if err != nil {
		fmt.Fprintln(stderr, "covercheck:", err)
		return 2
	}
	if !ok {
		return 1
	}
	return 0
}

type counts struct{ covered, total int }

func (c counts) percent() float64 {
	if c.total == 0 {
		return 100
	}
	return 100 * float64(c.covered) / float64(c.total)
}

// check reads a profile, prints the checked packages and reports whether
// every threshold is met. Blocks listed twice (atomic mode, several test
// binaries) count once, covered if any run covered them.
func check(r io.Reader, w io.Writer) (bool, error) {
	blocks := map[string]struct {
		stmts   int
		covered bool
	}{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return false, fmt.Errorf("malformed line %q", line)
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("malformed line %q", line)
		}
		b := blocks[fields[0]]
		b.stmts, b.covered = stmts, b.covered || count > 0
		blocks[fields[0]] = b
	}
	if err := sc.Err(); err != nil {
		return false, err
	}
	pkgs := map[string]counts{}
	var all counts
	for block, b := range blocks {
		file, _, _ := strings.Cut(block, ":")
		pkg := strings.TrimPrefix(path.Dir(file), module)
		c := pkgs[pkg]
		c.total += b.stmts
		product := strings.HasPrefix(pkg, "internal/") && pkg != "internal/oracletest"
		if product {
			all.total += b.stmts
		}
		if b.covered {
			c.covered += b.stmts
			if product {
				all.covered += b.stmts
			}
		}
		pkgs[pkg] = c
	}
	ok := true
	names := make([]string, 0, len(thresholds))
	for p := range thresholds {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		got, want := pkgs[p].percent(), thresholds[p]
		status := "ok"
		if got < want {
			status, ok = "BELOW", false
		}
		fmt.Fprintf(w, "%-22s %6.1f%%  (min %.0f%%)  %s\n", p, got, want, status)
	}
	status := "ok"
	if all.percent() < overall {
		status, ok = "BELOW", false
	}
	fmt.Fprintf(w, "%-22s %6.1f%%  (min %.0f%%)  %s\n", "internal/... (product)", all.percent(), overall, status)
	return ok, nil
}
