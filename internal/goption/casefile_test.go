package goption

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// A gcase is one GOption case of the oracle's --goption-cases format
// (tools/oracle/README.md): an option table and an argument vector.
type gcase struct {
	name   string
	strict bool
	ctx    Context
	order  []Ref // every entry, in declaration order
	init   map[Ref]Value
	argv   []string
}

// parseCaseFile reads the cases of one file.
func parseCaseFile(path string) ([]*gcase, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseCases(string(b), path)
}

func parseCases(src, where string) ([]*gcase, error) {
	var out []*gcase
	var cur *gcase
	group := -1
	for n, line := range strings.Split(src, "\n") {
		toks, err := tokenize(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", where, n+1, err)
		}
		if len(toks) == 0 {
			continue
		}
		bad := func(format string, a ...any) error {
			return fmt.Errorf("%s:%d: %s", where, n+1, fmt.Sprintf(format, a...))
		}
		if toks[0] != "case" && cur == nil {
			return nil, bad("%s before any case", toks[0])
		}
		switch toks[0] {
		case "case":
			cur = &gcase{name: toks[1], ctx: Context{Main: &Group{Name: "main"}}, init: map[Ref]Value{}}
			group = -1
			out = append(out, cur)
		case "strict":
			cur.strict = toks[1] == "true"
			cur.ctx.IgnoreUnknown = !cur.strict
		case "group":
			cur.ctx.Groups = append(cur.ctx.Groups, &Group{Name: toks[1]})
			group = len(cur.ctx.Groups) - 1
		case "option":
			if len(toks) < 5 {
				return nil, bad("option needs LONG SHORT TYPE FLAGS")
			}
			e := Entry{Long: toks[1]}
			if toks[2] != "-" {
				e.Short = toks[2][0]
			}
			a, ok := ParseArg(toks[3])
			if !ok {
				return nil, bad("unknown type %q", toks[3])
			}
			e.Arg = a
			if toks[4] != "-" {
				for _, f := range strings.Split(toks[4], ",") {
					fl, ok := ParseFlag(f)
					if !ok {
						return nil, bad("unknown flag %q", f)
					}
					e.Flags |= fl
				}
			}
			g := cur.ctx.Main
			if group >= 0 {
				g = cur.ctx.Groups[group]
			}
			g.Entries = append(g.Entries, e)
			r := Ref{group, len(g.Entries) - 1}
			cur.order = append(cur.order, r)
			if len(toks) > 5 {
				v, err := initValue(e.Arg, toks[5])
				if err != nil {
					return nil, bad("%v", err)
				}
				cur.init[r] = v
			}
		case "arg":
			cur.argv = append(cur.argv, toks[1])
		default:
			return nil, bad("unknown directive %q", toks[0])
		}
	}
	return out, nil
}

func initValue(a Arg, tok string) (Value, error) {
	switch a {
	case ArgNone:
		return Value{Bool: tok == "true"}, nil
	case ArgInt, ArgInt64:
		n, err := strconv.ParseInt(tok, 10, 64)
		return Value{Int: n}, err
	case ArgDouble:
		d, err := strconv.ParseFloat(tok, 64)
		return Value{Double: d}, err
	case ArgString, ArgFilename:
		if tok == "null" {
			return Value{}, nil
		}
		s := tok
		return Value{Str: &s}, nil
	case ArgCallback, ArgStringArray, ArgFilenameArray:
	}
	return Value{}, fmt.Errorf("no initial value for %s", a)
}

// tokenize splits a line into bare and quoted tokens; a comment line gives
// no token.
func tokenize(line string) ([]string, error) {
	var toks []string
	i := 0
	for {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			return toks, nil
		}
		if len(toks) == 0 && line[i] == '#' {
			return nil, nil
		}
		if line[i] != '"' {
			j := i
			for j < len(line) && line[j] != ' ' && line[j] != '\t' {
				j++
			}
			toks = append(toks, line[i:j])
			i = j
			continue
		}
		var b strings.Builder
		i++
		for {
			if i >= len(line) {
				return nil, fmt.Errorf("unterminated string")
			}
			c := line[i]
			if c == '"' {
				i++
				break
			}
			if c != '\\' {
				b.WriteByte(c)
				i++
				continue
			}
			if i+1 >= len(line) {
				return nil, fmt.Errorf("unterminated escape")
			}
			switch e := line[i+1]; e {
			case '\\', '"':
				b.WriteByte(e)
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 'v':
				b.WriteByte('\v')
			case 'f':
				b.WriteByte('\f')
			case 'x':
				n, err := strconv.ParseUint(line[i+2:i+4], 16, 8)
				if err != nil {
					return nil, err
				}
				b.WriteByte(byte(n))
				i += 2
			default:
				return nil, fmt.Errorf("unknown escape \\%c", e)
			}
			i += 2
		}
		toks = append(toks, b.String())
	}
}

