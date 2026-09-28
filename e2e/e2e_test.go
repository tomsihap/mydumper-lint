//go:build e2e

// Package e2e is the end-to-end suite (design §11.6): it runs the official
// mydumper and myloader images against a seeded MySQL server and proves the
// runtime consequences mydumper-lint's rules claim. See README.md.
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/rules"
)

const scenarioDir = "scenarios"

// TestMain writes the summary (E2E_SUMMARY, default .out/summary.md) once
// every scenario has run. Returning from TestMain exits with m.Run's code.
func TestMain(m *testing.M) {
	m.Run()
	if theSuite == nil || len(results.all()) == 0 {
		return
	}
	path := os.Getenv("E2E_SUMMARY")
	if path == "" {
		path = filepath.Join(theSuite.outDir, "summary.md")
	}
	if err := os.WriteFile(path, []byte(results.summary(theSuite)), 0o644); err != nil {
		logf("cannot write the summary: %v", err)
		return
	}
	logf("summary written to %s", path)
}

// TestScenarioFiles checks that every scenario parses and plans on every
// embedded version, without Docker.
func TestScenarioFiles(t *testing.T) {
	db, err := optionsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := LoadScenarios(scenarioDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range scenarios {
		applies := false
		for _, v := range db.Versions {
			view, err := db.View(v.Tag, optionsdb.DefaultBuild)
			if err != nil {
				t.Fatal(err)
			}
			tag := optionsdb.MustParseTag(v.Tag)
			if !sc.appliesTo(tag, view) {
				continue
			}
			applies = true
			if _, err := sc.planFor(tag, view); err != nil {
				t.Errorf("%s: %v", sc.Name, err)
			}
		}
		if !applies {
			t.Errorf("%s: applies to no embedded version", sc.Name)
		}
	}
}

// TestRuleE2EReferences checks that every scenario a rule's metadata cites
// as the proof of its runtime consequence exists (design §11.8).
func TestRuleE2EReferences(t *testing.T) {
	for _, r := range rules.All() {
		for _, name := range r.E2E {
			if _, err := os.Stat(filepath.Join(scenarioDir, name+".txtar")); err != nil {
				t.Errorf("%s cites e2e scenario %q: %v", r.ID, name, err)
			}
		}
	}
}

// TestReadmeDocumentsObservations keeps the observation table of README.md
// complete.
func TestReadmeDocumentsObservations(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range observationNames() {
		if !strings.Contains(string(data), "| `"+name) {
			t.Errorf("README.md does not document the observation %q", name)
		}
	}
}

// TestScenarios runs every scenario on every selected version it applies to.
func TestScenarios(t *testing.T) {
	s, err := getSuite()
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := LoadScenarios(scenarioDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			versions, fallback := s.versionsFor(sc)
			if len(versions) == 0 {
				msg := "no selected version applies (versions: " + condText(sc.Versions) + ")"
				results.add(outcome{Scenario: sc.Name, Status: "skip", Note: msg})
				t.Skip(msg)
			}
			t.Parallel()
			for _, tag := range versions {
				t.Run(tag, func(t *testing.T) {
					t.Parallel()
					s.run(t, sc, tag, fallback)
				})
			}
		})
	}
}

// versionsFor returns the selected versions a scenario applies to. With the
// default matrix, a scenario that none applies to runs on the newest
// image-verified version it applies to whose image passes the guard, so that
// every scenario runs in every default run (fallback is then true).
func (s *suite) versionsFor(sc *Scenario) (tags []string, fallback bool) {
	for _, tag := range s.versions {
		if sc.appliesTo(optionsdb.MustParseTag(tag), s.view(tag)) {
			tags = append(tags, tag)
		}
	}
	if len(tags) > 0 || !s.fallback {
		return tags, false
	}
	for i := len(s.db.Versions) - 1; i >= 0; i-- {
		v := s.db.Versions[i]
		if !v.ImageVerified || !sc.appliesTo(optionsdb.MustParseTag(v.Tag), s.view(v.Tag)) {
			continue
		}
		if s.image(v.Tag).skip == "" {
			return []string{v.Tag}, true
		}
	}
	return nil, false
}

