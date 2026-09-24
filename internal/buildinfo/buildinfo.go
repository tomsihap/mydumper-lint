// Package buildinfo exposes the version of the running binary.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

// Set at build time with -ldflags "-X github.com/tomsihap/mydumper-lint/internal/buildinfo.Version=…".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// String returns a one-line description such as
// "mydumper-lint 0.1.0 (commit 1a2b3c4, built 2026-09-24)".
func String() string {
	commit, date := Commit, Date
	if commit == "" || date == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch {
				case s.Key == "vcs.revision" && commit == "":
					commit = s.Value
				case s.Key == "vcs.time" && date == "":
					date = s.Value
				}
			}
		}
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if len(date) > 10 {
		date = date[:10]
	}
	out := "mydumper-lint " + Version
	switch {
	case commit != "" && date != "":
		out += fmt.Sprintf(" (commit %s, built %s)", commit, date)
	case commit != "":
		out += fmt.Sprintf(" (commit %s)", commit)
	}
	return out
}
