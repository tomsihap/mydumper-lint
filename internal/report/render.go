package report

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// tabWidth is the number of columns a tab expands to in source excerpts.
const tabWidth = 4

// cell is the terminal-safe rendering of one code point, or of one invalid
// UTF-8 byte, of some text.
type cell struct {
	off   int    // byte offset in the rendered text
	text  string // what is printed
	width int    // terminal columns occupied by text
}

// cells renders b so that it is safe to print on a terminal and stays on one
// line: control characters become control pictures (NUL is ␀, CR is ␍),
// invalid UTF-8 bytes become \xNN, and invisible or non-graphic code points
// (BOM, bidirectional controls, C1 controls) become \u{…}. In excerpts
// (inline false), a tab expands to tabWidth spaces; inline, it is shown as ␉.
func cells(b []byte, inline bool) []cell {
	var out []cell
	for off := 0; off < len(b); {
		r, size := utf8.DecodeRune(b[off:])
		c := cell{off: off}
		switch {
		case r == utf8.RuneError && size == 1:
			c.text, c.width = fmt.Sprintf(`\x%02x`, b[off]), 4
		case r == '\t' && !inline:
			c.text, c.width = strings.Repeat(" ", tabWidth), tabWidth
		case r < 0x20:
			c.text, c.width = string(0x2400+r), 1
		case r == 0x7f:
			c.text, c.width = "\U00002421", 1
		case !unicode.IsGraphic(r):
			c.text = fmt.Sprintf(`\u{%x}`, r)
			c.width = len(c.text)
		default: // a valid, graphic code point: print its bytes
			c.text, c.width = string(b[off:off+size]), runeWidth(r)
		}
		out = append(out, c)
		off += size
	}
	return out
}

// sanitize makes free text (paths, messages) safe to print on a terminal, on
// a single line. Printable ASCII is returned unchanged.
func sanitize(s string) string {
	plain := true
	for i := 0; i < len(s) && plain; i++ {
		plain = s[i] >= 0x20 && s[i] < 0x7f
	}
	if plain {
		return s
	}
	var sb strings.Builder
	for _, c := range cells([]byte(s), true) {
		sb.WriteString(c.text)
	}
	return sb.String()
}

// excerpt is a rendered source line and the columns of a span underlined on it.
type excerpt struct {
	text  string // the rendered line, trailing spaces trimmed
	col   int    // 0-based rendered column where the underline starts
	width int    // number of underlined columns, at least 1
}

// makeExcerpt renders line and underlines the bytes in [from, to), offsets
// relative to the start of the line. An empty range, or one that starts at
// the end of the line, gets a single caret at its position.
func makeExcerpt(line []byte, from, to int) excerpt {
	var sb strings.Builder
	col, width := 0, 0
	for _, c := range cells(line, false) {
		switch {
		case c.off < from:
			col += c.width
		case c.off < to:
			width += c.width
		}
		sb.WriteString(c.text)
	}
	return excerpt{text: strings.TrimRight(sb.String(), " "), col: col, width: max(width, 1)}
}

// runeWidth returns the number of terminal columns r occupies: 0 for
// combining marks, 2 for East Asian wide and fullwidth characters, 1 otherwise.
func runeWidth(r rune) int {
	switch {
	case r < 0x300: // before the first combining mark and the first wide block
		return 1
	case unicode.In(r, unicode.Mn, unicode.Me):
		return 0
	case isWide(r):
		return 2
	}
	return 1
}

// wideRanges approximates the East Asian Wide (W) and Fullwidth (F) blocks of
// Unicode's EastAsianWidth.txt: Hangul Jamo, CJK, Yi, Hangul syllables,
// fullwidth forms and the common emoji blocks.
var wideRanges = [...][2]rune{
	{0x1100, 0x115f}, {0x2329, 0x232a}, {0x2e80, 0x303e}, {0x3041, 0x33ff},
	{0x3400, 0x4dbf}, {0x4e00, 0x9fff}, {0xa000, 0xa4cf}, {0xa960, 0xa97f},
	{0xac00, 0xd7a3}, {0xf900, 0xfaff}, {0xfe10, 0xfe19}, {0xfe30, 0xfe6f},
	{0xff00, 0xff60}, {0xffe0, 0xffe6}, {0x1f300, 0x1f64f}, {0x1f900, 0x1f9ff},
	{0x20000, 0x2fffd}, {0x30000, 0x3fffd},
}

func isWide(r rune) bool {
	for _, w := range wideRanges {
		if r >= w[0] && r <= w[1] {
			return true
		}
	}
	return false
}
