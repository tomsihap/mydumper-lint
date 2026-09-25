package main

import (
	"fmt"
	"strings"
)

// This file recovers the little C structure the generator needs from the
// tokens of one file under one build configuration: function definitions
// (with their bodies and parameter names) and GOptionEntry arrays.

// cfgUnit is a unit restricted to the tokens compiled under one configuration.
type cfgUnit struct {
	u      *unit
	cfg    config
	toks   []token
	rawIdx []int  // index in u.raw of toks[i]
	unsure []bool // the condition of toks[i] cannot be decided
	funcs  []*funcDef
	arrays []*arrayDef
}

type funcDef struct {
	name    string
	static  bool
	cu      *cfgUnit
	line    int
	nameIdx int      // index of the name token in cu.toks
	params  []string // parameter names, in order ("" when unnamed)
	open    int      // index of the body's '{'
	close   int      // index of the body's '}'
}

// body returns the tokens between the braces.
func (f *funcDef) body() []token { return f.cu.toks[f.open+1 : f.close] }

func (f *funcDef) String() string { return fmt.Sprintf("%s (%s:%d)", f.name, f.cu.u.path, f.line) }

type arrayDef struct {
	name   string
	static bool
	cu     *cfgUnit
	fn     *funcDef // enclosing function; nil at file scope
	line   int
	open   int // index of the initializer's '{'
	close  int // index of the matching '}'
}

func (a *arrayDef) String() string { return fmt.Sprintf("%s (%s:%d)", a.name, a.cu.u.path, a.line) }

// uncertain reports whether any token in [from, to] has an undecidable
// condition, and returns the first such token.
func (cu *cfgUnit) uncertain(from, to int) (token, *cond, bool) {
	for i := from; i <= to && i < len(cu.toks); i++ {
		if cu.unsure[i] {
			// find the cond of this token for the message
			raw := cu.rawIdx[i]
			for k, r := range cu.u.code {
				if r == raw {
					return cu.toks[i], cu.u.conds[k], true
				}
			}
			return cu.toks[i], nil, true
		}
	}
	return token{}, nil, false
}

// filterUnit keeps the tokens whose condition is not false under cfg.
func filterUnit(u *unit, cfg config) *cfgUnit {
	cu := &cfgUnit{u: u, cfg: cfg}
	for i, r := range u.code {
		t := u.conds[i].truth(cfg)
		if t == triFalse {
			continue
		}
		cu.toks = append(cu.toks, u.raw[r])
		cu.rawIdx = append(cu.rawIdx, r)
		cu.unsure = append(cu.unsure, t == triUnknown)
	}
	return cu
}

func (cu *cfgUnit) errorf(i int, format string, args ...any) error {
	line := 0
	if i < len(cu.toks) {
		line = cu.toks[i].line
	}
	return fmt.Errorf("%s:%d: %s", cu.u.path, line, fmt.Sprintf(format, args...))
}

var closers = map[string]string{"(": ")", "[": "]", "{": "}"}

// matchClose returns the index of the bracket closing toks[i].
func matchClose(toks []token, i int) (int, bool) {
	var stack []string
	for j := i; j < len(toks); j++ {
		t := toks[j]
		if t.kind != tkPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			stack = append(stack, closers[t.text])
		case ")", "]", "}":
			if len(stack) == 0 || stack[len(stack)-1] != t.text {
				return j, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return j, true
			}
		}
	}
	return len(toks), false
}

// splitTop splits toks[from:to] at top-level commas. Each part is [start, end).
func splitTop(toks []token, from, to int) [][2]int {
	var parts [][2]int
	depth, start := 0, from
	for j := from; j < to; j++ {
		t := toks[j]
		if t.kind != tkPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ",":
			if depth == 0 {
				parts = append(parts, [2]int{start, j})
				start = j + 1
			}
		}
	}
	if start < to || len(parts) > 0 {
		parts = append(parts, [2]int{start, to})
	}
	return parts
}

// parse finds function definitions and GOptionEntry arrays.
func (cu *cfgUnit) parse() error {
	ts := cu.toks
	declStart, hasEq := 0, false
	for i := 0; i < len(ts); {
		t := ts[i]
		if t.kind != tkPunct {
			i++
			continue
		}
		switch t.text {
		case "(", "[":
			j, ok := matchClose(ts, i)
			if !ok {
				return cu.errorf(i, "unbalanced %q", t.text)
			}
			i = j + 1
			continue
		case "=":
			hasEq = true
		case "{":
			j, ok := matchClose(ts, i)
			if !ok {
				return cu.errorf(i, "unbalanced '{'")
			}
			if !hasEq && isFuncDecl(ts[declStart:i]) {
				f, err := cu.newFunc(declStart, i, j)
				if err != nil {
					return err
				}
				if f != nil {
					cu.funcs = append(cu.funcs, f)
					if err := cu.localArrays(f); err != nil {
						return err
					}
				}
				i = j + 1
				declStart, hasEq = i, false
				continue
			}
			i = j + 1 // initializer, or struct/union/enum body
			continue
		case ";":
			if err := cu.declArrays(declStart, i, nil); err != nil {
				return err
			}
			i++
			declStart, hasEq = i, false
			continue
		case ")", "]", "}":
			return cu.errorf(i, "unbalanced %q", t.text)
		}
		i++
	}
	return nil
}

