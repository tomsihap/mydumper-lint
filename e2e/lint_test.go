//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tomsihap/mydumper-lint/internal/rules"
)

// lintReport is the part of the JSON output (schemas/output.v1.json) the
// harness reads.
type lintReport struct {
	Files []struct {
		Path            string `json:"path"`
		Loadable        bool   `json:"loadable"`
		MydumperVersion string `json:"mydumper_version"`
		Diagnostics     []struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Range    struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
		} `json:"diagnostics"`
	} `json:"files"`
}

// lintOutcome is the comparison of the linter with a plan.
type lintOutcome struct {
	Command  string
	Got      []string // rule IDs reported, sorted
	Pending  []string // expected rule IDs that are not registered yet
	Problems []string // mismatches; empty when the linter agrees
	Details  string   // every diagnostic, for logs
}

// splitRegistered separates the rule IDs mydumper-lint implements from the
// ones the design names but that are not registered yet.
func splitRegistered(ids []string) (registered, pending []string) {
	for _, id := range ids {
		if _, ok := rules.Lookup(id); ok {
			registered = append(registered, id)
		} else {
			pending = append(pending, id)
		}
	}
	return registered, pending
}

// lint runs `mydumper-lint check --format json` on the scenario's files for
// the version and compares the reported rule IDs with the plan. Pending IDs
// are only recorded. When the plan observes config-loaded, the linter's
// "loadable" verdict must agree, unless a pending MDL1xx rule is expected
// (loadability is what MDL1xx rules decide, design §6.2).
func (s *suite) lint(ctx context.Context, sc *Scenario, dir, tag string, p plan) (lintOutcome, error) {
	var out lintOutcome
	registered, pending := splitRegistered(p.Lint)
	out.Pending = pending

	args := []string{"check", "--format", "json", "--mydumper-version", tag, "--preview", "--color", "never"}
	if slices.ContainsFunc(sc.Files, func(f scenarioFile) bool { return f.Name == lintConfigFile }) {
		args = append(args, "--config", filepath.Join(dir, lintConfigFile))
	} else {
		args = append(args, "--no-config")
	}
	if len(registered) > 0 {
		args = append(args, "--extend-select", strings.Join(registered, ",")) // opt-in rules
	}
	for _, f := range sc.LintFiles {
		args = append(args, filepath.Join(dir, f))
	}
	out.Command = "mydumper-lint " + strings.Join(args, " ")

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.lintBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return out, fmt.Errorf("%s: %w: %s", out.Command, err, strings.TrimSpace(stderr.String()))
	}
	var rep lintReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		return out, fmt.Errorf("%s: invalid JSON: %w", out.Command, err)
	}

	got := map[string]bool{}
	loadable := map[string]bool{}
	var details strings.Builder
	for _, f := range rep.Files {
		name := filepath.Base(f.Path)
		loadable[name] = f.Loadable
		if f.MydumperVersion != tag {
			out.Problems = append(out.Problems, fmt.Sprintf("%s was linted against %s, not %s", name, f.MydumperVersion, tag))
		}
		fmt.Fprintf(&details, "  %s: loadable=%v\n", name, f.Loadable)
		for _, d := range f.Diagnostics {
			got[d.ID] = true
			fmt.Fprintf(&details, "    %s:%d %s %s %s\n", name, d.Range.Start.Line, d.Severity, d.ID, d.Message)
		}
	}
	out.Details = details.String()
	for id := range got {
		out.Got = append(out.Got, id)
	}
	sort.Strings(out.Got)
	if len(rep.Files) != len(sc.LintFiles) {
		out.Problems = append(out.Problems, fmt.Sprintf("linted %d files, want %d", len(rep.Files), len(sc.LintFiles)))
	}

	var missing, unexpected []string
	for _, id := range registered {
		if !got[id] {
			missing = append(missing, id)
		}
	}
	for _, id := range out.Got {
		if !slices.Contains(registered, id) {
			unexpected = append(unexpected, id)
		}
	}
	if len(missing) > 0 {
		out.Problems = append(out.Problems, "expected but not reported: "+strings.Join(missing, ", "))
	}
	if len(unexpected) > 0 {
		out.Problems = append(out.Problems, "reported but not expected: "+strings.Join(unexpected, ", "))
	}

	pendingLoading := slices.ContainsFunc(pending, func(id string) bool { return strings.HasPrefix(id, "MDL1") })
	for _, e := range p.Expect {
		if e.Name != "config-loaded" {
			continue
		}
		if pendingLoading {
			out.Pending = append(out.Pending, "loadable")
			continue
		}
		want := e.Value == "true"
		if e.Arg != "" {
			if v, ok := loadable[e.Arg]; ok && v != want {
				out.Problems = append(out.Problems, fmt.Sprintf("linter says %s loadable=%v, scenario expects config-loaded %s: %s", e.Arg, v, e.Arg, e.Value))
			}
			continue
		}
		all := true
		for _, v := range loadable {
			all = all && v
		}
		if all != want {
			out.Problems = append(out.Problems, fmt.Sprintf("linter says every file loadable=%v, scenario expects config-loaded: %s", all, e.Value))
		}
	}
	return out, nil
}

// writeScenarioFiles writes the scenario's files into dir.
func writeScenarioFiles(sc *Scenario, dir string) error {
	for _, f := range sc.Files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
