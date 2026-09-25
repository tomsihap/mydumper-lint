package main

import (
	"fmt"
	"strconv"
	"strings"
)

// This file handles the preprocessor side of a source file: directives,
// conditional blocks and the conditions under which each token is compiled.
// Nothing is expanded. Conditions are kept as expressions and evaluated per
// build configuration with three-valued logic, so that a condition the
// generator cannot decide is noticed instead of guessed.

// Build macros the generator knows how to evaluate, in the order used when
// printing conditions. The optionsdb package evaluates the same names.
var knownMacros = []string{"WITH_SSL", "LIBMARIADB", "HAVE_MY_BOOL"}

func isKnownMacro(name string) bool {
	for _, m := range knownMacros {
		if m == name {
			return true
		}
	}
	return false
}

// tri is a three-valued truth value.
type tri uint8

const (
	triFalse tri = iota
	triTrue
	triUnknown
)

func triOf(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

// cexpr is a preprocessor condition.
type cexpr interface {
	truth(cfg config) tri
	String() string
}

type (
	eConst   struct{ v bool }
	eDefined struct{ name string } // defined(NAME)
	eMacro   struct{ name string } // NAME used as a value
	eOpaque  struct{ text string } // anything the generator does not decide
	eNot     struct{ x cexpr }
	eAnd     struct{ x, y cexpr }
	eOr      struct{ x, y cexpr }
)

func (e eConst) truth(config) tri { return triOf(e.v) }
func (e eConst) String() string {
	if e.v {
		return "1"
	}
	return "0"
}

func (e eDefined) truth(cfg config) tri {
	if isKnownMacro(e.name) {
		return triOf(cfg[e.name])
	}
	return triUnknown
}
func (e eDefined) String() string { return "defined(" + e.name + ")" }

// A known build macro is defined empty by config.h (#cmakedefine) or by the
// client library headers; used as a value it can only mean "defined".
func (e eMacro) truth(cfg config) tri {
	if isKnownMacro(e.name) {
		return triOf(cfg[e.name])
	}
	return triUnknown
}
func (e eMacro) String() string { return e.name }

func (e eOpaque) truth(config) tri  { return triUnknown }
func (e eOpaque) String() string    { return e.text }
func (e eNot) truth(cfg config) tri { return not3(e.x.truth(cfg)) }
func (e eNot) String() string       { return "!(" + e.x.String() + ")" }

func (e eAnd) truth(cfg config) tri {
	a := e.x.truth(cfg)
	if a == triFalse {
		return triFalse
	}
	b := e.y.truth(cfg)
	switch {
	case b == triFalse:
		return triFalse
	case a == triTrue && b == triTrue:
		return triTrue
	}
	return triUnknown
}
func (e eAnd) String() string { return "(" + e.x.String() + " && " + e.y.String() + ")" }

func (e eOr) truth(cfg config) tri {
	a := e.x.truth(cfg)
	if a == triTrue {
		return triTrue
	}
	b := e.y.truth(cfg)
	switch {
	case b == triTrue:
		return triTrue
	case a == triFalse && b == triFalse:
		return triFalse
	}
	return triUnknown
}
func (e eOr) String() string { return "(" + e.x.String() + " || " + e.y.String() + ")" }

func not3(t tri) tri {
	switch t {
	case triTrue:
		return triFalse
	case triFalse:
		return triTrue
	}
	return triUnknown
}

// config assigns a value to every known build macro.
type config map[string]bool

func (c config) String() string {
	parts := make([]string, 0, len(knownMacros))
	for _, m := range knownMacros {
		if c[m] {
			parts = append(parts, m)
		} else {
			parts = append(parts, "!"+m)
		}
	}
	return strings.Join(parts, " ")
}

// allConfigs returns every assignment of the known macros, in a fixed order:
// bit i of the index is the value of knownMacros[i].
func allConfigs() []config {
	n := len(knownMacros)
	out := make([]config, 0, 1<<n)
	for bits := 0; bits < 1<<n; bits++ {
		c := config{}
		for i, m := range knownMacros {
			c[m] = bits&(1<<i) != 0
		}
		out = append(out, c)
	}
	return out
}

// cond is the condition of one branch of a conditional block, chained to the
// condition of the enclosing block.
type cond struct {
	parent *cond
	expr   cexpr
	line   int // line of the directive that opened the branch
}

// truth evaluates the whole chain.
func (c *cond) truth(cfg config) tri {
	out := triTrue
	for ; c != nil; c = c.parent {
		switch c.expr.truth(cfg) {
		case triFalse:
			return triFalse
		case triUnknown:
			out = triUnknown
		}
	}
	return out
}

// describe renders the chain for error messages, innermost first.
func (c *cond) describe() string {
	var parts []string
	for ; c != nil; c = c.parent {
		parts = append(parts, fmt.Sprintf("%s (line %d)", c.expr, c.line))
	}
	return strings.Join(parts, " inside ")
}

// directive is one preprocessor line.
type directive struct {
	name string  // "if", "define", …; "" for the null directive
	args []token // tokens after the name
	line int
	pos  int // index in unit.raw of the '#' token
}

// define is an object-like macro whose body is one or more string literals.
type define struct {
	name, value string
	path        string
	line        int
	cond        *cond // condition of the #define line; nil when unconditional
}

// unit is one source file with every token classified.
type unit struct {
	path    string
	raw     []token // every token, directives included (fingerprints)
	code    []int   // indexes in raw of the tokens that are not part of a directive
	conds   []*cond // condition of raw[code[i]]
	strDefs []define
}

// newUnit lexes a file and resolves its conditional structure.
func newUnit(path string, src []byte) (*unit, error) {
	raw, err := lex(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	u := &unit{path: path, raw: raw}
	var dirs []directive
	inDir := make([]bool, len(raw))
	for i := 0; i < len(raw); i++ {
		if !raw[i].bol || !raw[i].is("#") {
			continue
		}
		d := directive{line: raw[i].line, pos: i}
		inDir[i] = true
		j := i + 1
		for ; j < len(raw) && !raw[j].bol; j++ {
			inDir[j] = true
		}
		if i+1 < j {
			d.name = raw[i+1].text
			d.args = raw[i+2 : j]
		}
		dirs = append(dirs, d)
		i = j - 1
	}

	type frame struct {
		parent  *cond
		prev    []cexpr // conditions of the earlier branches
		cur     *cond
		line    int
		sawElse bool
	}
	var stack []*frame
	current := func() *cond {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1].cur
	}
	di := 0
	for i := 0; i < len(raw); i++ {
		if !inDir[i] {
			u.code = append(u.code, i)
			u.conds = append(u.conds, current())
			continue
		}
		if di >= len(dirs) || dirs[di].pos != i {
			continue // inside a directive line already handled
		}
		d := dirs[di]
		di++
		switch d.name {
		case "if", "ifdef", "ifndef":
			e, err := directiveExpr(d)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, d.line, err)
			}
			if isIncludeGuard(d, dirs, di) {
				e = eConst{true}
			}
			f := &frame{parent: current(), prev: []cexpr{e}, line: d.line}
			f.cur = &cond{parent: f.parent, expr: e, line: d.line}
			stack = append(stack, f)
		case "elif", "else":
			if len(stack) == 0 {
				return nil, fmt.Errorf("%s:%d: #%s without #if", path, d.line, d.name)
			}
			f := stack[len(stack)-1]
			if f.sawElse {
				return nil, fmt.Errorf("%s:%d: #%s after #else", path, d.line, d.name)
			}
			var e cexpr
			for _, p := range f.prev {
				e = and(e, eNot{p})
			}
			if d.name == "elif" {
				x, err := directiveExpr(d)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: %w", path, d.line, err)
				}
				f.prev = append(f.prev, x)
				e = and(e, x)
			} else {
				f.sawElse = true
			}
			f.cur = &cond{parent: f.parent, expr: e, line: d.line}
		case "endif":
			if len(stack) == 0 {
				return nil, fmt.Errorf("%s:%d: #endif without #if", path, d.line)
			}
			stack = stack[:len(stack)-1]
		case "define":
			if def, ok := stringDefine(d); ok {
				def.path = path
				def.cond = current()
				u.strDefs = append(u.strDefs, def)
			}
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("%s:%d: unterminated conditional block", path, stack[len(stack)-1].line)
	}
	return u, nil
}