// isFuncDecl reports whether a declaration followed by '{' is a function
// definition: its declarator ends with a parameter list, possibly followed by
// attribute macros written in capitals (G_GNUC_UNUSED).
func isFuncDecl(decl []token) bool {
	k := len(decl) - 1
	for k >= 0 && decl[k].kind == tkIdent && isMacroCase(decl[k].text) {
		k--
	}
	return k >= 0 && decl[k].is(")")
}

func isMacroCase(s string) bool {
	return strings.ToUpper(s) == s && strings.IndexFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0
}

// newFunc builds a function from its declaration ts[from:open] and body
// ts[open:close+1]. It returns nil for declarators it does not understand
// (such as functions returning function pointers).
func (cu *cfgUnit) newFunc(from, open, close int) (*funcDef, error) {
	ts := cu.toks
	// the name is the identifier before the first top-level '('
	nameIdx := -1
	for j := from; j < open; j++ {
		if ts[j].is("(") {
			if j > from && ts[j-1].kind == tkIdent {
				nameIdx = j - 1
			}
			break
		}
		if ts[j].is("[") {
			k, ok := matchClose(ts, j)
			if !ok {
				return nil, cu.errorf(j, "unbalanced '['")
			}
			j = k
		}
	}
	if nameIdx < 0 {
		return nil, nil
	}
	f := &funcDef{name: ts[nameIdx].text, cu: cu, line: ts[nameIdx].line, nameIdx: nameIdx, open: open, close: close}
	for j := from; j < nameIdx; j++ {
		if ts[j].isIdent("static") {
			f.static = true
		}
	}
	pclose, ok := matchClose(ts, nameIdx+1)
	if !ok {
		return nil, cu.errorf(nameIdx, "unbalanced parameter list")
	}
	for _, p := range splitTop(ts, nameIdx+2, pclose) {
		f.params = append(f.params, paramName(ts[p[0]:p[1]]))
	}
	return f, nil
}

// paramName returns the declared name of one parameter: the last identifier at
// depth 0 before any '[' or, for a function declarator, the identifier before
// its parameter list.
func paramName(p []token) string {
	name := ""
	for j := 0; j < len(p); j++ {
		t := p[j]
		switch {
		case t.kind == tkIdent && !cTypeWords[t.text]:
			name = t.text
		case t.is("(") || t.is("["):
			if t.is("(") && name == "" {
				// (*name)(…): look inside the first group
				k, _ := matchClose(p, j)
				for m := j + 1; m < k; m++ {
					if p[m].kind == tkIdent {
						name = p[m].text
					}
				}
			}
			return name
		}
	}
	return name
}

// Words that can end a type but never name a parameter.
var cTypeWords = map[string]bool{
	"void": true, "char": true, "short": true, "int": true, "long": true, "float": true, "double": true,
	"signed": true, "unsigned": true, "const": true, "volatile": true, "restrict": true, "struct": true,
	"union": true, "enum": true, "_Bool": true,
}

// declArrays records the GOptionEntry arrays defined with an initializer in
// the declaration ts[from:end].
func (cu *cfgUnit) declArrays(from, end int, fn *funcDef) error {
	ts := cu.toks
	typeIdx := -1
	for j := from; j < end; j++ {
		if ts[j].isIdent("GOptionEntry") {
			typeIdx = j
			break
		}
		if ts[j].is("(") || ts[j].is("{") || ts[j].is("=") {
			return nil
		}
	}
	if typeIdx < 0 {
		return nil
	}
	static := false
	for j := from; j < typeIdx; j++ {
		if ts[j].isIdent("static") {
			static = true
		}
	}
	for _, d := range splitTop(ts, typeIdx+1, end) {
		// [const] NAME [ … ] = { … }
		j := d[0]
		for j < d[1] && ts[j].isIdent("const") {
			j++
		}
		if j+1 >= d[1] || ts[j].kind != tkIdent || !ts[j+1].is("[") {
			continue
		}
		name := ts[j]
		k, ok := matchClose(ts, j+1)
		if !ok {
			return cu.errorf(j+1, "unbalanced '['")
		}
		if k+2 >= d[1] || !ts[k+1].is("=") || !ts[k+2].is("{") {
			continue // declaration without initializer (extern)
		}
		closeIdx, ok := matchClose(ts, k+2)
		if !ok {
			return cu.errorf(k+2, "unbalanced initializer")
		}
		cu.arrays = append(cu.arrays, &arrayDef{
			name: name.text, static: static, cu: cu, fn: fn, line: name.line, open: k + 2, close: closeIdx,
		})
	}
	return nil
}

// localArrays records GOptionEntry arrays defined inside a function body.
func (cu *cfgUnit) localArrays(f *funcDef) error {
	ts := cu.toks
	start := f.open + 1
	for j := f.open + 1; j < f.close; j++ {
		switch {
		case ts[j].is(";") || ts[j].is("{") || ts[j].is("}"):
			start = j + 1
		case ts[j].isIdent("GOptionEntry"):
			// find the end of this declaration
			end := j
			for end < f.close && !ts[end].is(";") {
				if ts[end].is("{") || ts[end].is("(") || ts[end].is("[") {
					k, ok := matchClose(ts, end)
					if !ok {
						return cu.errorf(end, "unbalanced bracket")
					}
					end = k
				}
				end++
			}
			if err := cu.declArrays(start, end, f); err != nil {
				return err
			}
			j = end
			start = end + 1
		}
	}
	return nil
}
