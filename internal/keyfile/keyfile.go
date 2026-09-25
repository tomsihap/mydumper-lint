// Package keyfile emulates GLib's GKeyFile parser on the output of mydumper's
// pre-processor (design §3.3).
//
// Parse classifies every line the way g_key_file_load_from_data would, and
// keeps going after the first error so that all problems can be reported in
// one run. The first error is what mydumper actually sees (K20). Every
// rejected line gets exactly one Cause, which the MDL1xx rules map to.
//
// Recover computes, independently of the rules and their fixes, the file the
// author meant: rejected lines are repaired by fixed recovery rules (design
// §7.2). The fixer's self-check compares the models of recovered files.
package keyfile

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/preprocess"
	"github.com/tomsihap/mydumper-lint/internal/source"
)

// Kind classifies a line.
type Kind uint8

// Line kinds.
const (
	KindBlank    Kind = iota // empty line (after the CR strip)
	KindComment              // first significant byte is '#' or NUL, or only whitespace
	KindGroup                // valid group header
	KindEntry                // valid key/value pair
	KindRejected             // GLib rejects the line, hence the whole file
)

var kindNames = [...]string{KindBlank: "blank", KindComment: "comment", KindGroup: "group", KindEntry: "entry", KindRejected: "rejected"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Cause explains why a line is rejected.
type Cause uint8

// Rejection causes. Each maps to exactly one MDL1xx rule.
const (
	NoCause                  Cause = iota
	CauseBOM                       // UTF-8 BOM at the start of the file (MDL101)
	CauseWhitespaceOnly            // spaces/tabs only, turned into "  = 1" (MDL102)
	CauseCarriageReturn            // a line made of '\r' only, turned into "\r= 1" (MDL103)
	CauseBracketLeakBlank          // empty line turned into "= 1" by the state leak (MDL108 a)
	CauseBracketLeakFlag           // flag that did not receive "= 1" because of the leak (MDL108 b)
	CauseMissingFinalNewline       // flag on the last line, which has no '\n' (MDL104)
	CauseInvalidGroupLine          // malformed group header (MDL106)
	CauseEmptyKey                  // first significant character is '=' (MDL107)
	CauseKeyBeforeGroup            // key/value before any group (MDL105)
	CauseInvalidKeyName            // ']' in a key, or a malformed [locale] (MDL110)
	CauseNulByte                   // a NUL byte hides the rest of the line (MDL111)
	CauseBracketNoValue            // no '=' before a '[': the '[' copy skips "= 1" (MDL108 c)
	CauseUnknown                   // none of the above (MDL109)
)

var causeNames = [...]string{
	NoCause:                  "none",
	CauseBOM:                 "bom",
	CauseWhitespaceOnly:      "whitespace-only",
	CauseCarriageReturn:      "carriage-return",
	CauseBracketLeakBlank:    "bracket-leak-blank",
	CauseBracketLeakFlag:     "bracket-leak-flag",
	CauseMissingFinalNewline: "missing-final-newline",
	CauseInvalidGroupLine:    "invalid-group-line",
	CauseEmptyKey:            "empty-key",
	CauseKeyBeforeGroup:      "key-before-group",
	CauseInvalidKeyName:      "invalid-key-name",
	CauseNulByte:             "nul-byte",
	CauseBracketNoValue:      "bracket-no-value",
	CauseUnknown:             "unknown",
}

func (c Cause) String() string {
	if int(c) < len(causeNames) {
		return causeNames[c]
	}
	return "Cause(" + strconv.Itoa(int(c)) + ")"
}

// LineClass is the classification of one line.
type LineClass struct {
	Kind    Kind
	Cause   Cause  // Rejected lines only
	Message string // Rejected lines only: GLib's exact message
	Group   int    // index in Result.Groups of the group the line belongs to; -1 before any header
	Header  int    // for header lines (valid or not): index in Result.Groups; else -1
	Entry   int    // for Entry lines: index in Result.Entries; else -1
}

// Group is one group header line. Duplicate headers are kept (GLib merges them).
type Group struct {
	Name     string    // raw name bytes (best effort for rejected headers)
	Line     int       // 1-based
	Valid    bool      // GLib accepted the header
	NameSpan diag.Span // bytes of the name in the original file
}

// Entry is one key/value pair accepted by GLib.
type Entry struct {
	Group       int    // index in Result.Groups
	Line        int    // 1-based
	Key         string // as stored by GLib, including a [locale] suffix
	Value       string // raw value (leading whitespace removed, cut at a NUL)
	Locale      string // locale suffix without brackets; empty if none
	KeySpan     diag.Span
	EqOffset    int       // offset of '=' in the original file; -1 when synthesized
	ValueSpan   diag.Span // insertion point when Synthesized
	Synthesized bool      // "= 1" was added by mydumper's pre-processor
}

// LineError is a GLib load error on a line.
type LineError struct {
	Line    int
	Message string
}

// Result is the classification of a whole file.
type Result struct {
	Loadable   bool
	FirstError *LineError // what mydumper logs; nil when Loadable
	Lines      []LineClass
	Groups     []Group
	Entries    []Entry
}

var bom = []byte{0xef, 0xbb, 0xbf}

// GLib messages (design §3.3).
const (
	msgNotKV      = "Key file contains line “%s” which is not a key-value pair, group, or comment"
	msgNoGroup    = "Key file does not start with a group"
	msgGroupName  = "Invalid group name: "
	msgInvalidKey = "Invalid key name: "
)

func notKVMessage(line []byte) string {
	return strings.Replace(msgNotKV, "%s", makeValid(line), 1)
}

// Parse classifies every line of f, given the pre-processor's decisions.
func Parse(f *source.File, pre *preprocess.Result) *Result {
	p := &parser{f: f, r: &Result{Lines: make([]LineClass, len(f.Lines))}, cur: -1}
	for i, l := range f.Lines {
		p.line(l, pre.Lines[i])
	}
	p.r.Loadable = p.r.FirstError == nil
	return p.r
}

type parser struct {
	f   *source.File
	r   *Result
	cur int // current group; -1 is GLib's nameless start group
}

// glibLine returns the bytes GLib parses for line l: the content, the "= 1"
// inserted by the pre-processor, the NUL a '[' copy reads at EOF, minus the
// '\r' GLib strips before '\n'.
func glibLine(content []byte, l source.Line, info preprocess.LineInfo) []byte {
	g := make([]byte, 0, len(content)+4)
	g = append(g, content...)
	if info.AppendsEqOne {
		g = append(g, preprocess.EqOne...)
	}
	if !l.HasNewline && info.BracketAt >= 0 {
		g = append(g, 0)
	}
	if l.HasNewline && len(g) > 0 && g[len(g)-1] == '\r' {
		g = g[:len(g)-1]
	}
	return g
}

func (p *parser) line(l source.Line, info preprocess.LineInfo) {
	content := p.f.Content(l.Num)
	g := glibLine(content, l, info)
	lc := LineClass{Group: p.cur, Header: -1, Entry: -1}
	idx := l.Num - 1
	if len(g) == 0 {
		lc.Kind = KindBlank
		p.r.Lines[idx] = lc
		return
	}
	res := classify(g, p.cur >= 0)
	switch res.kind {
	case KindComment:
		lc.Kind = KindComment
	case KindGroup:
		lc.Kind = KindGroup
		lc.Header = p.addGroup(res.name, l, res.nameOff, true)
		p.cur = lc.Header
		lc.Group = p.cur
	case KindEntry:
		lc.Kind = KindEntry
		lc.Entry = p.addEntry(res, content, l)
	case KindBlank: // not produced by classify; kept for exhaustiveness
		lc.Kind = KindBlank
	case KindRejected:
		lc.Kind = KindRejected
		lc.Message = res.message
		lc.Cause = cause(l, info, content, g, res)
		if p.r.FirstError == nil {
			p.r.FirstError = &LineError{Line: l.Num, Message: res.message}
		}
		if name, off, ok := p.bestEffortHeader(content, lc.Cause); ok {
			lc.Header = p.addGroup(name, l, off, false)
			p.cur = lc.Header
			lc.Group = p.cur
		}
	}
	p.r.Lines[idx] = lc
}

func (p *parser) addGroup(name []byte, l source.Line, nameOff int, valid bool) int {
	start := l.Start + nameOff
	p.r.Groups = append(p.r.Groups, Group{
		Name:     string(name),
		Line:     l.Num,
		Valid:    valid,
		NameSpan: diag.Span{Start: start, End: start + len(name)},
	})
	return len(p.r.Groups) - 1
}

func (p *parser) addEntry(res classified, content []byte, l source.Line) int {
	e := Entry{
		Group:  p.cur,
		Line:   l.Num,
		Key:    string(res.key),
		Value:  string(res.value),
		Locale: res.locale,
		KeySpan: diag.Span{
			Start: l.Start + res.keyOff,
			End:   l.Start + res.keyOff + len(res.key),
		},
		EqOffset: -1,
	}
	if res.eqOff < len(content) {
		e.EqOffset = l.Start + res.eqOff
		e.ValueSpan = diag.Span{Start: l.Start + res.valueOff, End: l.Start + res.valueOff + len(res.value)}
	} else {
		e.Synthesized = true
		at := l.Start + len(content)
		if len(content) > 0 && content[len(content)-1] == '\r' {
			at--
		}
		e.ValueSpan = diag.Span{Start: at, End: at}
	}
	p.r.Entries = append(p.r.Entries, e)
	return len(p.r.Entries) - 1
}

// bestEffortHeader gives rejected header-like lines a placeholder group, so
// that the lines below are attributed to what the author meant.
func (p *parser) bestEffortHeader(content []byte, c Cause) (name []byte, off int, ok bool) {
	b := content
	shift := 0
	if c == CauseBOM {
		b, shift = content[len(bom):], len(bom)
	}
	ls := 0
	for ls < len(b) && isSpace(b[ls]) {
		ls++
	}
	if ls >= len(b) || b[ls] != '[' {
		return nil, 0, false
	}
	rest := b[ls+1:]
	if end := bytes.IndexByte(rest, ']'); end >= 0 {
		rest = rest[:end]
	}
	if i := bytes.IndexByte(rest, 0); i >= 0 {
		rest = rest[:i]
	}
	return rest, shift + ls + 1, true
}

// classified is the outcome of GLib's parse_line on one line.
type classified struct {
	kind     Kind
	message  string
	failure  failure
	name     []byte // group name
	nameOff  int
	key      []byte
	keyOff   int
	eqOff    int
	value    []byte
	valueOff int
	locale   string
}

type failure uint8

const (
	failNone failure = iota
	failNotKV
	failGroupName
	failNoGroup
	failKeyName
)

// classify reproduces g_key_file_parse_line on g. hasGroup tells whether a
// named group is current (GLib's current_group->name != NULL).
func classify(g []byte, hasGroup bool) classified {
	cs := g // C-string view: GLib's scanning functions stop at a NUL
	if i := bytes.IndexByte(g, 0); i >= 0 {
		cs = g[:i]
	}
	ls := 0
	for ls < len(cs) && isSpace(cs[ls]) {
		ls++
	}
	if ls == len(cs) || cs[ls] == '#' {
		return classified{kind: KindComment}
	}
	if isGroup(cs[ls:]) {
		gl := g[ls:]
		end := len(gl) - 1
		for gl[end] != ']' {
			end--
		}
		name := gl[1:end]
		if i := bytes.IndexByte(name, 0); i >= 0 { // g_strndup stops at a NUL
			name = name[:i]
		}
		if !isGroupName(name) {
			return classified{kind: KindRejected, failure: failGroupName, message: msgGroupName + string(name)}
		}
		return classified{kind: KindGroup, name: name, nameOff: ls + 1}
	}
	if !isKeyValue(cs[ls:]) {
		return classified{kind: KindRejected, failure: failNotKV, message: notKVMessage(g)}
	}
	if !hasGroup {
		return classified{kind: KindRejected, failure: failNoGroup, message: msgNoGroup}
	}
	gl := g[ls:]
	eq := bytes.IndexByte(cs[ls:], '=')
	ke := eq - 1
	for isSpace(gl[ke]) {
		ke--
	}
	key := gl[:ke+1]
	if !isKeyName(key) {
		return classified{kind: KindRejected, failure: failKeyName, message: msgInvalidKey + string(key)}
	}
	vs := eq + 1
	for vs < len(gl) && isSpace(gl[vs]) {
		vs++
	}
	value := gl[vs:]
	if i := bytes.IndexByte(value, 0); i >= 0 {
		value = value[:i]
	}
	return classified{
		kind: KindEntry, key: key, keyOff: ls, eqOff: ls + eq,
		value: value, valueOff: ls + vs, locale: keyLocale(key),
	}
}

// isSpace is g_ascii_isspace: space, \t, \n, \f, \r. Not \v.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// isCntrl is g_ascii_iscntrl.
func isCntrl(c byte) bool { return c < 0x20 || c == 0x7f }

// nextChar is g_utf8_find_next_char(p, NULL) on a NUL-terminated string.
func nextChar(s []byte, p int) int {
	p++
	for p < len(s) && s[p]&0xc0 == 0x80 {
		p++
	}
	return p
}

// isGroup is g_key_file_line_is_group on a C string starting at '['.
func isGroup(s []byte) bool {
	if len(s) == 0 || s[0] != '[' {
		return false
	}
	p := 1
	for p < len(s) && s[p] != ']' {
		p = nextChar(s, p)
	}
	if p >= len(s) {
		return false
	}
	p = nextChar(s, p)
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p = nextChar(s, p)
	}
	return p >= len(s)
}

