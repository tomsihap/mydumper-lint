// Command gendocs generates the documentation that derives from the code
// (design §12.5): one page per rule in docs/rules/ with examples taken from
// the rule's golden tests, the rule index, the rule table and the
// supported-versions table of the README, docs/versions.md, and the JSON
// Schema of the configuration.
//
//	go run ./tools/gendocs          write the files
//	go run ./tools/gendocs -check   fail if any file is out of date
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
	"github.com/tomsihap/mydumper-lint/internal/rules"
	"github.com/tomsihap/mydumper-lint/internal/txtar"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("gendocs", flag.ContinueOnError)
	fl.SetOutput(stderr)
	check := fl.Bool("check", false, "report out-of-date files instead of writing them")
	root := fl.String("root", ".", "repository root")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	files, err := generate(*root)
	if err != nil {
		fmt.Fprintf(stderr, "gendocs: %v\n", err)
		return 1
	}
	stale := 0
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(*root, name)
		old, _ := os.ReadFile(path)
		if bytes.Equal(old, files[name]) {
			continue
		}
		stale++
		if *check {
			fmt.Fprintf(stderr, "gendocs: %s is out of date\n", name)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: documentation directories of the repository
			fmt.Fprintf(stderr, "gendocs: %v\n", err)
			return 1
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil { //nolint:gosec // G306: documentation files of the repository
			fmt.Fprintf(stderr, "gendocs: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s\n", name)
	}
	if *check && stale > 0 {
		fmt.Fprintln(stderr, "gendocs: run `make docs` and commit the result")
		return 1
	}
	return 0
}

// generate returns every generated file, by path relative to the root.
func generate(root string) (map[string][]byte, error) {
	out := map[string][]byte{}
	all := rules.All()
	for _, r := range all {
		page, err := rulePage(root, r)
		if err != nil {
			return nil, err
		}
		out["docs/rules/"+r.ID+".md"] = page
	}
	out["docs/rules/README.md"] = ruleIndex(all)
	db, err := optionsdb.Load()
	if err != nil {
		return nil, err
	}
	out["docs/versions.md"] = []byte("# Supported mydumper versions\n\n" +
		"Generated from the embedded knowledge base (`internal/optionsdb`). " +
		"`mydumper-lint versions` prints the same table.\n\n" + versionsTable(db))
	schema, err := config.Schema()
	if err != nil {
		return nil, err
	}
	out["schemas/config.v1.json"] = schema
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return nil, err
	}
	readme, err = replaceBetween(readme, "rules", ruleTable(all, "docs/rules/"))
	if err != nil {
		return nil, err
	}
	if readme, err = replaceBetween(readme, "versions", versionsSummary(db)); err != nil {
		return nil, err
	}
	out["README.md"] = readme
	return out, nil
}

// replaceBetween replaces the text between <!-- NAME:begin --> and
// <!-- NAME:end --> markers.
func replaceBetween(b []byte, name, content string) ([]byte, error) {
	begin, end := []byte("<!-- "+name+":begin -->"), []byte("<!-- "+name+":end -->")
	i, j := bytes.Index(b, begin), bytes.Index(b, end)
	if i < 0 || j < i {
		return nil, fmt.Errorf("README.md: missing %s or %s", begin, end)
	}
	var buf bytes.Buffer
	buf.Write(b[:i+len(begin)])
	buf.WriteString("\n" + content)
	buf.Write(b[j:])
	return buf.Bytes(), nil
}

var familyTitles = []struct {
	family rules.Family
	title  string
}{
	{rules.FamilySuppressions, "Suppressions"},
	{rules.FamilyLoading, "Loading: mydumper ignores the whole file"},
	{rules.FamilyGroups, "Groups"},
	{rules.FamilyValues, "Lines, keys and values"},
	{rules.FamilyOptions, "Options"},
	{rules.FamilyTables, "Table sections and masking"},
	{rules.FamilyConnection, "Connection (MySQL client library)"},
	{rules.FamilyConventions, "Team conventions"},
}

func fixLabel(r *rules.Rule) string {
	if r.Fix == 0 {
		return "—"
	}
	return r.Fix.String()
}

func flags(r *rules.Rule) string {
	var f []string
	if r.OptIn {
		f = append(f, "opt-in")
	}
	if r.Preview {
		f = append(f, "preview")
	}
	if r.Unsuppressible {
		f = append(f, "unsuppressible")
	}
	return strings.Join(f, ", ")
}