func and(a, b cexpr) cexpr {
	if a == nil {
		return b
	}
	return eAnd{a, b}
}

// directiveExpr returns the condition of #if, #ifdef, #ifndef or #elif.
func directiveExpr(d directive) (cexpr, error) {
	switch d.name {
	case "ifdef", "ifndef":
		if len(d.args) < 1 || d.args[0].kind != tkIdent {
			return nil, fmt.Errorf("#%s needs a macro name", d.name)
		}
		var e cexpr = eDefined{d.args[0].text}
		if d.name == "ifndef" {
			e = eNot{e}
		}
		return e, nil
	}
	p := &exprParser{toks: d.args}
	e, err := p.parse()
	if err != nil {
		return nil, fmt.Errorf("#%s: %w", d.name, err)
	}
	return e, nil
}

// isIncludeGuard reports whether d (at index di-1 in dirs) opens the usual
// include-guard or define-if-missing pattern: #ifndef X / #if !defined(X)
// immediately followed by #define X. Known build macros never qualify.
func isIncludeGuard(d directive, dirs []directive, di int) bool {
	var name string
	switch {
	case d.name == "ifndef" && len(d.args) == 1 && d.args[0].kind == tkIdent:
		name = d.args[0].text
	case d.name == "if":
		a := d.args
		switch {
		case len(a) == 5 && a[0].is("!") && a[1].isIdent("defined") && a[2].is("(") && a[3].kind == tkIdent && a[4].is(")"):
			name = a[3].text
		case len(a) == 3 && a[0].is("!") && a[1].isIdent("defined") && a[2].kind == tkIdent:
			name = a[2].text
		}
	}
	if name == "" || isKnownMacro(name) || di >= len(dirs) {
		return false
	}
	next := dirs[di]
	return next.name == "define" && len(next.args) >= 1 && next.args[0].isIdent(name)
}

