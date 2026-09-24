// Package cli implements the mydumper-lint command line (design §8).
package cli

import (
	"fmt"
	"io"

	"github.com/tomsihap/mydumper-lint/internal/buildinfo"
)

// Exit codes (design §8.2).
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitError    = 2
)

// Run executes the command line args (without the program name) and returns
// the process exit code. It never calls os.Exit, so it can be tested.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, buildinfo.String())
		return ExitOK
	}
	fmt.Fprintln(stderr, "mydumper-lint: only the version command exists at this stage")
	return ExitError
}
