package optionsdb

import (
	"fmt"
	"strings"
)

// Build describes how a mydumper binary was compiled, which decides the
// conditional options (V5).
type Build struct {
	Client string // client library: "mysql" or "mariadb"
	SSL    bool   // built WITH_SSL
}

// Client libraries.
const (
	ClientMySQL   = "mysql"
	ClientMariaDB = "mariadb"
)

// DefaultBuild is the build of the official images: MySQL client, SSL on
// (design §5.5).
var DefaultBuild = Build{Client: ClientMySQL, SSL: true}

// Validate checks the client name.
func (b Build) Validate() error {
	if b.Client != ClientMySQL && b.Client != ClientMariaDB {
		return fmt.Errorf("unknown client library %q (want %q or %q)", b.Client, ClientMySQL, ClientMariaDB)
	}
	return nil
}

func (b Build) String() string {
	ssl := "ssl"
	if !b.SSL {
		ssl = "no-ssl"
	}
	return b.Client + "/" + ssl
}

// allBuilds lists every build the knowledge base distinguishes.
var allBuilds = []Build{
	{ClientMySQL, true}, {ClientMySQL, false}, {ClientMariaDB, true}, {ClientMariaDB, false},
}

// macro returns the value of a build macro for b.
//
//   - WITH_SSL is set by CMake (option WITH_SSL, on by default) ⇔ SSL.
//   - LIBMARIADB is defined by the MariaDB Connector/C headers ⇔ Client ==
//     "mariadb".
//   - HAVE_MY_BOOL is never defined: no version of mydumper's CMakeLists.txt
//     or config.h.in defines it, and the MySQL 8 client headers of the
//     official images do not either (my_bool was removed in MySQL 8.0). The
//     code only uses it to pick the type of a connection flag; no option
//     depends on it in any embedded version.
func (b Build) macro(name string) (bool, error) {
	switch name {
	case "WITH_SSL":
		return b.SSL, nil
	case "LIBMARIADB":
		return b.Client == ClientMariaDB, nil
	case "HAVE_MY_BOOL":
		return false, nil
	}
	return false, fmt.Errorf("unknown build condition %q", name)
}

// condition is a parsed build condition: || of && of possibly negated
// macros, with parentheses.
type condition interface {
	holds(b Build) bool
}

type (
	condMacro struct{ name string }
	condNot   struct{ x condition }
	condAnd   struct{ x, y condition }
	condOr    struct{ x, y condition }
	condTrue  struct{}
)

func (c condMacro) holds(b Build) bool { v, _ := b.macro(c.name); return v }
func (c condNot) holds(b Build) bool   { return !c.x.holds(b) }
func (c condAnd) holds(b Build) bool   { return c.x.holds(b) && c.y.holds(b) }
func (c condOr) holds(b Build) bool    { return c.x.holds(b) || c.y.holds(b) }
func (condTrue) holds(Build) bool      { return true }

// parseCondition parses a condition as written by the generator, such as
// "WITH_SSL && !LIBMARIADB". The empty string means "always".
func parseCondition(s string) (condition, error) {
	if strings.TrimSpace(s) == "" {
		return condTrue{}, nil
	}
	p := &condParser{src: s}
	p.lex()
	c, err := p.or()
	if err == nil && p.pos < len(p.toks) {
		err = fmt.Errorf("unexpected %q", p.toks[p.pos])
	}
	if err != nil {
		return nil, fmt.Errorf("invalid condition %q: %w", s, err)
	}
	return c, nil
}

type condParser struct {
	src  string
	toks []string
	pos  int
}

func (p *condParser) lex() {
	s := p.src
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t':
			i++
		case strings.HasPrefix(s[i:], "&&") || strings.HasPrefix(s[i:], "||"):
			p.toks = append(p.toks, s[i:i+2])
			i += 2
		case c == '!' || c == '(' || c == ')':
			p.toks = append(p.toks, s[i:i+1])
			i++
		default:
			j := i
			for j < len(s) && (s[j] == '_' || (s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z') || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j == i {
				j++ // a stray byte: keep it so the parser reports it
			}
			p.toks = append(p.toks, s[i:j])
			i = j
		}
	}
}

func (p *condParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *condParser) or() (condition, error) {
	x, err := p.and()
	for err == nil && p.peek() == "||" {
		p.pos++
		var y condition
		y, err = p.and()
		x = condOr{x, y}
	}
	return x, err
}

func (p *condParser) and() (condition, error) {
	x, err := p.unary()
	for err == nil && p.peek() == "&&" {
		p.pos++
		var y condition
		y, err = p.unary()
		x = condAnd{x, y}
	}
	return x, err
}

func (p *condParser) unary() (condition, error) {
	t := p.peek()
	switch t {
	case "":
		return nil, fmt.Errorf("unexpected end")
	case "!":
		p.pos++
		x, err := p.unary()
		return condNot{x}, err
	case "(":
		p.pos++
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("missing ')'")
		}
		p.pos++
		return x, nil
	}
	if _, err := DefaultBuild.macro(t); err != nil {
		return nil, err
	}
	p.pos++
	return condMacro{t}, nil
}
