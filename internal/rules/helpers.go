package rules

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

// rejectionConsequence says what mydumper does with a file GLib rejects (F1),
// adapted to the file: the [client] hand-off (F12) and dropped masking rules.
func rejectionConsequence(p *Pass) string {
	var b strings.Builder
	b.WriteString("mydumper ignores this entire file: it only logs a WARNING " +
		"(\"Failed to load config file\") and runs with its defaults, exit code 0.")
	if hasGroup(p, "client") {
		b.WriteString(" The [client] section is still read by the MySQL client library," +
			" so the connection works and the run looks normal.")
	}
	if hasMaskedColumn(p) {
		b.WriteString(" The masking rules are NOT applied: masked columns are dumped in plaintext.")
	}
	return b.String()
}

func hasGroup(p *Pass, name string) bool {
	for _, g := range p.KF.Groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

// hasMaskedColumn reports whether a table section declares a masked column (F9).
func hasMaskedColumn(p *Pass) bool {
	for _, e := range p.KF.Entries {
		if model.IsTableGroup(p.KF.Groups[e.Group].Name) && isMaskedColumnKey(e.Key) {
			return true
		}
	}
	return false
}

// isMaskedColumnKey: starts with a backtick and contains a second one (F9).
func isMaskedColumnKey(k string) bool {
	return len(k) > 1 && k[0] == '`' && strings.IndexByte(k[1:], '`') >= 0
}

// glibSays formats a GLib error for a message. GLib quotes the raw line, so
// control characters are escaped to keep terminals and reports intact.
func glibSays(msg string) string {
	var b strings.Builder
	for _, r := range msg {
		switch {
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return "GLib: " + b.String()
}

// content returns line n (1-based) without a trailing '\r'.
func content(p *Pass, n int) []byte {
	c := p.File.Content(n)
	if len(c) > 0 && c[len(c)-1] == '\r' {
		return c[:len(c)-1]
	}
	return c
}

// lineSpan is the span of line n's content, without a trailing '\r'.
func lineSpan(p *Pass, n int) diag.Span {
	l := p.File.Line(n)
	return diag.Span{Start: l.Start, End: l.Start + len(content(p, n))}
}

// deleteLine is an edit removing line n and its '\n'.
func deleteLine(p *Pass, n int) diag.Edit {
	s, e := p.File.LineSpan(n, true)
	return diag.Edit{Start: s, End: e}
}

// newline returns the terminator to use for a line inserted next to line n.
func newline(p *Pass, n int) string {
	if p.File.EndsWithCR(n) && p.File.Line(n).HasNewline {
		return "\r\n"
	}
	return "\n"
}

// forCause calls fn for every rejected line with the given cause.
func forCause(p *Pass, c keyfile.Cause, fn func(n int, lc keyfile.LineClass)) {
	for i, lc := range p.KF.Lines {
		if lc.Kind == keyfile.KindRejected && lc.Cause == c {
			fn(i+1, lc)
		}
	}
}

// isBlankish reports whether b only holds GLib whitespace (space, tab, form
// feed, carriage return).
func isBlankish(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\f' && c != '\r' {
			return false
		}
	}
	return true
}

// quote shows a fragment of a line in a message, escaping what a terminal
// would not show, and truncating long fragments.
func quote(b []byte) string {
	const maxRunes = 60
	// Markdown convention: a fragment containing a backtick is wrapped in
	// double backticks and spaces.
	open, closing := "`", "`"
	if bytes.IndexByte(b, '`') >= 0 {
		open, closing = "`` ", " ``"
	}
	var sb strings.Builder
	sb.WriteString(open)
	n := 0
	for len(b) > 0 && n < maxRunes {
		r, size := utf8.DecodeRune(b)
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&sb, `\x%02x`, b[0])
		case r == '\t':
			sb.WriteString(`\t`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&sb, `\x%02x`, r)
		default:
			sb.WriteRune(r)
		}
		b = b[size:]
		n++
	}
	if len(b) > 0 {
		sb.WriteString("…")
	}
	sb.WriteString(closing)
	return sb.String()
}

// controlName names a control character for messages.
func controlName(c byte) string {
	names := map[byte]string{
		0x00: "NUL", 0x07: "BEL", 0x09: "TAB", 0x08: "BACKSPACE", 0x0b: "VERTICAL TAB", 0x0c: "FORM FEED",
		0x1b: "ESCAPE", 0x7f: "DELETE",
	}
	if n, ok := names[c]; ok {
		return fmt.Sprintf("U+%04X %s", c, n)
	}
	return fmt.Sprintf("U+%04X", c)
}

// firstSignificant returns the index of the first non-whitespace byte of b
// (GLib whitespace: space, \t, \f, \r), or len(b).
func firstSignificant(b []byte) int {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\f' || b[i] == '\r') {
		i++
	}
	return i
}

var bom = []byte{0xef, 0xbb, 0xbf}

func hasBOM(b []byte) bool { return bytes.HasPrefix(b, bom) }
