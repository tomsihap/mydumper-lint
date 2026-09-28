// Package cli implements the mydumper-lint command line (design §8).
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/buildinfo"
)

// Exit codes (design §8.2).
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitError    = 2
)

// env is what a command needs from the process.
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
}

type command struct {
	name    string
	summary string
	run     func(args []string, e *env) int
}

var commands []command

func init() {
	commands = []command{
		{"check", "Lint (and optionally fix) configuration files. The default command.", runCheck},
		{"rules", "List the rules.", runRules},
		{"explain", "Show the documentation of a rule.", runExplain},
		{"inspect", "Show what mydumper sees in a file: GLib's view and the effective configuration.", runInspect},
		{"server", "Run as a language server (LSP) for editors, on stdin and stdout.", runServer},
		{"versions", "List the mydumper versions mydumper-lint knows, and the default target.", runVersions},
		{"config", "Show the configuration that applies to a file, or the configuration schema.", runConfig},
		{"completion", "Print a shell completion script (bash, zsh, fish, powershell).", runCompletion},
		{"version", "Print the version of mydumper-lint.", runVersion},
	}
}

// Run executes the command line args (without the program name) and returns
// the process exit code. It never calls os.Exit, so it can be tested.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := &env{stdin: stdin, stdout: stdout, stderr: stderr, getenv: os.Getenv}
	if len(args) == 0 {
		return runCheck(nil, e)
	}
	switch args[0] {
	case "-h", "--help", "help":
		if len(args) > 1 {
			for _, c := range commands {
				if c.name == args[1] {
					return c.run([]string{"--help"}, e)
				}
			}
		}
		fmt.Fprint(stdout, mainHelp())
		return ExitOK
	case "--version", "-V":
		return runVersion(nil, e)
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:], e)
		}
	}
	return runCheck(args, e) // "mydumper-lint file.cnf" means check
}

func mainHelp() string {
	var b strings.Builder
	b.WriteString("mydumper-lint finds everything that makes mydumper and myloader silently ignore or\n" +
		"misread their configuration files, and fixes what can be fixed safely.\n\n" +
		"Usage:\n  mydumper-lint [command] [flags] [path ...]\n\nCommands:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-11s %s\n", c.name, c.summary)
	}
	b.WriteString("\nRun 'mydumper-lint <command> --help' for the flags of a command.\n" +
		"Documentation: https://github.com/tomsihap/mydumper-lint\n")
	return b.String()
}

func runVersion(_ []string, e *env) int {
	fmt.Fprintln(e.stdout, buildinfo.String())
	return ExitOK
}

// usageError prints a usage error and returns ExitError.
func usageError(e *env, cmd string, err error) int {
	fmt.Fprintf(e.stderr, "mydumper-lint %s: %v\nRun 'mydumper-lint %s --help' for usage.\n", cmd, err, cmd)
	return ExitError
}
