package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/tomsihap/mydumper-lint/internal/config"
)

const configHelp = `Usage:
  mydumper-lint config path [FILE]    configuration file that applies to FILE (default: .)
  mydumper-lint config show [FILE]    effective settings for FILE, as JSON
  mydumper-lint config schema         JSON Schema of .mydumper-lint.yaml
`

func runConfig(args []string, e *env) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	pos, err := parse(fs, args)
	if errors.Is(err, errHelp) || (err == nil && len(pos) == 0) {
		fmt.Fprint(e.stdout, configHelp)
		return ExitOK
	}
	if err != nil {
		return usageError(e, "config", err)
	}
	target := "x.cnf"
	if len(pos) > 1 {
		target = pos[1]
	}
	switch pos[0] {
	case "schema":
		b, err := config.Schema()
		if err != nil {
			return ExitError
		}
		e.stdout.Write(b)
		return ExitOK
	case "path", "show":
		cfg, err := config.NewLoader().For(target)
		if err != nil {
			fmt.Fprintf(e.stderr, "mydumper-lint: %v\n", err)
			return ExitError
		}
		if pos[0] == "path" {
			if cfg == nil {
				fmt.Fprintln(e.stdout, "(no configuration file: defaults apply)")
			} else {
				fmt.Fprintln(e.stdout, cfg.Path)
			}
			return ExitOK
		}
		s := cfg.Resolve(target)
		out := map[string]any{
			"config_file":      "",
			"mydumper_version": s.MydumperVersion,
			"mydumper_build":   map[string]any{"client": s.Build.Client, "ssl": *s.Build.SSL},
			"fail_on":          s.FailOn,
			"preview":          s.Preview,
			"select":           nonNil(s.Selection.Select),
			"extend_select":    nonNil(s.Selection.ExtendSelect),
			"ignore":           nonNil(s.Selection.Ignore),
			"include":          nonNil(s.Include),
			"exclude":          nonNil(s.Exclude),
			"conventions":      s.Conventions != nil,
		}
		if cfg != nil {
			out["config_file"] = cfg.Path
		}
		sev := map[string]string{}
		for k, v := range s.Selection.Severity {
			sev[k] = v.String()
		}
		out["severity"] = sev
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return ExitError
		}
		return ExitOK
	}
	return usageError(e, "config", fmt.Errorf("unknown subcommand %q", pos[0]))
}
