//go:build e2e

package e2e

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// seededColumn is a column of e2e/fixtures/seed.sql whose values the column
// observation recognizes.
type seededColumn struct {
	Index  int      // position of the column in the table's rows
	Values []string // every value the seed stores
}

// Seeded values of e2e/fixtures/seed.sql that observations look for.
var (
	// seededColumns: users(id, email, name).
	seededColumns = map[string]seededColumn{
		"app.users.email": {Index: 1, Values: []string{"alice@e2e.example", "bob@e2e.example", "zoe@e2e.example"}},
	}
	// seededRoutine appears in a dump only when routines are dumped.
	seededRoutine = "PROCEDURE `e2e_count_users`"
	// seededDatabase is the database of the seed; data files are named after it.
	seededDatabase = "app"
)

// runResult is what one container run left behind.
type runResult struct {
	Tool      string
	Dir       string // host directory mounted at /work
	ExitCode  int
	Stdout    string
	Stderr    string
	Duration  time.Duration
	RestoreDB string
	suite     *suite

	dumpDone bool
	dumpDir  string // host path of the dump directory, "" if none
	dumpErr  error
}

// observation is one kind of `expect` line.
type observation struct {
	Doc   string   // for README.md: what it observes
	Tools []string // tools it applies to
	Multi bool     // may appear several times (values accumulate)
	// validate checks the argument and the expected value at parse time.
	validate func(arg, value string) error
	// check compares the run with the expectation. got describes what was
	// observed, for messages.
	check func(r *runResult, e expectation) (got string, ok bool, err error)
}