func (s *suite) view(tag string) *optionsdb.View {
	v, err := s.db.View(tag, optionsdb.DefaultBuild)
	if err != nil {
		panic(err) // tags come from the knowledge base itself
	}
	return v
}

func condText(cs []cond) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, ", ")
}

// run runs one scenario on one version: files, container, observations,
// linter. Everything it sees goes to .out/logs/<scenario>/<version>.log.
func (s *suite) run(t *testing.T, sc *Scenario, tag string, fallback bool) {
	rs := &runState{t: t, o: &outcome{Scenario: sc.Name, Version: tag, Fallback: fallback}}
	start := time.Now()
	t.Cleanup(func() {
		rs.o.Duration = time.Since(start)
		switch {
		case t.Skipped():
			rs.o.Status = "skip"
		case t.Failed():
			rs.o.Status = "FAIL"
			if rs.o.Note == "" && len(rs.problems) > 0 {
				rs.o.Note = strings.Join(rs.problems, "; ")
			}
		default:
			rs.o.Status = "pass"
		}
		results.add(*rs.o)
	})

	p, err := sc.planFor(optionsdb.MustParseTag(tag), s.view(tag))
	if err != nil {
		rs.o.Note = err.Error()
		t.Fatal(err)
	}
	g := s.image(tag)
	if g.skip != "" {
		rs.o.Note = g.skip
		t.Skip(g.skip)
	}

	dir := filepath.Join(s.outDir, "work", sc.Name, tag)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioFiles(sc, dir); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&rs.log, "scenario %s on %s\nimage %s (%s)\n", sc.Name, tag, g.ref, g.desc)
	defer func() {
		path := filepath.Join(s.outDir, "logs", sc.Name, tag+".log")
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(rs.log.String()), 0o644)
		if t.Failed() || s.keep {
			t.Logf("log: %s\nwork directory: %s", path, dir)
		} else {
			_ = os.RemoveAll(dir)
			_ = os.Remove(filepath.Dir(dir)) // the scenario's directory, once empty
		}
	}()

	if !s.lintOnly {
		s.runAndObserve(rs, sc, g.ref, dir, p)
	}
	s.checkLint(rs, sc, dir, tag, p)
	if t.Failed() {
		fmt.Fprintf(&rs.log, "\n--- work directory ---\n%s", listing(dir))
	}
}

// runState is what one run accumulates.
type runState struct {
	t        *testing.T
	o        *outcome
	log      strings.Builder
	problems []string
}

func (rs *runState) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	rs.problems = append(rs.problems, msg)
	rs.t.Error(msg)
}