// stringDefine recognizes #define NAME "a" "b" (an object-like macro whose
// body is only string literals).
func stringDefine(d directive) (define, bool) {
	if len(d.args) < 2 || d.args[0].kind != tkIdent {
		return define{}, false
	}
	var sb strings.Builder
	for _, t := range d.args[1:] {
		if t.kind != tkString {
			return define{}, false
		}
		s, err := unquote(t.text)
		if err != nil {
			return define{}, false
		}
		sb.WriteString(s)
	}
	return define{name: d.args[0].text, value: sb.String(), line: d.line}, true
}

// exprParser parses a #if expression (C11 6.10.1). Arithmetic, comparisons
// and function-like macros are kept as opaque atoms.
type exprParser struct {
	toks []token
	pos  int
}

func (p *exprParser) parse() (cexpr, error) {
	if len(p.toks) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	e, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unexpected %q", p.toks[p.pos].text)
	}
	return e, nil
}

func (p *exprParser) peek(text string) bool {
	return p.pos < len(p.toks) && p.toks[p.pos].is(text)
}

func (p *exprParser) text(from int) string {
	parts := make([]string, 0, p.pos-from)
	for _, t := range p.toks[from:p.pos] {
		parts = append(parts, t.text)
	}
	return strings.Join(parts, " ")
}

func (p *exprParser) ternary() (cexpr, error) {
	from := p.pos
	c, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.peek("?") {
		return c, nil
	}
	p.pos++
	if _, err := p.ternary(); err != nil {
		return nil, err
	}
	if !p.peek(":") {
		return nil, fmt.Errorf("missing ':'")
	}
	p.pos++
	if _, err := p.ternary(); err != nil {
		return nil, err
	}
	return eOpaque{p.text(from)}, nil
}

// Binary operators by precedence, loosest first.
var binLevels = [][]string{
	{"||"}, {"&&"}, {"|"}, {"^"}, {"&"}, {"==", "!="}, {"<", ">", "<=", ">="}, {"<<", ">>"}, {"+", "-"}, {"*", "/", "%"},
}

func (p *exprParser) binary(level int) (cexpr, error) {
	if level == len(binLevels) {
		return p.unary()
	}
	from := p.pos
	x, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		for _, o := range binLevels[level] {
			if p.peek(o) {
				op = o
			}
		}
		if op == "" {
			return x, nil
		}
		p.pos++
		y, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		switch op {
		case "||":
			x = eOr{x, y}
		case "&&":
			x = eAnd{x, y}
		default:
			x = eOpaque{p.text(from)}
		}
	}
}

func (p *exprParser) unary() (cexpr, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	from := p.pos
	t := p.toks[p.pos]
	switch {
	case t.is("!"):
		p.pos++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return eNot{x}, nil
	case t.is("-") || t.is("+") || t.is("~"):
		p.pos++
		if _, err := p.unary(); err != nil {
			return nil, err
		}
		return eOpaque{p.text(from)}, nil
	case t.is("("):
		p.pos++
		x, err := p.ternary()
		if err != nil {
			return nil, err
		}
		if !p.peek(")") {
			return nil, fmt.Errorf("missing ')'")
		}
		p.pos++
		return x, nil
	case t.kind == tkNumber:
		p.pos++
		s := strings.TrimRight(strings.ToLower(t.text), "ul")
		v, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return eOpaque{t.text}, nil
		}
		return eConst{v != 0}, nil
	case t.kind == tkChar:
		p.pos++
		return eOpaque{t.text}, nil
	case t.isIdent("defined"):
		p.pos++
		paren := p.peek("(")
		if paren {
			p.pos++
		}
		if p.pos >= len(p.toks) || p.toks[p.pos].kind != tkIdent {
			return nil, fmt.Errorf("defined needs a macro name")
		}
		name := p.toks[p.pos].text
		p.pos++
		if paren {
			if !p.peek(")") {
				return nil, fmt.Errorf("missing ')' after defined(%s", name)
			}
			p.pos++
		}
		return eDefined{name}, nil
	case t.kind == tkIdent:
		p.pos++
		if p.peek("(") { // function-like macro call
			depth := 0
			for ; p.pos < len(p.toks); p.pos++ {
				switch {
				case p.toks[p.pos].is("("):
					depth++
				case p.toks[p.pos].is(")"):
					depth--
				}
				if depth == 0 {
					p.pos++
					return eOpaque{p.text(from)}, nil
				}
			}
			return nil, fmt.Errorf("unbalanced parentheses")
		}
		return eMacro{t.text}, nil
	}
	return nil, fmt.Errorf("unexpected %q", t.text)
}
