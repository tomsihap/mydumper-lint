package main

import (
	"bytes"
	"fmt"
	"strings"
)

// This file is a small C lexer. It implements translation phases 2 and 3
// (line splicing, comments, tokens) closely enough to read mydumper's source:
// the generator never compiles anything, it only needs a faithful token
// stream with line numbers and preprocessor directives kept apart.

type tokKind uint8

const (
	tkIdent  tokKind = iota + 1
	tkNumber         // preprocessing number
	tkString         // string literal, quotes and prefix included
	tkChar           // character constant, quotes and prefix included
	tkPunct          // punctuator, or any other single byte
	tkRaw            // rest of a directive line kept verbatim (#include, #error, …)
)

func (k tokKind) String() string {
	switch k {
	case tkIdent:
		return "ident"
	case tkNumber:
		return "number"
	case tkString:
		return "string"
	case tkChar:
		return "char"
	case tkPunct:
		return "punct"
	case tkRaw:
		return "raw"
	}
	return fmt.Sprintf("tokKind(%d)", k)
}

type token struct {
	kind tokKind
	text string
	line int  // 1-based physical line of the first byte
	bol  bool // first token of a logical line
}

func (t token) is(text string) bool { return t.kind == tkPunct && t.text == text }

func (t token) isIdent(name string) bool { return t.kind == tkIdent && t.text == name }

// Directives whose argument is not a token sequence: the rest of the line is
// kept as one tkRaw token so that apostrophes in #error text or header names
// such as <glib/gstdio.h> cannot derail the lexer.
var rawDirectives = map[string]bool{
	"include": true, "include_next": true, "import": true,
	"error": true, "warning": true, "pragma": true, "line": true, "ident": true, "sccs": true,
}

// Multi-byte punctuators, longest first.
var puncts = [][]byte{
	[]byte("%:%:"), []byte("..."), []byte("<<="), []byte(">>="),
	[]byte("->"), []byte("++"), []byte("--"), []byte("<<"), []byte(">>"), []byte("<="), []byte(">="),
	[]byte("=="), []byte("!="), []byte("&&"), []byte("||"), []byte("*="), []byte("/="), []byte("%="),
	[]byte("+="), []byte("-="), []byte("&="), []byte("^="), []byte("|="), []byte("##"),
	[]byte("<:"), []byte(":>"), []byte("<%"), []byte("%>"), []byte("%:"),
}

var commentEnd = []byte("*/")

// lex splits C source into tokens. Comments are dropped; a block comment is a
// space, so newlines inside it never end a directive (C11 5.1.1.2).
func lex(src []byte) ([]token, error) {
	buf, lines := splice(src)
	l := &lexer{buf: buf, lines: lines, bol: true}
	for {
		t, ok, err := l.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return l.out, nil
		}
		l.out = append(l.out, t)
	}
}

// splice removes backslash-newline pairs (phase 2) and returns, for every
// byte of the result, its physical line number.
func splice(src []byte) ([]byte, []int32) {
	buf := make([]byte, 0, len(src))
	lines := make([]int32, 0, len(src))
	line := int32(1)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\\' {
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
				line++
				continue
			}
			if i+2 < len(src) && src[i+1] == '\r' && src[i+2] == '\n' {
				i += 2
				line++
				continue
			}
		}
		buf = append(buf, c)
		lines = append(lines, line)
		if c == '\n' {
			line++
		}
	}
	return buf, lines
}

type lexer struct {
	buf   []byte
	lines []int32
	pos   int
	bol   bool
	out   []token
	// directive state: afterHash is set after a '#' that starts a line,
	// inDirective until the end of that line
	afterHash   bool
	inDirective bool
}

func (l *lexer) lineAt(p int) int {
	if p >= len(l.lines) {
		if len(l.lines) == 0 {
			return 1
		}
		return int(l.lines[len(l.lines)-1])
	}
	return int(l.lines[p])
}

func (l *lexer) errorf(p int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", l.lineAt(p), fmt.Sprintf(format, args...))
}

func (l *lexer) peek(off int) byte {
	if l.pos+off < len(l.buf) {
		return l.buf[l.pos+off]
	}
	return 0
}

// skipSpace skips whitespace and comments, recording newlines.
func (l *lexer) skipSpace() error {
	for l.pos < len(l.buf) {
		c := l.buf[l.pos]
		switch {
		case c == '\n':
			l.bol = true
			l.afterHash = false
			l.inDirective = false
			l.pos++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.pos++
		case c == '/' && l.peek(1) == '*':
			end := bytes.Index(l.buf[l.pos+2:], commentEnd)
			if end < 0 {
				return l.errorf(l.pos, "unterminated comment")
			}
			l.pos += 2 + end + 2
		case c == '/' && l.peek(1) == '/':
			for l.pos < len(l.buf) && l.buf[l.pos] != '\n' {
				l.pos++
			}
		default:
			return nil
		}
	}
	return nil
}