// isGroupName is g_key_file_is_group_name.
func isGroupName(name []byte) bool {
	q := 0
	for q < len(name) && name[q] != ']' && name[q] != '[' && !isCntrl(name[q]) {
		q = nextChar(name, q)
	}
	return q >= len(name) && len(name) > 0
}

// isKeyValue is g_key_file_line_is_key_value_pair on a C string.
func isKeyValue(s []byte) bool {
	return bytes.IndexByte(s, '=') >= 0 && s[0] != '='
}

// isKeyName is g_key_file_is_key_name(name, len(name)).
func isKeyName(name []byte) bool {
	end := len(name)
	next := func(q int) int { // g_utf8_find_next_char(q, end), NULL mapped to end
		q++
		for q < end && name[q]&0xc0 == 0x80 {
			q++
		}
		return q
	}
	q := 0
	for q < end && name[q] != 0 && name[q] != '=' && name[q] != '[' && name[q] != ']' {
		q = next(q)
	}
	if q == 0 {
		return false
	}
	if name[0] == ' ' || name[q-1] == ' ' {
		return false
	}
	if q < end && name[q] == '[' {
		q++
		for q < end && name[q] != 0 && (isAlnumAt(name[q:end]) || strings.IndexByte("-_.@", name[q]) >= 0) {
			q = next(q)
		}
		if q >= end || name[q] != ']' {
			return false
		}
		q++
	}
	return q >= end
}