// ruleTable is the rule table of the README and of the rule index.
func ruleTable(all []*rules.Rule, link string) string {
	var b strings.Builder
	for _, ft := range familyTitles {
		fmt.Fprintf(&b, "\n**%s**\n\n| Rule | Name | Severity | Fix | Summary |\n|---|---|---|---|---|\n", ft.title)
		for _, r := range all {
			if r.Family != ft.family {
				continue
			}
			sev := r.Severity.String()
			if f := flags(r); f != "" && f != "unsuppressible" {
				sev += " (" + strings.TrimSuffix(strings.TrimSuffix(f, ", unsuppressible"), "unsuppressible") + ")"
			}
			fmt.Fprintf(&b, "| [%s](%s%s.md) | `%s` | %s | %s | %s |\n", r.ID, link, r.ID, r.Name, sev, fixLabel(r), escapeTable(r.Summary))
		}
	}
	return b.String()
}

func escapeTable(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func ruleIndex(all []*rules.Rule) []byte {
	var b strings.Builder
	b.WriteString("# Rules\n\n<!-- Generated by tools/gendocs: do not edit. -->\n\n")
	fmt.Fprintf(&b, "mydumper-lint has %d rules. Every rule is enabled by default unless it is opt-in; "+
		"select them with `--select`, `--extend-select` and `--ignore`, or in `.mydumper-lint.yaml`.\n", len(all))
	b.WriteString(ruleTable(all, ""))
	return []byte(b.String())
}

// rulePage renders one rule, with an example from its golden tests.
func rulePage(root string, r *rules.Rule) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · `%s`\n\n<!-- Generated by tools/gendocs from internal/rules: do not edit. -->\n\n", r.ID, r.Name)
	fmt.Fprintf(&b, "%s\n\n", r.Summary)
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Family | %s |\n| Default severity | %s |\n| Fix | %s |\n", r.Family, r.Severity, fixLabel(r))
	if f := flags(r); f != "" {
		fmt.Fprintf(&b, "| Flags | %s |\n", f)
	}
	if len(r.Refs) > 0 {
		fmt.Fprintf(&b, "| Design references | %s |\n", strings.Join(r.Refs, ", "))
	}
	if len(r.E2E) > 0 {
		var links []string
		for _, s := range r.E2E {
			links = append(links, fmt.Sprintf("[`%s`](../../e2e/scenarios/%s.txtar)", s, s))
		}
		fmt.Fprintf(&b, "| Proven on real mydumper by | %s |\n", strings.Join(links, ", "))
	}
	b.WriteString("\n## Why\n\n" + r.Why + "\n")
	ex, err := example(root, r.ID)
	if err != nil {
		return nil, err
	}
	if ex != "" {
		b.WriteString("\n## Example\n\n" + ex)
	}
	b.WriteString("\n## Configuration\n\n")
	switch {
	case r.OptIn:
		fmt.Fprintf(&b, "Opt-in: enable it with `--extend-select %s`, or in `.mydumper-lint.yaml`:\n\n```yaml\nrules:\n  extend-select: [%s]\n```\n", r.ID, r.ID)
	default:
		fmt.Fprintf(&b, "Enabled by default. To change its severity or turn it off:\n\n```yaml\nrules:\n  severity:\n    %s: warning   # or error, info, off\n```\n", r.ID)
	}
	if r.Unsuppressible {
		b.WriteString("\nIts diagnostics mean mydumper ignores the whole file: they cannot be suppressed by comments, only by the configuration or `--ignore`.\n")
	} else if r.Family != rules.FamilySuppressions {
		fmt.Fprintf(&b, "\nTo accept one occurrence, put `# mydumper-lint: disable-next-line=%s` on the line above it.\n", r.ID)
	}
	return []byte(b.String()), nil
}

