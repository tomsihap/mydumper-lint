package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
	"github.com/tomsihap/mydumper-lint/internal/target"
)

const inspectHelp = `Usage: mydumper-lint inspect [flags] FILE

Show what mydumper sees in a configuration file: whether GLib loads it (and
the exact error mydumper logs if not), the lines mydumper's pre-processor
rewrites, the groups and keys GLib returns, and for every key whether it has
an effect and why not. Use - to read stdin.

Flags:
`

// inspectDoc is the JSON document printed by inspect.
type inspectDoc struct {
	Path            string          `json:"path"`
	MydumperVersion string          `json:"mydumper_version"`
	Preprocessor    bool            `json:"preprocessor"` // the version runs mydumper's pre-processor
	Loadable        bool            `json:"loadable"`
	Health          string          `json:"health"`
	Error           *inspectError   `json:"error"`
	Rewritten       []inspectLine   `json:"rewritten_lines"`
	GLib            []inspectGroup  `json:"glib_view"`
	Model           []inspectMGroup `json:"model"`
}

type inspectError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type inspectLine struct {
	Line       int    `json:"line"`
	Original   string `json:"original"`
	Rewritten  string `json:"rewritten"`
	Leak       bool   `json:"state_leak"`
	LeakOrigin int    `json:"leak_origin,omitempty"`
}

type inspectGroup struct {
	Name    string         `json:"name"`
	Entries []inspectEntry `json:"entries"`
}

type inspectEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type inspectMGroup struct {
	Name    string          `json:"name"`
	Kind    string          `json:"kind"`
	Tool    string          `json:"tool,omitempty"`
	Lines   []int           `json:"lines"`
	Entries []inspectMEntry `json:"entries"`
}

type inspectMEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Line      int    `json:"line"`
	Effective bool   `json:"effective"`
	Reason    string `json:"reason"`
}

func runInspect(args []string, e *env) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	format := fs.String("format", "text", "Output `format`: text or json.")
	version := fs.String("mydumper-version", "", "Target mydumper `version`.")
	configPath := fs.String("config", "", "Configuration `file` to use.")
	noConfig := fs.Bool("no-config", false, "Ignore configuration files.")
	lang := fs.String("lang", "", "Value of LANG in mydumper's environment (localized keys depend on it).")
	paths, err := parse(fs, args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(e.stdout, inspectHelp+flagUsage(fs))
		return ExitOK
	}
	if err != nil {
		return usageError(e, "inspect", err)
	}
	if len(paths) != 1 {
		return usageError(e, "inspect", errors.New("expected exactly one file"))
	}
	if *format != "text" && *format != "json" {
		return usageError(e, "inspect", fmt.Errorf("--format must be text or json, not %q", *format))
	}
	path := paths[0]
	var src []byte
	if path == "-" {
		src, err = io.ReadAll(e.stdin)
		path = "<stdin>"
	} else {
		src, err = os.ReadFile(path)
	}
	if err != nil {
		fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	var cfg *config.Config
	switch {
	case *configPath != "":
		cfg, err = config.Load(*configPath)
	case !*noConfig:
		cfg, err = config.NewLoader().For(path)
	}
	if err != nil {
		fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	s := cfg.Resolve(path)
	if *version != "" {
		s.MydumperVersion = *version
	}
	t, err := resolveTarget(s)
	if err != nil {
		fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
		return ExitError
	}
	if t.Notice != "" {
		fmt.Fprintf(e.stderr, "mydumper-lint: %s\n", t.Notice)
	}
	languages := keyfile.LanguageNames(func(k string) string {
		if k == "LANG" {
			return *lang
		}
		return ""
	})
	doc := buildInspect(path, src, t, languages)
	if *format == "json" {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			return ExitError
		}
		return ExitOK
	}
	printInspect(e.stdout, doc)
	return ExitOK
}