// isAlnumAt is g_unichar_isalnum(g_utf8_get_char_validated(b)).
func isAlnumAt(b []byte) bool {
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size <= 1 {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// keyLocale is GLib's key_get_locale.
func keyLocale(key []byte) string {
	i := bytes.LastIndexByte(key, '[')
	if i < 0 || len(key)-i <= 2 {
		return ""
	}
	return string(key[i+1 : len(key)-1])
}

// makeValid is g_utf8_make_valid(line, len): every invalid byte and every NUL
// becomes U+FFFD.
func makeValid(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == 0 || (r == utf8.RuneError && size == 1) {
			sb.WriteRune(utf8.RuneError)
			b = b[1:]
			continue
		}
		sb.Write(b[:size])
		b = b[size:]
	}
	return sb.String()
}

// cause assigns the single, most specific cause of a rejected line.
func cause(l source.Line, info preprocess.LineInfo, content, g []byte, res classified) Cause {
	switch {
	case l.Num == 1 && bytes.HasPrefix(content, bom):
		return CauseBOM
	case l.HasNewline && info.BracketAt < 0 && info.AppendsEqOne != info.IdealAppendsEqOne:
		if info.AppendsEqOne {
			return CauseBracketLeakBlank
		}
		return CauseBracketLeakFlag
	case len(content) > 0 && allSpace(content):
		if len(bytes.Trim(content, "\r")) == 0 {
			return CauseCarriageReturn
		}
		return CauseWhitespaceOnly
	case bytes.IndexByte(content, 0) >= 0:
		return CauseNulByte
	case !l.HasNewline && info.BracketAt < 0 && bytes.IndexByte(content, '=') < 0:
		return CauseMissingFinalNewline // it would get "= 1" if it ended with '\n'
	}
	ls := 0
	for ls < len(g) && isSpace(g[ls]) {
		ls++
	}
	switch {
	case ls < len(g) && g[ls] == '[':
		return CauseInvalidGroupLine
	case ls < len(g) && g[ls] == '=':
		return CauseEmptyKey
	case info.BracketAt >= 0 && res.failure == failNotKV:
		return CauseBracketNoValue // no '=' at all: the '[' copy skipped "= 1"
	case res.failure == failNoGroup:
		return CauseKeyBeforeGroup
	case res.failure == failKeyName:
		return CauseInvalidKeyName
	}
	return CauseUnknown
}

// allSpace reports whether b only holds GLib whitespace (no '\n' can occur).
func allSpace(b []byte) bool {
	for _, c := range b {
		if !isSpace(c) {
			return false
		}
	}
	return true
}