func (l *lexer) next() (token, bool, error) {
	if err := l.skipSpace(); err != nil {
		return token{}, false, err
	}
	if l.pos >= len(l.buf) {
		return token{}, false, nil
	}
	start := l.pos
	bol := l.bol
	l.bol = false
	c := l.buf[start]
	mk := func(k tokKind) token {
		return token{kind: k, text: string(l.buf[start:l.pos]), line: l.lineAt(start), bol: bol}
	}

	switch {
	case isIdentStart(c):
		for l.pos < len(l.buf) && isIdentChar(l.buf[l.pos]) {
			l.pos++
		}
		word := string(l.buf[start:l.pos])
		if q := l.peek(0); (q == '"' || q == '\'') && (word == "L" || word == "u" || word == "U" || word == "u8") {
			if err := l.quoted(q); err != nil {
				if l.inDirective {
					l.pos = start
					return l.rawRest(), true, nil
				}
				return token{}, false, err
			}
			if q == '"' {
				return mk(tkString), true, nil
			}
			return mk(tkChar), true, nil
		}
		t := mk(tkIdent)
		if l.afterHash {
			l.afterHash = false
			if rawDirectives[word] {
				l.out = append(l.out, t)
				return l.rawRest(), true, nil
			}
		}
		return t, true, nil
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		l.pos++
		for l.pos < len(l.buf) {
			d := l.buf[l.pos]
			if (d == '+' || d == '-') && strings.ContainsRune("eEpP", rune(l.buf[l.pos-1])) {
				l.pos++
				continue
			}
			if isIdentChar(d) || d == '.' {
				l.pos++
				continue
			}
			break
		}
		l.afterHash = false
		return mk(tkNumber), true, nil
	case c == '"' || c == '\'':
		if err := l.quoted(c); err != nil {
			// GCC only warns about a stray quote in a directive: keep going.
			if l.inDirective {
				l.pos = start
				return l.rawRest(), true, nil
			}
			return token{}, false, err
		}
		l.afterHash = false
		if c == '"' {
			return mk(tkString), true, nil
		}
		return mk(tkChar), true, nil
	}

	for _, p := range puncts {
		if bytes.HasPrefix(l.buf[start:], p) {
			l.pos += len(p)
			t := mk(tkPunct)
			l.afterHash = false
			return t, true, nil
		}
	}
	l.pos++
	t := mk(tkPunct)
	l.afterHash = bol && c == '#'
	if l.afterHash {
		l.inDirective = true
	}
	return t, true, nil
}

// quoted consumes a string literal or character constant opened by q at the
// current position.
func (l *lexer) quoted(q byte) error {
	open := l.pos
	l.pos++
	for l.pos < len(l.buf) {
		switch c := l.buf[l.pos]; c {
		case '\\':
			l.pos += 2
		case '\n':
			return l.errorf(open, "unterminated %c literal", q)
		case q:
			l.pos++
			return nil
		default:
			l.pos++
		}
	}
	return l.errorf(open, "unterminated %c literal", q)
}

// rawRest returns the rest of the current directive line as one token.
// Comments are still removed.
func (l *lexer) rawRest() token {
	for l.pos < len(l.buf) && (l.buf[l.pos] == ' ' || l.buf[l.pos] == '\t') {
		l.pos++
	}
	start := l.pos
	var sb strings.Builder
	for l.pos < len(l.buf) && l.buf[l.pos] != '\n' {
		if l.buf[l.pos] == '/' && l.peek(1) == '*' {
			end := bytes.Index(l.buf[l.pos+2:], commentEnd)
			if end < 0 {
				l.pos = len(l.buf)
				break
			}
			l.pos += 2 + end + 2
			sb.WriteByte(' ')
			continue
		}
		if l.buf[l.pos] == '/' && l.peek(1) == '/' {
			for l.pos < len(l.buf) && l.buf[l.pos] != '\n' {
				l.pos++
			}
			break
		}
		sb.WriteByte(l.buf[l.pos])
		l.pos++
	}
	return token{kind: tkRaw, text: strings.TrimSpace(sb.String()), line: l.lineAt(start)}
}

func isIdentStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }
func isIdentChar(c byte) bool  { return isIdentStart(c) || isDigit(c) }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }

// unquote decodes a C string literal or character constant token (prefix
// allowed) into its bytes. Escapes follow C11 6.4.4.4.
func unquote(lit string) (string, error) {
	i := strings.IndexAny(lit, `"'`)
	if i < 0 || len(lit) < i+2 || lit[len(lit)-1] != lit[i] {
		return "", fmt.Errorf("not a literal: %s", lit)
	}
	body := lit[i+1 : len(lit)-1]
	var sb strings.Builder
	for j := 0; j < len(body); j++ {
		c := body[j]
		if c != '\\' {
			sb.WriteByte(c)
			continue
		}
		j++
		if j >= len(body) {
			return "", fmt.Errorf("dangling escape in %s", lit)
		}
		switch e := body[j]; e {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case 'a':
			sb.WriteByte('\a')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'v':
			sb.WriteByte('\v')
		case '\\', '\'', '"', '?':
			sb.WriteByte(e)
		case 'x':
			v, n := 0, 0
			for j+1 < len(body) && isHex(body[j+1]) {
				j++
				v = v*16 + hexVal(body[j])
				n++
			}
			if n == 0 {
				return "", fmt.Errorf("bad \\x escape in %s", lit)
			}
			sb.WriteByte(byte(v))
		default:
			if e < '0' || e > '7' {
				return "", fmt.Errorf("unknown escape \\%c in %s", e, lit)
			}
			v := int(e - '0')
			for n := 1; n < 3 && j+1 < len(body) && body[j+1] >= '0' && body[j+1] <= '7'; n++ {
				j++
				v = v*8 + int(body[j]-'0')
			}
			sb.WriteByte(byte(v))
		}
	}
	return sb.String(), nil
}

func isHex(c byte) bool { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }

func hexVal(c byte) int {
	if isDigit(c) {
		return int(c - '0')
	}
	return int(c|0x20-'a') + 10
}
