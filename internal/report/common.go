package report

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// toolName and toolURI identify mydumper-lint in machine-readable formats.
const (
	toolName = "mydumper-lint"
	toolURI  = "https://github.com/tomsihap/mydumper-lint"
)

// fileView converts the byte offsets of one result into user-facing
// positions (design §4.6). Offsets outside the file are clamped to it, so a
// bogus span can never make a reporter panic.
type fileView struct {
	path string
	src  *source.File
}

func newFileView(r FileResult) fileView {
	return fileView{path: r.Path, src: source.New(r.Path, r.Source)}
}

func (v fileView) clamp(off int) int { return min(max(off, 0), len(v.src.Bytes)) }

// span returns the start and end positions of sp. An inverted span is treated
// as empty at its start.
func (v fileView) span(sp diag.Span) (start, end source.Position) {
	s := v.clamp(sp.Start)
	e := max(v.clamp(sp.End), s)
	return v.src.Position(s), v.src.Position(e)
}

// line returns the content of line n without its '\n', or nil for the
// virtual line after a final newline (or the only line of an empty file).
func (v fileView) line(n int) []byte {
	if n > len(v.src.Lines) {
		return nil
	}
	return v.src.Content(n)
}

// lineStart returns the offset of the first byte of line n.
func (v fileView) lineStart(n int) int {
	if n > len(v.src.Lines) {
		return len(v.src.Bytes)
	}
	return v.src.Lines[n-1].Start
}

// ruleLabel is "MDL102[whitespace-only-line]", or the bare ID without a name.
func ruleLabel(d diag.Diagnostic) string {
	if d.RuleName == "" {
		return d.RuleID
	}
	return d.RuleID + "[" + d.RuleName + "]"
}

// fullMessage is the message followed by the mydumper consequence, for the
// formats that carry a single text per diagnostic.
func fullMessage(d diag.Diagnostic) string {
	if d.Consequence == "" {
		return d.Message
	}
	return d.Message + " (mydumper: " + d.Consequence + ")"
}

// fixCommand is the command line that applies f.
func fixCommand(f *diag.Fix) string {
	if f.Applicability == diag.Safe {
		return "--fix"
	}
	return "--fix --unsafe-fixes"
}

// fingerprints returns a stable identifier for each diagnostic of r: the
// SHA-256 of the rule ID, the slash-separated path, the whitespace-normalized
// content of the diagnostic's first line, and the rank of the diagnostic
// among the earlier ones sharing these three values. Line numbers play no
// part, so an identifier survives lines being added or removed above the
// diagnostic, and the rank keeps identifiers unique within a report.
func fingerprints(r FileResult, v fileView) []string {
	seen := make(map[string]int, len(r.Diagnostics))
	out := make([]string, len(r.Diagnostics))
	path := filepath.ToSlash(r.Path)
	for i, d := range r.Diagnostics {
		start, _ := v.span(d.Span)
		key := d.RuleID + "\x00" + path + "\x00" + normalizeLine(v.line(start.Line))
		rank := seen[key]
		seen[key] = rank + 1
		sum := sha256.Sum256([]byte(key + "\x00" + strconv.Itoa(rank)))
		out[i] = hex.EncodeToString(sum[:])
	}
	return out
}

// normalizeLine trims a line and collapses its inner whitespace, so that
// re-indenting a line or changing its line ending keeps its fingerprint.
func normalizeLine(b []byte) string {
	return strings.Join(strings.Fields(string(b)), " ")
}

// plural formats a count and a noun: "1 error", "0 errors", "2 infos".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
