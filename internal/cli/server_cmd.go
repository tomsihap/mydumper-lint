package cli

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/tomsihap/mydumper-lint/internal/buildinfo"
	"github.com/tomsihap/mydumper-lint/internal/config"
	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/fix"
	"github.com/tomsihap/mydumper-lint/internal/lint"
	"github.com/tomsihap/mydumper-lint/internal/lsp"
)

const serverHelp = `Usage: mydumper-lint server [flags]

Run mydumper-lint as a language server (LSP) on stdin and stdout, for
editors: the diagnostics of check while you type, their fixes as code actions
("Fix all safe mydumper-lint problems" among them), and each rule's
explanation on hover. Editors start it themselves: see docs/editors.md.

Each file gets the settings check would give it: the nearest
.mydumper-lint.yaml, or --config. The editor can also set the mydumper version
(initializationOptions or settings: {"mydumperLint": {"mydumperVersion":
"v0.19.3-3"}}). Load sets are not analyzed in the editor yet.

Flags:
`

func runServer(args []string, e *env) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	version := fs.String("mydumper-version", "", "Target mydumper `version` for every file, unless the editor sets one.")
	configPath := fs.String("config", "", "Use this configuration `file` instead of discovering .mydumper-lint.yaml.")
	noConfig := fs.Bool("no-config", false, "Ignore configuration files.")
	fs.Bool("stdio", false, "Talk over stdin and stdout (the only transport; accepted because editors pass it).")
	paths, err := parse(fs, args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(e.stdout, serverHelp+flagUsage(fs))
		return ExitOK
	}
	if err == nil && len(paths) > 0 {
		err = errors.New("the server takes no file: the editor sends them")
	}
	if err != nil {
		return usageError(e, "server", err)
	}
	l := &serverLinter{version: *version, noConfig: *noConfig}
	if *configPath != "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
			return ExitError
		}
		abs, _ := filepath.Abs(*configPath)
		cfg.Dir = filepath.Dir(abs)
		l.fixed = cfg
	}
	l.Configure(lsp.Settings{})
	srv := &lsp.Server{Linter: l, Version: buildinfo.Version, Log: e.stderr}
	switch err := srv.Serve(e.stdin, e.stdout); {
	case errors.Is(err, lsp.ErrNoShutdown):
		return ExitFindings // LSP: exit code 1 without a shutdown request
	case err != nil:
		fmt.Fprintf(e.stderr, "mydumper-lint server: %v\n", err)
		return ExitError
	}
	return ExitOK
}

// serverLinter lints editor documents with check's settings resolution.
type serverLinter struct {
	version  string // --mydumper-version
	noConfig bool
	fixed    *config.Config // --config
	client   string         // the editor's mydumperVersion setting
	loader   *config.Loader
	targets  *targetCache
}

// Configure applies the editor's settings and forgets the configuration
// files and linters built so far.
func (l *serverLinter) Configure(s lsp.Settings) {
	l.client = s.MydumperVersion
	l.loader = nil
	if !l.noConfig && l.fixed == nil {
		l.loader = config.NewLoader()
	}
	l.targets = newTargetCache()
}

// linter returns the linter and the settings for a document, and the name
// to report it under.
func (l *serverLinter) linter(path string) (*lint.Linter, config.Settings, string, error) {
	cfg := l.fixed
	if l.loader != nil && path != "" {
		c, err := l.loader.For(path)
		if err != nil {
			return nil, config.Settings{}, "", err
		}
		cfg = c
	}
	s := cfg.Resolve(path)
	for _, v := range []string{l.version, l.client} { // the editor's setting wins
		if v != "" {
			s.MydumperVersion = v
		}
	}
	lin, _, _, err := l.targets.linter(s)
	name := path
	if name == "" {
		name = "untitled.cnf"
	}
	return lin, s, name, err
}

func (l *serverLinter) Check(path string, text []byte) ([]diag.Diagnostic, error) {
	lin, _, name, err := l.linter(path)
	if err != nil {
		return nil, err
	}
	return lin.Check(name, text).Diagnostics, nil
}

func (l *serverLinter) Fix(path string, text []byte) ([]byte, error) {
	lin, s, name, err := l.linter(path)
	if err != nil {
		return nil, err
	}
	res, err := lin.Fix(name, text, fix.Options{ExtendSafe: s.ExtendSafe})
	if err != nil {
		return nil, err
	}
	return res.Output, nil
}