// example picks a golden case of the rule that reports it, preferring one
// with a fix, and renders it: the input, the diagnostics, the fixed file.
func example(root, id string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "internal", "rules", "testdata", id, "*.txtar"))
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	best, bestScore := "", -1
	var bestArchive *txtar.Archive
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		a := txtar.Parse(raw)
		ds, _, _ := a.Get("diagnostics")
		if !bytes.Contains(ds, []byte(" "+id+"[")) {
			continue
		}
		// Prefer a case that shows a fix, the rule at its default severity,
		// and an input that needs no escaping.
		score := 0
		if opts, _, _ := a.Get("options"); bytes.Contains(opts, []byte("doc-example:")) {
			score += 100
		}
		if _, ok, _ := a.Get("fixed.cnf"); ok {
			score += 4
		} else if _, ok, _ := a.Get("fixed-unsafe.cnf"); ok {
			score += 3
		}
		if r, ok := rules.Lookup(id); ok && bytes.Contains(ds, []byte(" "+r.Severity.String()+" "+id+"[")) {
			score += 2
		}
		if !escaped(a, "input.cnf") {
			score++
		}
		if score > bestScore {
			best, bestScore, bestArchive = p, score, a
		}
	}
	if best == "" {
		return "", nil
	}
	a := bestArchive
	var b strings.Builder
	if opts, ok, _ := a.Get("options"); ok {
		for _, line := range strings.Split(string(opts), "\n") {
			if v, ok := strings.CutPrefix(line, "mydumper-version:"); ok {
				fmt.Fprintf(&b, "With mydumper %s:\n\n", strings.TrimSpace(v))
			}
		}
	}
	b.WriteString("❌ Before:\n\n" + fence(a, "input.cnf"))
	ds, _, _ := a.Get("diagnostics")
	b.WriteString("\n```text\n" + string(onlyRule(ds, id)) + "```\n")
	for _, name := range []string{"fixed.cnf", "fixed-unsafe.cnf"} {
		if _, ok, _ := a.Get(name); ok {
			label := "✅ After `mydumper-lint check --fix`:"
			if name == "fixed-unsafe.cnf" {
				label = "✅ After `mydumper-lint check --fix --unsafe-fixes`:"
			}
			b.WriteString("\n" + label + "\n\n" + fence(a, name))
			break
		}
	}
	return b.String(), nil
}

func escaped(a *txtar.Archive, name string) bool {
	for _, f := range a.Files {
		if f.Name == name+".esc" {
			return true
		}
	}
	return false
}

// fence renders a file section in a code block. An escaped section keeps
// its escapes, one line per line of the file, so that invisible bytes stay
// visible; trailing spaces are shown as ·.
func fence(a *txtar.Archive, name string) string {
	for _, f := range a.Files {
		if f.Name != name+".esc" {
			continue
		}
		text := strings.TrimSuffix(string(f.Data), "\n")
		var b strings.Builder
		b.WriteString("```text\n")
		lines := strings.Split(text, `\n`)
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, l := range lines {
			t := strings.TrimRight(l, " ")
			b.WriteString(t + strings.Repeat("·", len(l)-len(t)) + "\n")
		}
		b.WriteString("```\n\n(`\\r`, `\\t` and `\\xHH` stand for the bytes they name, `·` for a trailing space)\n")
		return b.String()
	}
	data, _, _ := a.Get(name)
	return "```ini\n" + string(data) + "```\n"
}

// onlyRule keeps the golden diagnostics of one rule.
func onlyRule(ds []byte, id string) []byte {
	var out bytes.Buffer
	keep := false
	for _, line := range strings.SplitAfter(string(ds), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			keep = strings.Contains(line, " "+id+"[")
		}
		if keep {
			out.WriteString(line)
		}
	}
	return out.Bytes()
}

func versionsTable(db *optionsdb.DB) string {
	def, _ := db.Resolve("latest")
	var b strings.Builder
	b.WriteString("| Version | Released | Status | Unknown options | Pre-processor | Image checked |\n|---|---|---|---|---|---|\n")
	for i := len(db.Versions) - 1; i >= 0; i-- {
		v := db.Versions[i]
		tag := "`" + v.Tag + "`"
		if def.Version != nil && v.Tag == def.Version.Tag {
			tag += " (default)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", tag, orDash(v.Date),
			pick(v.Prerelease, "pre-release", "stable"), pick(v.IgnoreUnknownOptions, "ignored", "fatal"),
			pick(v.Preprocessor, "yes", "no"), pick(v.ImageVerified, "yes", "no"))
	}
	return b.String()
}

func versionsSummary(db *optionsdb.DB) string {
	def, _ := db.Resolve("latest")
	tag := ""
	if def.Version != nil {
		tag = def.Version.Tag
	}
	return fmt.Sprintf("mydumper-lint knows %d mydumper versions, from %s to %s; the default target is %s, "+
		"the latest stable release. See [docs/versions.md](docs/versions.md).\n",
		len(db.Versions), db.Oldest().Tag, db.Newest().Tag, tag)
}

func pick(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
