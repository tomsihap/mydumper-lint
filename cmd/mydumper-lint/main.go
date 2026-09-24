// Command mydumper-lint lints and fixes mydumper and myloader configuration
// files. See https://github.com/tomsihap/mydumper-lint.
package main

import (
	"os"

	"github.com/tomsihap/mydumper-lint/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