func buildInspect(path string, src []byte, t target.Target, languages []string) inspectDoc {
	f := source.New(path, src)
	v := t.View.Version()
	pre := preprocess.Run(f)
	if !v.Preprocessor {
		pre = preprocess.Passthrough(f)
	}
	kf := keyfile.Parse(f, pre)
	m := model.Build(kf, model.Options{Languages: languages, Target: t.View})
	doc := inspectDoc{
		Path: path, MydumperVersion: v.Tag, Preprocessor: v.Preprocessor, Loadable: kf.Loadable,
		Health: m.Health.String(), Rewritten: []inspectLine{}, GLib: []inspectGroup{}, Model: []inspectMGroup{},
	}
	if kf.FirstError != nil {
		doc.Error = &inspectError{Line: kf.FirstError.Line, Message: kf.FirstError.Message}
	}
	for i, info := range pre.Lines {
		leak := info.AppendsEqOne != info.IdealAppendsEqOne
		if !info.AppendsEqOne && !leak {
			continue
		}
		orig := string(f.Content(i + 1))
		rw := orig
		if info.AppendsEqOne {
			rw += preprocess.EqOne
		}
		doc.Rewritten = append(doc.Rewritten, inspectLine{Line: i + 1, Original: orig, Rewritten: rw, Leak: leak, LeakOrigin: info.LeakOrigin})
	}
	for _, g := range kf.GLibView(languages) {
		ig := inspectGroup{Name: g.Name, Entries: []inspectEntry{}}
		for _, en := range g.Entries {
			ig.Entries = append(ig.Entries, inspectEntry{Key: en.Key, Value: en.Value})
		}
		doc.GLib = append(doc.GLib, ig)
	}
	for _, g := range m.Groups {
		mg := inspectMGroup{Name: g.Name, Kind: g.Kind.String(), Tool: g.Tool, Lines: g.Lines, Entries: []inspectMEntry{}}
		for _, en := range g.Entries {
			mg.Entries = append(mg.Entries, inspectMEntry{Key: en.Key, Value: en.Value, Line: en.Line, Effective: en.Effective, Reason: en.Reason.String()})
		}
		doc.Model = append(doc.Model, mg)
	}
	return doc
}

func printInspect(w io.Writer, d inspectDoc) {
	fmt.Fprintf(w, "%s\n", d.Path)
	fmt.Fprintf(w, "  mydumper version: %s\n", d.MydumperVersion)
	if !d.Preprocessor {
		fmt.Fprintln(w, "  this version has no pre-processor: GLib reads the file as written")
	}
	if d.Loadable {
		fmt.Fprintf(w, "  GLib loads the file (health: %s)\n", d.Health)
	} else {
		fmt.Fprintf(w, "  GLib REJECTS the file: mydumper ignores all of it\n  mydumper logs: Failed to load config file %s: %s\n  (line %d)\n",
			d.Path, strings.ReplaceAll(d.Error.Message, "\r", `\r`), d.Error.Line)
	}
	if len(d.Rewritten) > 0 {
		fmt.Fprintln(w, "\nLines rewritten by mydumper's pre-processor:")
		for _, l := range d.Rewritten {
			note := ""
			if l.Leak {
				note = fmt.Sprintf("  (state leaked from line %d)", l.LeakOrigin)
			}
			fmt.Fprintf(w, "  %4d  %q -> %q%s\n", l.Line, l.Original, l.Rewritten, note)
		}
	}
	fmt.Fprintln(w, "\nEffective configuration:")
	if len(d.Model) == 0 {
		fmt.Fprintln(w, "  (no groups)")
	}
	for _, g := range d.Model {
		tool := ""
		if g.Tool != "" {
			tool = ", " + g.Tool
		}
		fmt.Fprintf(w, "  [%s]  (%s%s)\n", g.Name, g.Kind, tool)
		for _, en := range g.Entries {
			mark := "  "
			if !en.Effective {
				mark = "✗ "
			}
			reason := ""
			if !en.Effective {
				reason = "  <- " + en.Reason
			}
			fmt.Fprintf(w, "    %s%s = %q%s\n", mark, en.Key, en.Value, reason)
		}
	}
}