// runAndObserve runs the tool (after preparing a source dump for myloader)
// and checks every expectation of the plan.
func (s *suite) runAndObserve(rs *runState, sc *Scenario, ref, dir string, p plan) {
	t := rs.t
	ctx := context.Background()
	args := slices.Clone(sc.Args)
	restoreDB := ""
	if slices.ContainsFunc(sc.Args, func(a string) bool { return strings.Contains(a, restoreDBPlaceholder) }) ||
		slices.ContainsFunc(p.Expect, func(e expectation) bool { return strings.Contains(e.Arg, restoreDBPlaceholder) }) {
		restoreDB = "e2e_restore_" + randomHex(4)
		for i := range args {
			args[i] = strings.ReplaceAll(args[i], restoreDBPlaceholder, restoreDB)
		}
		defer func() {
			// A leftover restore database does not change the observations
			// (every scenario selects the databases it dumps): warn only.
			if err := s.dropDatabase(restoreDB); err != nil {
				fmt.Fprintf(&rs.log, "\nWARNING: dropping %s: %v\n", restoreDB, err)
				t.Logf("WARNING: dropping %s: %v", restoreDB, err)
			}
		}()
	}
	if sc.Tool == "myloader" {
		prep := []string{
			"--host", mysqlHost, "--user", e2eUser, "--password", e2ePassword,
			"--database", seededDatabase, "--outputdir", "/work/" + sourceDumpDir,
		}
		fmt.Fprintf(&rs.log, "\n$ mydumper %s\n", strings.Join(prep, " "))
		r, err := s.runTool(ctx, ref, "mydumper", dir, nil, prep)
		fmt.Fprintf(&rs.log, "exit %d in %s\n%s", r.ExitCode, r.Duration.Round(time.Millisecond), r.Stderr)
		if err != nil || r.ExitCode != 0 {
			rs.o.Note = fmt.Sprintf("preparing the source dump failed: exit %d %v", r.ExitCode, err)
			t.Fatalf("%s\n%s", rs.o.Note, r.Stderr)
		}
	}

	envText := "(none: only the image's environment)"
	if len(sc.Env) > 0 {
		envText = strings.Join(sc.Env, " ")
	}
	fmt.Fprintf(&rs.log, "\ncontainer env added: %s\n$ %s %s\n", envText, sc.Tool, strings.Join(args, " "))
	r, err := s.runTool(ctx, ref, sc.Tool, dir, sc.Env, args)
	if err != nil {
		rs.o.Note = err.Error()
		fmt.Fprintf(&rs.log, "%v\n%s", err, r.Stderr)
		t.Fatal(err)
	}
	fmt.Fprintf(&rs.log, "exit %d in %s\n--- stderr ---\n%s", r.ExitCode, r.Duration.Round(time.Millisecond), r.Stderr)
	if strings.TrimSpace(r.Stdout) != "" {
		fmt.Fprintf(&rs.log, "--- stdout ---\n%s", r.Stdout)
	}
	res := &runResult{
		Tool: sc.Tool, Dir: dir, ExitCode: r.ExitCode, Stdout: r.Stdout, Stderr: r.Stderr,
		Duration: r.Duration, RestoreDB: restoreDB, suite: s,
	}

	rs.log.WriteString("\n--- observations ---\n")
	for _, e := range p.Expect {
		got, ok, err := observations[e.Name].check(res, e)
		status := "ok"
		switch {
		case err != nil:
			status = "ERROR"
			rs.fail("expect %s (line %d): %v", e, e.Line, err)
		case !ok:
			status = "MISMATCH"
			rs.fail("expect %s (line %d): got %s", e, e.Line, got)
		}
		fmt.Fprintf(&rs.log, "[%s] expect %s — got %s\n", status, e, got)
	}
	if t.Failed() {
		t.Logf("stderr:\n%s", tail(r.Stderr, 30))
	}
}

// checkLint compares the linter with the plan (see suite.lint).
func (s *suite) checkLint(rs *runState, sc *Scenario, dir, tag string, p plan) {
	lo, err := s.lint(context.Background(), sc, dir, tag, p)
	fmt.Fprintf(&rs.log, "\n--- lint ---\n$ %s\nexpected: [%s]  reported: [%s]  pending: [%s]\n%s",
		lo.Command, strings.Join(p.Lint, ", "), strings.Join(lo.Got, ", "), strings.Join(lo.Pending, ", "), lo.Details)
	rs.o.Pending = lo.Pending
	if err != nil {
		rs.fail("lint: %v", err)
	}
	for _, pr := range lo.Problems {
		fmt.Fprintf(&rs.log, "[MISMATCH] lint: %s\n", pr)
		rs.fail("lint: %s", pr)
	}
	if len(lo.Pending) > 0 {
		rs.t.Logf("lint pending (rules not registered yet): %s", strings.Join(lo.Pending, ", "))
	}
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{"…"}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
