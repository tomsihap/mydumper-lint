//go:build e2e

package e2e

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// outcome is the result of one scenario on one version.
type outcome struct {
	Scenario string
	Version  string
	Status   string // pass, FAIL or skip
	Duration time.Duration
	Fallback bool     // version chosen because no selected version applies
	Note     string   // skip reason or first problem
	Pending  []string // expected rule IDs not registered yet
}

// recorder collects outcomes from parallel subtests.
type recorder struct {
	mu  sync.Mutex
	out []outcome
}

var results recorder

func (r *recorder) add(o outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out = append(r.out, o)
}

func (r *recorder) all() []outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.out)
}

// summary renders the results as Markdown: the images and their guards, a
// scenario × version table, failures, skips and pending rule IDs.
func (r *recorder) summary(s *suite) string {
	out := r.all()
	var b strings.Builder
	b.WriteString("# mydumper-lint end-to-end results\n\n")
	fmt.Fprintf(&b, "Selected versions: %s — %s.\n\n", strings.Join(s.versions, ", "), s.selector)

	// Columns: every version that ran, in version order.
	cols := map[string]bool{}
	for _, o := range out {
		if o.Version != "" { // "" is a scenario that ran on no version
			cols[o.Version] = true
		}
	}
	var versions []string
	for v := range cols {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return optionsdb.MustParseTag(versions[i]).Less(optionsdb.MustParseTag(versions[j]))
	})

	b.WriteString("Images (pinned by the digest `tools/gen-optionsdb -verify-images` recorded):\n\n")
	for _, v := range versions {
		g := s.image(v)
		if g.skip != "" {
			fmt.Fprintf(&b, "- %s: **SKIPPED** — %s\n", v, g.skip)
		} else {
			fmt.Fprintf(&b, "- %s: `%s` — %s\n", v, g.ref, g.desc)
		}
	}

	var pass, fail, skip int
	cell := map[[2]string]string{}
	var scenarios []string
	for _, o := range out {
		key := [2]string{o.Scenario, o.Version}
		c := fmt.Sprintf("%s %.1fs", o.Status, o.Duration.Seconds())
		if o.Status == "skip" {
			c = "skip"
		}
		if o.Fallback {
			c += " †"
		}
		cell[key] = c
		if !slices.Contains(scenarios, o.Scenario) {
			scenarios = append(scenarios, o.Scenario)
		}
		switch o.Status {
		case "pass":
			pass++
		case "FAIL":
			fail++
		default:
			skip++
		}
	}
	sort.Strings(scenarios)
	fmt.Fprintf(&b, "\nRuns: %d passed, %d failed, %d skipped.\n\n", pass, fail, skip)

	b.WriteString("| Scenario | " + strings.Join(versions, " | ") + " |\n")
	b.WriteString("|---|" + strings.Repeat("---|", len(versions)) + "\n")
	for _, sc := range scenarios {
		row := []string{"`" + sc + "`"}
		for _, v := range versions {
			c, ok := cell[[2]string{sc, v}]
			if !ok {
				c = "n/a"
			}
			row = append(row, c)
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	if slices.ContainsFunc(out, func(o outcome) bool { return o.Fallback }) {
		b.WriteString("\n† the scenario applies to none of the selected versions: it ran on the newest " +
			"image-verified version it applies to.\n")
	}

	section := func(title string, keep func(outcome) (string, bool)) {
		var lines []string
		for _, o := range out {
			if text, ok := keep(o); ok {
				v := o.Version
				if v == "" {
					v = "no version"
				}
				lines = append(lines, fmt.Sprintf("- `%s` on %s: %s", o.Scenario, v, text))
			}
		}
		if len(lines) == 0 {
			return
		}
		sort.Strings(lines)
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", title, strings.Join(lines, "\n"))
	}
	section("Failures", func(o outcome) (string, bool) { return o.Note, o.Status == "FAIL" })
	section("Skipped", func(o outcome) (string, bool) { return o.Note, o.Status == "skip" })

	// Pending checks, grouped by scenario: the same list on every version is
	// printed once.
	pending := map[string]map[string][]string{} // scenario → list → versions
	for _, o := range out {
		if len(o.Pending) == 0 || o.Status == "skip" {
			continue
		}
		if pending[o.Scenario] == nil {
			pending[o.Scenario] = map[string][]string{}
		}
		list := strings.Join(o.Pending, ", ")
		pending[o.Scenario][list] = append(pending[o.Scenario][list], o.Version)
	}
	if len(pending) > 0 {
		b.WriteString("\n## Pending lint checks\n\nExpected rule IDs that are not registered yet " +
			"(`loadable`: the linter's verdict, while a pending MDL1xx rule decides it). Logged, not checked.\n\n")
		for _, sc := range sortedKeys(pending) {
			lists := sortedKeys(pending[sc])
			for _, list := range lists {
				where := ""
				if len(lists) > 1 {
					where = " (" + strings.Join(pending[sc][list], ", ") + ")"
				}
				fmt.Fprintf(&b, "- `%s`%s: %s\n", sc, where, list)
			}
		}
	}
	return b.String()
}