// quoteToken writes s as one token of the case format.
func quoteToken(s string) string {
	bare := s != ""
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= ' ' || c > '~' || c == '"' || c == '\\' || (i == 0 && c == '#') {
			bare = false
		}
	}
	if bare {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < ' ' || c == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c) // raw bytes >= 0x80 are allowed in quoted strings
		}
	}
	b.WriteByte('"')
	return b.String()
}

// format writes a case in the case format, for the oracle.
func (c *gcase) format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "case %s\nstrict %v\n", c.name, c.strict)
	writeGroup := func(g *Group, gi int) {
		for ei, e := range g.Entries {
			short := "-"
			if e.Short != 0 {
				short = string(e.Short)
			}
			var flags []string
			for name, f := range flagNames {
				if e.Flags&f != 0 {
					flags = append(flags, name)
				}
			}
			fl := "-"
			if len(flags) > 0 {
				sortStrings(flags)
				fl = strings.Join(flags, ",")
			}
			fmt.Fprintf(&b, "option %s %s %s %s", e.Long, short, e.Arg, fl)
			if v, ok := c.init[Ref{gi, ei}]; ok {
				fmt.Fprintf(&b, " %s", formatInit(e.Arg, v))
			}
			b.WriteByte('\n')
		}
	}
	writeGroup(c.ctx.Main, -1)
	for gi, g := range c.ctx.Groups {
		fmt.Fprintf(&b, "group %s\n", g.Name)
		writeGroup(g, gi)
	}
	for _, a := range c.argv {
		fmt.Fprintf(&b, "arg %s\n", quoteToken(a))
	}
	return b.String()
}

func formatInit(a Arg, v Value) string {
	switch a {
	case ArgNone:
		return strconv.FormatBool(v.Bool)
	case ArgInt, ArgInt64:
		return strconv.FormatInt(v.Int, 10)
	case ArgDouble:
		return strconv.FormatFloat(v.Double, 'g', -1, 64)
	case ArgString, ArgFilename, ArgCallback, ArgStringArray, ArgFilenameArray:
	}
	if v.Str == nil {
		return "null"
	}
	return quoteToken(*v.Str)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// renderJSON writes the result of a case exactly like the oracle's
// --goption-cases line, with its byte-string encoding.
func (c *gcase) renderJSON(r Result) string {
	var b strings.Builder
	b.WriteString(`{"name":`)
	jsonBytes(&b, c.name)
	fmt.Fprintf(&b, `,"ok":%v,"error":`, r.OK)
	if r.Error == "" {
		b.WriteString("null")
	} else {
		jsonBytes(&b, r.Error)
	}
	b.WriteString(`,"values":{`)
	first := true
	for _, ref := range c.order {
		e := c.entry(ref)
		if e.Arg == ArgCallback {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		jsonBytes(&b, e.Long)
		b.WriteByte(':')
		v := r.Values[ref]
		switch e.Arg {
		case ArgNone:
			fmt.Fprintf(&b, "%v", v.Bool)
		case ArgInt, ArgInt64:
			jsonBytes(&b, strconv.FormatInt(v.Int, 10))
		case ArgDouble:
			jsonBytes(&b, dtostr(v.Double))
		case ArgString, ArgFilename, ArgCallback, ArgStringArray, ArgFilenameArray:
			if v.Str == nil {
				b.WriteString("null")
			} else {
				jsonBytes(&b, *v.Str)
			}
		}
	}
	b.WriteString(`},"callbacks":{`)
	first = true
	for _, ref := range c.order {
		e := c.entry(ref)
		if e.Arg != ArgCallback {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		jsonBytes(&b, e.Long)
		b.WriteString(":[")
		n := 0
		for _, call := range r.Calls {
			if call.Ref != ref {
				continue
			}
			if n > 0 {
				b.WriteByte(',')
			}
			n++
			if call.Value == nil {
				b.WriteString("null")
			} else {
				jsonBytes(&b, *call.Value)
			}
		}
		b.WriteByte(']')
	}
	b.WriteString(`},"leftover":[`)
	for i, s := range r.Leftover {
		if i > 0 {
			b.WriteByte(',')
		}
		jsonBytes(&b, s)
	}
	b.WriteString("]}")
	return b.String()
}

func (c *gcase) entry(r Ref) *Entry {
	if r.Group < 0 {
		return &c.ctx.Main.Entries[r.Entry]
	}
	return &c.ctx.Groups[r.Group].Entries[r.Entry]
}

// jsonBytes is the oracle's json_bytes: byte b is the code point U+00bb;
// printable ASCII is written as is, except '"' and '\'.
func jsonBytes(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c <= 0x7e:
			b.WriteByte(c)
		default:
			b.WriteString(`\u00`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	b.WriteByte('"')
}

// dtostr is g_ascii_dtostr: "%.17g" in the C locale.
func dtostr(d float64) string {
	switch {
	case math.IsNaN(d):
		if math.Signbit(d) {
			return "-nan"
		}
		return "nan"
	case math.IsInf(d, 1):
		return "inf"
	case math.IsInf(d, -1):
		return "-inf"
	}
	return strconv.FormatFloat(d, 'g', 17, 64)
}
