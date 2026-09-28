// Package playground runs mydumper-lint on one file held in memory, for the
// WebAssembly playground (cmd/mydumper-lint-wasm, web/playground). It drives
// the command line itself, with --no-config and the file on stdin, so the
// playground and the binary always agree: what the page shows is what
// `mydumper-lint check --mydumper-version V FILE` prints.
package playground

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/buildinfo"
	"github.com/tomsihap/mydumper-lint/internal/cli"
)

// DefaultName is the file name used in messages when the request has none.
const DefaultName = "input.cnf"

// Request is one file to analyze.
type Request struct {
	Source  []byte
	Name    string // shown in messages; DefaultName when empty
	Version string // mydumper version spec: "v1.0.5-1", "0.19", "latest"…; "latest" when empty
}

// Fix is the outcome of `check --fix` (or --fix --unsafe-fixes) on the file.
type Fix struct {
	Output  []byte   `json:"output"`  // the fixed file, byte for byte (base64 in JSON)
	Diff    string   `json:"diff"`    // unified diff from the file to Output; empty when nothing changed
	Notes   []string `json:"notes"`   // fixes the tool refused to apply, and why
	Failure string   `json:"failure"` // why fixing failed (the file is then unchanged); empty on success
}

// Result is everything the page shows for one file.
type Result struct {
	// Check is the JSON report of `check --format json` (schemas/output.v1.json).
	Check json.RawMessage `json:"check"`
	// Inspect is the JSON document of `inspect --format json`.
	Inspect json.RawMessage `json:"inspect"`
	Safe    Fix             `json:"safe"`
	Unsafe  Fix             `json:"unsafe"`
	// Notices are what the tool says besides the report, such as a version
	// newer than every version it knows.
	Notices []string `json:"notices"`
	// Command is the equivalent command line, to reproduce the result.
	Command string `json:"command"`
	// Error is set when the tool could not analyze the file at all (for
	// example an unknown mydumper version); the other fields are then empty.
	Error string `json:"error,omitempty"`
}

// Analyze runs the checks, the fixer and inspect on the file.
func Analyze(req Request) Result {
	name, version := req.Name, req.Version
	if name == "" {
		name = DefaultName
	}
	if version == "" {
		version = "latest"
	}
	common := []string{"--no-config", "--mydumper-version", version}
	res := Result{Command: "mydumper-lint check --mydumper-version " + version + " " + shellQuote(name), Notices: []string{}}

	code, out, errOut := run(req.Source, append([]string{"check", "--format", "json", "--stdin-filename", name}, append(common, "-")...))
	if code == cli.ExitError || !json.Valid(out) {
		res.Error = firstMessage(errOut)
		return res
	}
	res.Check, res.Notices = out, messages(errOut)
	_, out, _ = run(req.Source, append([]string{"inspect", "--format", "json"}, append(common, "-")...))
	if json.Valid(out) {
		res.Inspect = out
	}
	res.Safe = fix(req.Source, name, common, false)
	res.Unsafe = fix(req.Source, name, common, true)
	return res
}

func fix(src []byte, name string, common []string, unsafe bool) Fix {
	args := []string{"check", "--fix", "--format", "concise", "--stdin-filename", name}
	if unsafe {
		args = append(args, "--unsafe-fixes")
	}
	code, out, errOut := run(src, append(args, append(common, "-")...))
	f := Fix{Output: out, Notes: []string{}}
	for _, msg := range messages(errOut) {
		if code == cli.ExitError {
			f.Failure = msg
		} else if strings.Contains(msg, "not applied:") {
			f.Notes = append(f.Notes, msg)
		}
	}
	if code == cli.ExitError {
		f.Output = src
		if f.Failure == "" {
			f.Failure = "the fixer failed"
		}
		return f
	}
	if !bytes.Equal(out, src) {
		_, diff, _ := run(src, append([]string{"check", "--diff", "--stdin-filename", name}, append(fixFlags(unsafe), append(common, "-")...)...))
		f.Diff = string(diff)
	}
	return f
}

func fixFlags(unsafe bool) []string {
	if unsafe {
		return []string{"--unsafe-fixes"}
	}
	return nil
}

// Versions returns the JSON of `versions --format json`.
func Versions() json.RawMessage {
	_, out, _ := run(nil, []string{"versions", "--format", "json"})
	return out
}

// Rules returns the JSON of `rules --format json`.
func Rules() json.RawMessage {
	_, out, _ := run(nil, []string{"rules", "--format", "json"})
	return out
}

// Build describes the running mydumper-lint.
func Build() string { return buildinfo.String() }

func run(stdin []byte, args []string) (int, []byte, []byte) {
	var out, errOut bytes.Buffer
	code := cli.Run(args, bytes.NewReader(stdin), &out, &errOut)
	return code, out.Bytes(), errOut.Bytes()
}

// messagePrefix is how the tool starts its messages: "mydumper-lint: " or
// "mydumper-lint check: ".
var messagePrefix = regexp.MustCompile(`^mydumper-lint(?: [a-z]+)?: `)

// messages returns the tool's own lines of stderr, without their prefix
// (diagnostics printed there by --fix are left out).
func messages(stderr []byte) []string {
	out := []string{}
	for _, line := range strings.Split(string(stderr), "\n") {
		if loc := messagePrefix.FindStringIndex(line); loc != nil {
			out = append(out, line[loc[1]:])
		}
	}
	return out
}

// firstMessage returns the first message of stderr, or a generic one.
func firstMessage(stderr []byte) string {
	if ms := messages(stderr); len(ms) > 0 {
		return ms[0]
	}
	if s := strings.TrimSpace(string(stderr)); s != "" {
		return s
	}
	return "mydumper-lint could not analyze the file"
}

// shellQuote quotes a file name for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-/") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