var (
	bothTools    = []string{"mydumper", "myloader"}
	mydumperOnly = []string{"mydumper"}
	plainFileRE  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	columnRE     = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+\.[a-z0-9_]+$`)
	tableRE      = regexp.MustCompile(`^(?:[a-z0-9_]+|\$\{RESTORE_DB\})\.[a-z0-9_]+$`)
	threadsRE    = regexp.MustCompile(`Using (\d+) dumper threads`)
	connErrorRE  = regexp.MustCompile(`(?i)error connect(?:ion|ing) to database[^\n]*`)
)

// observations is the catalog of runtime observations (README.md, "Observations").
var observations = map[string]observation{
	"config-loaded": {
		Doc: "`true` unless stderr contains GLib's `Failed to load config file` warning (F1). " +
			"With a file argument (`expect config-loaded extra.cnf: false`), only the warning for " +
			"`/work/<file>` counts.",
		Tools: bothTools,
		validate: func(arg, value string) error {
			if arg != "" && !plainFileRE.MatchString(arg) {
				return fmt.Errorf("argument %q: want a file name", arg)
			}
			return wantBool(value)
		},
		check: func(r *runResult, e expectation) (string, bool, error) {
			needle := "Failed to load config file"
			if e.Arg != "" {
				needle += " /work/" + e.Arg + ":"
			}
			got := "true"
			if line := lineContaining(r.Stderr, needle); line != "" {
				got = "false (" + line + ")"
			}
			return got, strings.HasPrefix(got, e.Value), nil
		},
	},
	"exit": {
		Doc:   "the exit code of the tool: `0`, another number, or `nonzero`.",
		Tools: bothTools,
		validate: func(arg, value string) error {
			if err := noArg(arg); err != nil {
				return err
			}
			if value == "nonzero" {
				return nil
			}
			if n, err := atoiStrict(value); err != nil || n > 255 {
				return fmt.Errorf("value %q: want nonzero or an exit code (0-255)", value)
			}
			return nil
		},
		check: func(r *runResult, e expectation) (string, bool, error) {
			got := strconv.Itoa(r.ExitCode)
			if e.Value == "nonzero" {
				return got, r.ExitCode != 0, nil
			}
			return got, got == e.Value, nil
		},
	},
	"stderr-contains": {
		Doc:      "stderr contains the text (repeatable). mydumper and myloader log everything to stderr.",
		Tools:    bothTools,
		Multi:    true,
		validate: textArg,
		check: func(r *runResult, e expectation) (string, bool, error) {
			if line := lineContaining(r.Stderr, e.Value); line != "" {
				return "present: " + line, true, nil
			}
			return "absent", false, nil
		},
	},
	"stderr-lacks": {
		Doc:      "stderr does not contain the text (repeatable).",
		Tools:    bothTools,
		Multi:    true,
		validate: textArg,
		check: func(r *runResult, e expectation) (string, bool, error) {
			if line := lineContaining(r.Stderr, e.Value); line != "" {
				return "present: " + line, false, nil
			}
			return "absent", true, nil
		},
	},
	"connected": {
		Doc: "`true` when mydumper wrote its `metadata` (or `metadata.partial`) file, which it " +
			"creates only after connecting to the server; `false` otherwise (the connection " +
			"error, if any, is quoted).",
		Tools:    mydumperOnly,
		validate: boolValue,
		check: func(r *runResult, e expectation) (string, bool, error) {
			dir, err := r.dump()
			if err != nil {
				return "", false, err
			}
			got := "true"
			if dir == "" {
				got = "false"
				if m := connErrorRE.FindString(r.Stderr); m != "" {
					got += " (" + m + ")"
				}
			}
			return got, strings.HasPrefix(got, e.Value), nil
		},
	},
	"column": {
		Doc: "`expect column app.users.email: plaintext|masked|empty|null`, from the rows of the " +
			"table's data files: `plaintext` when every row holds its seeded value, `masked` when " +
			"every row holds another non-empty value, `empty` when every value is an empty string, " +
			"`null` when every value is NULL.",
		Tools: mydumperOnly,
		validate: func(arg, value string) error {
			if _, ok := seededColumns[arg]; !ok || !columnRE.MatchString(arg) {
				return fmt.Errorf("argument %q: want a seeded column (%s)", arg, strings.Join(sortedKeys(seededColumns), ", "))
			}
			switch value {
			case "plaintext", "masked", "empty", "null":
				return nil
			}
			return fmt.Errorf("value %q: want plaintext, masked, empty or null", value)
		},
		check: func(r *runResult, e expectation) (string, bool, error) {
			parts := strings.SplitN(e.Arg, ".", 3)
			data, files, err := r.dataFiles(parts[0], parts[1])
			if err != nil {
				return "", false, err
			}
			if len(files) == 0 {
				return "no data file for " + parts[0] + "." + parts[1], false, nil
			}
			rows, err := sqlRows(string(data))
			if err != nil {
				return "", false, fmt.Errorf("%s: %w", strings.Join(files, ", "), err)
			}
			got := classifyColumn(rows, seededColumns[e.Arg])
			return got + " in " + strings.Join(files, ", "), strings.Fields(got)[0] == e.Value, nil
		},
	},
	"routines-dumped": {
		Doc:      "`true` when a dump file defines the seeded stored procedure (" + seededRoutine + ").",
		Tools:    mydumperOnly,
		validate: boolValue,
		check: func(r *runResult, e expectation) (string, bool, error) {
			file, err := r.dumpFileContaining(seededRoutine)
			if err != nil {
				return "", false, err
			}
			got := "false"
			if file != "" {
				got = "true (" + file + ")"
			}
			return got, strings.HasPrefix(got, e.Value), nil
		},
	},
	"data-dumped": {
		Doc:      "`true` when the dump holds at least one data file of the seeded `app` database.",
		Tools:    mydumperOnly,
		validate: boolValue,
		check: func(r *runResult, e expectation) (string, bool, error) {
			_, files, err := r.dataFiles(seededDatabase, "")
			if err != nil {
				return "", false, err
			}
			got := "false"
			if len(files) > 0 {
				got = "true (" + strings.Join(files, ", ") + ")"
			}
			return got, strings.HasPrefix(got, e.Value), nil
		},
	},
	"threads": {
		Doc: "the number of dumper threads mydumper reports (`Using N dumper threads`, logged " +
			"with `--verbose 3`).",
		Tools: mydumperOnly,
		validate: func(arg, value string) error {
			if err := noArg(arg); err != nil {
				return err
			}
			_, err := atoiStrict(value)
			return err
		},
		check: func(r *runResult, e expectation) (string, bool, error) {
			m := threadsRE.FindStringSubmatch(r.Stderr)
			if m == nil {
				return "not logged (run with --verbose 3)", false, nil
			}
			return m[1], m[1] == e.Value, nil
		},
	},
	"dump-dir": {
		Doc: "where mydumper wrote the dump, relative to /work (`dump`, `export-*` for the " +
			"default timestamped directory), or `none`.",
		Tools: mydumperOnly,
		validate: func(arg, value string) error {
			if err := noArg(arg); err != nil {
				return err
			}
			if value == "" || strings.HasPrefix(value, "/") {
				return fmt.Errorf("value %q: want a path relative to /work, export-* or none", value)
			}
			return nil
		},
		check: func(r *runResult, e expectation) (string, bool, error) {
			dir, err := r.dump()
			if err != nil {
				return "", false, err
			}
			got := "none"
			if dir != "" {
				rel, err := filepath.Rel(r.Dir, dir)
				if err != nil {
					return "", false, err
				}
				got = filepath.ToSlash(rel)
			}
			ok, err := filepath.Match(e.Value, got)
			return got, ok || got == e.Value, err
		},
	},
	"dump-contains": {
		Doc:      "a file of the dump contains the text (repeatable).",
		Tools:    mydumperOnly,
		Multi:    true,
		validate: textArg,
		check: func(r *runResult, e expectation) (string, bool, error) {
			file, err := r.dumpFileContaining(e.Value)
			if err != nil || file == "" {
				return "absent", false, err
			}
			return "present in " + file, true, nil
		},
	},
	"dump-lacks": {
		Doc:      "no file of the dump contains the text (repeatable). Fails when there is no dump.",
		Tools:    mydumperOnly,
		Multi:    true,
		validate: textArg,
		check: func(r *runResult, e expectation) (string, bool, error) {
			dir, err := r.dump()
			if err != nil {
				return "", false, err
			}
			if dir == "" {
				return "no dump", false, nil
			}
			file, err := r.dumpFileContaining(e.Value)
			if err != nil {
				return "", false, err
			}
			if file != "" {
				return "present in " + file, false, nil
			}
			return "absent", true, nil
		},
	},
	"rows": {
		Doc: "`expect rows <db>.<table>: N`: the row count of a table on the server after the run " +
			"(`missing` when the table does not exist). `<db>` may be `${RESTORE_DB}`.",
		Tools: bothTools,
		validate: func(arg, value string) error {
			if !tableRE.MatchString(arg) {
				return fmt.Errorf("argument %q: want db.table", arg)
			}
			if value == "missing" {
				return nil
			}
			_, err := atoiStrict(value)
			return err
		},
		check: func(r *runResult, e expectation) (string, bool, error) {
			db, table, _ := strings.Cut(strings.ReplaceAll(e.Arg, restoreDBPlaceholder, r.RestoreDB), ".")
			got, err := r.suite.countRows(db, table)
			if err != nil {
				return "", false, err
			}
			return got, got == e.Value, nil
		},
	},
}

func observationNames() []string { return sortedKeys(observations) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func noArg(arg string) error {
	if arg != "" {
		return fmt.Errorf("unexpected argument %q", arg)
	}
	return nil
}

func wantBool(value string) error {
	if value != "true" && value != "false" {
		return fmt.Errorf("value %q: want true or false", value)
	}
	return nil
}

func boolValue(arg, value string) error {
	if err := noArg(arg); err != nil {
		return err
	}
	return wantBool(value)
}

func textArg(arg, value string) error {
	if err := noArg(arg); err != nil {
		return err
	}
	if value == "" {
		return errors.New("empty text")
	}
	return nil
}

// lineContaining returns the first line of s containing needle, trimmed.
func lineContaining(s, needle string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// dump locates the dump directory: the only directory under the work
// directory that holds a metadata or metadata.partial file (mydumper creates
// it right after connecting). The myloader source dump is ignored.
func (r *runResult) dump() (string, error) {
	if r.dumpDone {
		return r.dumpDir, r.dumpErr
	}
	r.dumpDone = true
	var dirs []string
	err := filepath.WalkDir(r.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path == filepath.Join(r.Dir, sourceDumpDir) {
			return filepath.SkipDir
		}
		if !d.IsDir() && (d.Name() == "metadata" || d.Name() == "metadata.partial") {
			if dir := filepath.Dir(path); len(dirs) == 0 || dirs[len(dirs)-1] != dir {
				dirs = append(dirs, dir)
			}
		}
		return nil
	})
	switch {
	case err != nil:
		r.dumpErr = err
	case len(dirs) > 1:
		r.dumpErr = fmt.Errorf("several dump directories: %s", strings.Join(dirs, ", "))
	case len(dirs) == 1:
		r.dumpDir = dirs[0]
	}
	return r.dumpDir, r.dumpErr
}

// dumpFiles returns the regular files of the dump directory, sorted.
func (r *runResult) dumpFiles() ([]string, error) {
	dir, err := r.dump()
	if err != nil || dir == "" {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// readDumpFile reads a dump file, decompressing gzip. zstd is not supported:
// scenarios that observe data must not compress it.
func (r *runResult) readDumpFile(name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(r.dumpDir, name))
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasSuffix(name, ".gz"):
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return io.ReadAll(zr)
	case strings.HasSuffix(name, ".zst"):
		return nil, fmt.Errorf("%s: zstd-compressed dump files are not supported by the harness", name)
	}
	return data, nil
}

// dumpFileContaining returns the first dump file (by name) containing text,
// or "".
func (r *runResult) dumpFileContaining(text string) (string, error) {
	files, err := r.dumpFiles()
	if err != nil {
		return "", err
	}
	for _, f := range files {
		data, err := r.readDumpFile(f)
		if err != nil {
			return "", err
		}
		if bytes.Contains(data, []byte(text)) {
			return f, nil
		}
	}
	return "", nil
}

// dataFiles returns the concatenated content and the names of the data files
// of db.table (every table of db when table is empty): files named
// "db.table.*" that are not schema files.
func (r *runResult) dataFiles(db, table string) ([]byte, []string, error) {
	files, err := r.dumpFiles()
	if err != nil {
		return nil, nil, err
	}
	prefix := db + "."
	if table != "" {
		prefix += table + "."
	}
	var data []byte
	var names []string
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) || strings.Contains(f, "-schema") || strings.HasPrefix(f, "metadata") {
			continue
		}
		b, err := r.readDumpFile(f)
		if err != nil {
			return nil, nil, err
		}
		data = append(data, b...)
		names = append(names, f)
	}
	return data, names, nil
}

// listing describes the work directory for failure messages.
func listing(dir string) string {
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(&b, "  %s: %v\n", path, err)
			return nil // best effort: a listing for a failure message
		}
		if path == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			rel += "/"
		}
		fmt.Fprintf(&b, "  %s\n", rel)
		return nil
	})
	return b.String()
}
