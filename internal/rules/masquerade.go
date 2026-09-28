package rules

import (
	"fmt"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// The masking grammar of mydumper_masquerade.c, the same in structure from
// v0.19.1-1 to v1.0.8-1 (design F10, F11). A masked column's value starts
// with a function name; mydumper reads the function's arguments at a fixed
// offset, whatever separates them from the name, and parses them with
// hand-written loops that copy each token into a 256-byte buffer.

// masqArgOffset is where mydumper starts reading a function's arguments:
// "random_format " is 14 bytes long, and so on. Functions not listed take
// modifiers after the first space instead.
var masqArgOffset = map[string]int{"random_format": 14, "apply": 6, "constant": 9, "regex": 6}

// masqBuffer is the size of the parser's token buffers.
const masqBuffer = 256

// masqIssue is a problem the parser has with a value. Offsets are relative
// to the value.
type masqIssue struct {
	msg        string
	start, end int
	undefined  bool // undefined behavior (reads or writes out of bounds), not a clean error
}

// masqGrammar is what changed in the parser over versions.
type masqGrammar struct {
	escapedQuotes    bool // v0.20.1-2: \' does not close a <regex '…'> pattern
	replaceNullFatal bool // v0.21.4-1: REPLACE_NULL 0 aborts
}

// grammarFor returns the parser of the target version (the newest when no
// version is known).
func grammarFor(p *Pass) masqGrammar {
	t, err := optionsdb.ParseTag(p.Version)
	if err != nil {
		return masqGrammar{escapedQuotes: true, replaceNullFatal: true}
	}
	return masqGrammar{
		escapedQuotes:    !t.Less(optionsdb.MustParseTag("v0.20.1-2")),
		replaceNullFatal: !t.Less(optionsdb.MustParseTag("v0.21.4-1")),
	}
}

// checkMasquerade returns what makes mydumper stop, or misbehave, on a
// masked column's value whose function is fn.
func checkMasquerade(value, fn string, g masqGrammar) []masqIssue {
	off, fixed := masqArgOffset[fn]
	if !fixed {
		return checkModifiers(value, g)
	}
	if len(value) < off {
		return []masqIssue{{
			msg: fmt.Sprintf("`%s` has no arguments: mydumper reads them from byte %d of the value, past its end",
				fn, off+1),
			start: 0, end: len(value), undefined: true,
		}}
	}
	arg := value[off:]
	var issues []masqIssue
	switch fn {
	case "apply":
		items, bad := quotedItems(arg, off)
		issues = append(issues, bad...)
		if len(bad) == 0 && (len(items) == 0 || len(items) > 2) {
			issues = append(issues, masqIssue{
				msg:   fmt.Sprintf("apply takes 1 or 2 quoted arguments, not %d (\"Parsing apply function failed\")", len(items)),
				start: off, end: len(value),
			})
		}
	case "regex":
		items, bad := quotedItems(arg, off)
		issues = append(issues, bad...)
		if len(bad) == 0 && len(items)%2 != 0 {
			issues = append(issues, masqIssue{
				msg:   fmt.Sprintf("regex takes pairs of quoted arguments (pattern, replacement), not %d (\"Parsing regex function failed\")", len(items)),
				start: off, end: len(value),
			})
		}
		for i := 0; i < len(items); i += 2 {
			if _, err := regexp2.Compile(items[i].text, regexp2.None); err != nil {
				issues = append(issues, masqIssue{
					msg:   fmt.Sprintf("the pattern %s does not compile (%v; checked with a PCRE-like engine)", quote([]byte(items[i].text)), err),
					start: items[i].start, end: items[i].end,
				})
			}
		}
	case "random_format":
		issues = append(issues, checkFormat(arg, off, g)...)
	}
	return issues
}

type masqItem struct {
	text       string
	start, end int // offsets in the value
}

// quotedItems is the loop of parse_apply_function and parse_regex_function:
// '…' items, anything else skipped one byte at a time.
func quotedItems(arg string, base int) ([]masqItem, []masqIssue) {
	var items []masqItem
	var issues []masqIssue
	for i := 0; i < len(arg); {
		if arg[i] == '\'' {
			j := strings.IndexByte(arg[i+1:], '\'')
			if j < 0 {
				issues = append(issues, masqIssue{msg: "a quote is not closed (\"Parsing format failed missing quote (')\")", start: base + i, end: base + len(arg)})
				return items, issues
			}
			text := arg[i+1 : i+1+j]
			items = append(items, masqItem{text: text, start: base + i + 1, end: base + i + 1 + j})
			if len(text) >= masqBuffer {
				issues = append(issues, overflow(base+i+1, base+i+1+j))
			}
			i += j + 2
		} else {
			i++
		}
		for i < len(arg) && arg[i] == ' ' {
			i++
		}
	}
	return items, issues
}

func overflow(start, end int) masqIssue {
	return masqIssue{
		msg:   fmt.Sprintf("an argument of %d bytes or more overflows mydumper's %d-byte buffer", masqBuffer, masqBuffer),
		start: start, end: end, undefined: true,
	}
}

// checkFormat is parse_random_format: '…' constants, <file F>, <string N>,
// <number N>, <regex '…'> tags, and delimiters.
func checkFormat(arg string, base int, g masqGrammar) []masqIssue {
	var issues []masqIssue
	for i := 0; i < len(arg); {
		switch arg[i] {
		case '\'':
			j := strings.IndexByte(arg[i+1:], '\'')
			if j < 0 {
				return append(issues, masqIssue{msg: "a quote is not closed (\"Parsing format failed missing quote (')\")", start: base + i, end: base + len(arg)})
			}
			if j >= masqBuffer {
				issues = append(issues, overflow(base+i+1, base+i+1+j))
			}
			i += j + 2
		case '<':
			start := i
			i++
			for i < len(arg) && arg[i] != '>' && arg[i] != ' ' {
				i++
			}
			tag := arg[start+1 : i]
			if i < len(arg) && arg[i] == ' ' {
				tag += " "
				i++
				for i < len(arg) && arg[i] == ' ' {
					i++
				}
				if tag == "regex " {
					if i >= len(arg) || arg[i] != '\'' {
						return append(issues, masqIssue{msg: "<regex …> needs a quoted pattern (\"Missing initial quote (') on regex\")", start: base + start, end: base + len(arg)})
					}
					end := regexEnd(arg, i+1, g.escapedQuotes)
					if end < 0 {
						return append(issues, masqIssue{msg: "the pattern of <regex …> is not closed (\"Incorrect format, EOF found\")", start: base + start, end: base + len(arg)})
					}
					pattern := arg[i+1 : end]
					if _, err := regexp2.Compile(pattern, regexp2.None); err != nil {
						issues = append(issues, masqIssue{
							msg:   fmt.Sprintf("the pattern %s does not compile (%v; checked with a PCRE-like engine)", quote([]byte(pattern)), err),
							start: base + i + 1, end: base + end,
						})
					}
					i = end
				}
				for i < len(arg) && arg[i] != '>' {
					i++
				}
			}
			if i >= len(arg) {
				return append(issues, masqIssue{msg: "a <tag> is not closed (\"Parsing format failed missing close character (>)\")", start: base + start, end: base + len(arg)})
			}
			if i-start-1 >= masqBuffer {
				issues = append(issues, overflow(base+start, base+i+1))
			}
			body := arg[start+1 : i]
			switch {
			case body == "":
			case strings.HasPrefix(body, "file "), strings.HasPrefix(body, "string "),
				strings.HasPrefix(body, "number "), strings.HasPrefix(body, "regex "):
			default:
				issues = append(issues, masqIssue{
					msg:   fmt.Sprintf("<%s> is not a tag mydumper knows: <file F>, <string N>, <number N> or <regex '…'> (\"Parsing format failed key inside <tag> not valid\")", body),
					start: base + start, end: base + i + 1,
				})
			}
			i++
		default:
			start := i
			for i < len(arg) && arg[i] != '<' && arg[i] != '\'' {
				i++
			}
			if i-start >= masqBuffer {
				issues = append(issues, overflow(base+start, base+i))
			}
		}
	}
	return issues
}

// regexEnd returns the index of the quote that closes a <regex '…'>
// pattern starting at i, or -1; escaped quotes are skipped when the version
// allows them.
func regexEnd(arg string, i int, escaped bool) int {
	for ; i < len(arg); i++ {
		if arg[i] == '\'' && (!escaped || arg[i-1] != '\\') {
			return i
		}
	}
	return -1
}

// checkModifiers is parse_basic: after the first space, words copied into
// a 256-byte buffer. REPLACE_NULL and MAX_LENGTH skip exactly one byte, then
// read their argument (past the end of the value when they are the last
// word; an empty argument when two spaces follow them). Other words are
// ignored.
func checkModifiers(value string, g masqGrammar) []masqIssue {
	sp := strings.IndexByte(value, ' ')
	if sp < 0 {
		return nil
	}
	var issues []masqIssue
	word := func(i int) int {
		for i < len(value) && value[i] != ' ' {
			i++
		}
		return i
	}
	for i := sp; i < len(value); {
		for i < len(value) && value[i] == ' ' {
			i++
		}
		start := i
		i = word(i)
		w := value[start:i]
		if len(w) >= masqBuffer {
			issues = append(issues, overflow(start, i))
		}
		replaceNull, maxLength := strings.HasPrefix(w, "REPLACE_NULL"), strings.HasPrefix(w, "MAX_LENGTH")
		if !replaceNull && !maxLength {
			continue
		}
		if i >= len(value) {
			issues = append(issues, masqIssue{
				msg:   fmt.Sprintf("%s needs an argument: mydumper reads it past the end of the value", w),
				start: start, end: i, undefined: true,
			})
			break
		}
		i++ // mydumper skips exactly one byte
		argStart := i
		i = word(i)
		if len(value[argStart:i]) >= masqBuffer {
			issues = append(issues, overflow(argStart, i))
		}
		if replaceNull && g.replaceNullFatal && atoi(value[argStart:i]) == 0 {
			issues = append(issues, masqIssue{
				msg:   "REPLACE_NULL needs a positive length right after one space (\"REPLACE_NULL receives an integer value as paramter\")", //nolint:misspell // mydumper's own message, quoted verbatim
				start: start, end: i,
			})
		}
	}
	return issues
}

// atoi is C's atoi: optional whitespace and sign, then digits.
func atoi(s string) int {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg, s = s[0] == '-', s[1:]
	}
	n := 0
	for _, c := range []byte(s) {
		if c < '0' || c > '9' || n > 1<<30 {
			break
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		return -n
	}
	return n
}

// identityConsequence says what mydumper dumps for a column masked with no
// known function (F10): the value up to v0.21.2-4, nothing from v0.21.3-1.
func identityConsequence(p *Pass) string {
	const plain = "mydumper dumps the column unmasked, in plaintext."
	const empty = "mydumper dumps the column empty: its data is silently lost."
	target, err := optionsdb.ParseTag(p.Version)
	if err != nil {
		return "mydumper dumps the column in plaintext (up to v0.21.2-4) or empty (from v0.21.3-1)."
	}
	if target.Less(optionsdb.MustParseTag("v0.21.3-1")) {
		return plain
	}
	return empty
}
