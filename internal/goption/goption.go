// Package goption emulates GLib's command-line parser, GOption
// (g_option_context_parse), for the argument vectors mydumper builds from a
// configuration group: [group, --key1, value1, --key2, value2, …] (design
// §3.4).
//
// It is a behavioral re-implementation of glib/goption.c, identical from GLib
// 2.68 to 2.88, and it is checked against real GLib through the oracle
// (testdata/goption-cases, and differential tests when MYDUMPER_LINT_ORACLE
// is set). Besides the outcome GLib reports, Parse records what happened to
// every element of the vector, so that a linter can explain which key had
// which effect.
//
// Numbers are parsed like strtol and strtoll on an LP64 platform (the
// official mydumper images are x86-64 Linux builds).
package goption

import (
	"strings"
	"unicode/utf8"
)

// Arg is the type of an option's argument (GOptionArg).
type Arg uint8

// Argument types.
const (
	ArgNone Arg = iota
	ArgString
	ArgInt
	ArgCallback
	ArgFilename
	ArgStringArray
	ArgFilenameArray
	ArgDouble
	ArgInt64
)

var argNames = [...]string{
	ArgNone: "none", ArgString: "string", ArgInt: "int", ArgCallback: "callback", ArgFilename: "filename",
	ArgStringArray: "string_array", ArgFilenameArray: "filename_array", ArgDouble: "double", ArgInt64: "int64",
}

func (a Arg) String() string {
	if int(a) < len(argNames) {
		return argNames[a]
	}
	return "Arg(?)"
}

// ParseArg maps an argument type name, as the knowledge base writes it, to
// an Arg.
func ParseArg(s string) (Arg, bool) {
	for a, name := range argNames {
		if name == s {
			return Arg(a), true
		}
	}
	return 0, false
}

// Flags is a set of GOptionFlags.
type Flags uint16

// Option flags. Hidden, InMain and Deprecated only matter for --help.
const (
	FlagHidden Flags = 1 << iota
	FlagInMain
	FlagReverse
	FlagNoArg
	FlagFilename
	FlagOptionalArg
	FlagNoAlias
	FlagDeprecated
)

var flagNames = map[string]Flags{
	"hidden": FlagHidden, "in_main": FlagInMain, "reverse": FlagReverse, "no_arg": FlagNoArg,
	"filename": FlagFilename, "optional_arg": FlagOptionalArg, "noalias": FlagNoAlias, "deprecated": FlagDeprecated,
}

// ParseFlag maps a flag name, as the knowledge base writes it
// ("optional_arg"), to its bit.
func ParseFlag(s string) (Flags, bool) {
	f, ok := flagNames[s]
	return f, ok
}

// Entry is one option (a GOptionEntry).
type Entry struct {
	Long  string // long name, without dashes
	Short byte   // short name, or 0
	Arg   Arg
	Flags Flags
}

// NoArg reports whether the option never takes a value (GLib's NO_ARG): a
// flag, or a callback flagged no_arg.
func (e *Entry) NoArg() bool {
	return e.Arg == ArgNone || (e.Arg == ArgCallback && e.Flags&FlagNoArg != 0)
}

// OptionalArg reports whether the option's value is optional (GLib's
// OPTIONAL_ARG): only callbacks can have one.
func (e *Entry) OptionalArg() bool {
	return e.Arg == ArgCallback && e.Flags&FlagOptionalArg != 0
}

// Group is an option group (a GOptionGroup).
type Group struct {
	Name    string
	Entries []Entry
}

// Charset is the character set of the process locale. GOption converts the
// values of string options and of callbacks without the filename flag from
// it to UTF-8 (g_locale_to_utf8); mydumper calls setlocale(LC_ALL, "").
type Charset uint8

// Charsets.
const (
	// CharsetASCII is the C locale (no LANG, LC_ALL or LC_CTYPE): a value
	// with a byte ≥ 0x80 is an error.
	CharsetASCII Charset = iota
	// CharsetUTF8 is a UTF-8 locale: a value that is not valid UTF-8 is an
	// error.
	CharsetUTF8
	// CharsetOther is an 8-bit locale such as ISO-8859-1: every value
	// converts.
	CharsetOther
)

// Context is an option context (a GOptionContext) with GLib's built-in help
// disabled, as mydumper and myloader set it up.
type Context struct {
	Main          *Group   // the main group (g_option_context_set_main_group); may be nil
	Groups        []*Group // the other groups, in g_option_context_add_group order
	IgnoreUnknown bool     // g_option_context_set_ignore_unknown_options(context, TRUE)
	Charset       Charset
}

// Ref identifies an entry of a context: Group is -1 for the main group, else
// an index in Context.Groups.
type Ref struct {
	Group, Entry int
}

// Use tells what parsing did with one element of the vector.
type Use uint8

// Uses.
const (
	UseNotReached     Use = iota // argv[0], or parsing failed before this element
	UseOption                    // names one or more options that were applied
	UseValue                     // consumed as the value of an option named by another element
	UseUnknown                   // names an unknown option (ignored when IgnoreUnknown)
	UseLeftover                  // not an option: left in the vector
	UseSeparator                 // "--": ends option parsing
	UseAfterSeparator            // after "--": left in the vector without being parsed
)

var useNames = [...]string{
	UseNotReached: "not-reached", UseOption: "option", UseValue: "value", UseUnknown: "unknown",
	UseLeftover: "leftover", UseSeparator: "separator", UseAfterSeparator: "after-separator",
}

func (u Use) String() string {
	if int(u) < len(useNames) {
		return useNames[u]
	}
	return "Use(?)"
}

// Applied is one option GLib handed to its argument parser (parse_arg), in
// order, whether that succeeded or not.
type Applied struct {
	Ref     Ref
	Name    string  // as GLib names it in messages: "--threads" or "-t"
	Element int     // index of the element naming the option
	ValueAt int     // index of the element holding the value, -1 when inline or none
	Value   *string // the value parse_arg received; nil for none
	Alias   bool    // reached as --<group prefix>-<name>
}

// Value is the variable of a non-callback option (its arg_data).
type Value struct {
	Bool   bool     // ArgNone
	Int    int64    // ArgInt, ArgInt64
	Double float64  // ArgDouble
	Str    *string  // ArgString, ArgFilename; nil is NULL
	Strs   []string // ArgStringArray, ArgFilenameArray; nil is NULL
}

// Result is the outcome of Parse.
type Result struct {
	OK bool
	// Error is GLib's message when parsing failed; mydumper then aborts with
	// "option parsing failed: <Error>, try --help". It can also be set when
	// OK: GLib leaks the error of an optional callback value that fails to
	// convert (the callback then receives no value).
	Error string
	// ErrorAt is the index of the element that names the option Error is
	// about (or the unknown option), -1 when Error is empty.
	ErrorAt int
	// Uses has one entry per element of the vector.
	Uses []Use
	// Applied lists the options handed to parse_arg, in order.
	Applied []Applied
	// Values holds the variables after parsing. On failure GLib reverts
	// what this parse changed, its own way: strings and arrays get their
	// value back, numbers the value before their last assignment, and flags
	// become false. Callback calls are not undone.
	Values map[Ref]Value
	// Calls lists the callback invocations, in order.
	Calls []Applied
	// Leftover is the vector after parsing, element 0 included; unchanged
	// on failure.
	Leftover []string
}

// Parse parses argv (argv[0] is skipped, like a program name) and returns
// what GLib would do. init holds the variables before parsing; missing
// entries start at their zero value.
func (c *Context) Parse(argv []string, init map[Ref]Value) Result {
	p := &parser{c: c, argv: argv, uses: make([]Use, len(argv)), values: map[Ref]Value{}}
	for r, v := range init {
		p.values[r] = v
	}
	ok := p.run()
	res := Result{OK: ok, Error: p.err, ErrorAt: -1, Uses: p.uses, Applied: p.applied, Calls: p.calls}
	if p.err != "" {
		res.ErrorAt = p.errAt
	}
	if !ok {
		res.Values = p.values
		for r, prev := range p.prev {
			res.Values[r] = prev
		}
		res.Leftover = append([]string(nil), argv...)
		return res
	}
	res.Values = p.values
	for i, s := range argv {
		if i == 0 {
			res.Leftover = append(res.Leftover, s)
			continue
		}
		if r, rewritten := p.rewrites[i]; rewritten {
			res.Leftover = append(res.Leftover, "-"+r)
			continue
		}
		if !p.nulled[i] {
			res.Leftover = append(res.Leftover, s)
		}
	}
	return res
}

type parser struct {
	c        *Context
	argv     []string
	uses     []Use
	applied  []Applied
	calls    []Applied
	values   map[Ref]Value
	nulled   map[int]bool   // GLib's pending nulls: removed from the vector on success
	rewrites map[int]string // short-option clusters reduced to their unknown letters
	arrays   map[Ref]bool   // array options already appended to during this parse
	prev     map[Ref]Value  // GLib's change list: what a failure restores
	err      string         // the first error set (GLib never overwrites a GError)
	errAt    int
	failed   bool
}

// setError records an error the way g_set_error does: the first one stays.
func (p *parser) setError(at int, msg string) {
	if p.err == "" {
		p.err, p.errAt = msg, at
	}
}

// fail ends parsing: parse_arg or an unknown option returned FALSE.
func (p *parser) fail() bool {
	p.failed = true
	return false
}

// group returns the group of a reference.
func (p *parser) group(g int) *Group {
	if g < 0 {
		return p.c.Main
	}
	return p.c.Groups[g]
}

func (p *parser) null(i int) {
	if p.nulled == nil {
		p.nulled = map[int]bool{}
	}
	p.nulled[i] = true
}

// run is g_option_context_parse's main loop. It returns false on failure.
func (p *parser) run() bool {
	argv := p.argv
	stop, hasUnknown, separator := false, false, 0
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		parsed := false
		if len(a) >= 2 && a[0] == '-' && !stop {
			if a[1] == '-' {
				name := a[2:]
				if name == "" { // "--" ends option parsing
					separator, stop = i, true
					p.uses[i] = UseSeparator
					continue
				}
				start := i
				if p.c.Main != nil {
					if !p.long(-1, &i, name, false, &parsed) {
						return p.fail()
					}
				}
				if parsed {
					p.uses[start] = UseOption
					continue
				}
				for g := range p.c.Groups {
					if !p.long(g, &i, name, false, &parsed) {
						return p.fail()
					}
					if parsed {
						break
					}
				}
				if parsed {
					p.uses[start] = UseOption
					continue
				}
				// --<group>-<option>: the part before the first dash may be any
				// prefix of a group name; the main group has no such alias.
				if dash := strings.IndexByte(name, '-'); dash > 0 {
					for g, grp := range p.c.Groups {
						if !strings.HasPrefix(grp.Name, name[:dash]) {
							continue
						}
						if !p.long(g, &i, name[dash+1:], true, &parsed) {
							return p.fail()
						}
						if parsed {
							break
						}
					}
				}
				if parsed {
					p.uses[start] = UseOption
				} else {
					p.uses[start] = UseUnknown
				}
				if p.c.IgnoreUnknown {
					continue // an unknown long option does not count as unknown for "--"
				}
			} else if !p.short(&i, &parsed, &hasUnknown) {
				return p.fail()
			}
			if !parsed && !p.c.IgnoreUnknown {
				p.uses[i] = UseUnknown
				p.setError(i, "Unknown option "+a)
				return p.fail()
			}
			continue
		}
		// Not an option (mydumper declares no G_OPTION_REMAINING entry).
		if stop {
			p.uses[i] = UseAfterSeparator
		} else {
			p.uses[i] = UseLeftover
		}
		if hasUnknown || a != "" && a[0] == '-' {
			separator = 0
		}
	}
	if separator > 0 {
		p.null(separator)
	}
	return true
}

// short parses a cluster of short options, "-abc". It returns false on
// failure; parsed is set from the last letter handled, like GLib.
func (p *parser) short(i *int, parsed, hasUnknown *bool) bool {
	idx := *i
	letters := p.argv[idx][1:]
	newIdx := idx
	known := make([]bool, len(letters))
	anyKnown := false
	for j := 0; j < len(letters); j++ {
		*parsed = false
		if p.c.Main != nil && !p.shortIn(-1, idx, &newIdx, letters[j], parsed) {
			return false
		}
		if !*parsed {
			for g := range p.c.Groups {
				if !p.shortIn(g, idx, &newIdx, letters[j], parsed) {
					return false
				}
				if *parsed {
					break
				}
			}
		}
		switch {
		case p.c.IgnoreUnknown && *parsed:
			known[j], anyKnown = true, true
		case p.c.IgnoreUnknown:
			continue
		case !*parsed:
			j = len(letters) // GLib breaks out of the loop
		default:
			anyKnown = true
		}
	}
	if !*parsed {
		*hasUnknown = true
	}
	if p.c.IgnoreUnknown {
		var rest []byte
		for j := 0; j < len(letters); j++ {
			if !known[j] {
				rest = append(rest, letters[j])
			}
		}
		if rest != nil {
			if p.rewrites == nil {
				p.rewrites = map[int]string{}
			}
			p.rewrites[idx] = string(rest)
		} else {
			p.null(idx)
		}
		if anyKnown {
			p.uses[idx] = UseOption
		} else {
			p.uses[idx] = UseUnknown
		}
		*i = newIdx
		return true
	}
	if *parsed {
		p.null(idx)
		p.uses[idx] = UseOption
		*i = newIdx
	}
	return true
}

// shortIn is parse_short_option for one group. Every entry with that short
// name is applied, like GLib.
func (p *parser) shortIn(g, idx int, newIdx *int, letter byte, parsed *bool) bool {
	grp := p.group(g)
	for e := range grp.Entries {
		entry := &grp.Entries[e]
		if entry.Short == 0 || entry.Short != letter {
			continue
		}
		name := "-" + string(letter)
		a := Applied{Ref: Ref{g, e}, Name: name, Element: idx, ValueAt: -1}
		p.uses[idx] = UseOption
		if !entry.NoArg() {
			if *newIdx > idx {
				p.setError(idx, "Error parsing option "+name)
				return false
			}
			switch {
			case idx < len(p.argv)-1:
				next := p.argv[idx+1]
				if !entry.OptionalArg() || next == "" || next[0] != '-' {
					v := next
					a.Value, a.ValueAt = &v, idx+1
					p.null(idx + 1)
					p.uses[idx+1] = UseValue
					*newIdx = idx + 1
				}
			case entry.OptionalArg():
			default:
				p.setError(idx, "Missing argument for "+name)
				return false
			}
		}
		if !p.apply(a) {
			return false
		}
		*parsed = true
	}
	return true
}

// long is parse_long_option for one group.
func (p *parser) long(g int, i *int, name string, aliased bool, parsed *bool) bool {
	grp := p.group(g)
	start := *i
	for e := range grp.Entries {
		if *i >= len(p.argv) {
			return true
		}
		entry := &grp.Entries[e]
		if aliased && entry.Flags&FlagNoAlias != 0 {
			continue
		}
		opt := "--" + entry.Long
		a := Applied{Ref: Ref{g, e}, Name: opt, Element: start, ValueAt: -1, Alias: aliased}
		if entry.NoArg() && name == entry.Long {
			p.uses[start] = UseOption
			ok := p.apply(a)
			p.null(start)
			*parsed = true
			return ok
		}
		n := len(entry.Long)
		if !strings.HasPrefix(name, entry.Long) || (len(name) != n && name[n] != '=') {
			continue
		}
		p.null(*i) // *i moved on if an earlier entry of the group took a value
		p.uses[start] = UseOption
		switch {
		case len(name) > n: // --name=value
			v := name[n+1:]
			a.Value = &v
		case *i < len(p.argv)-1:
			next := p.argv[*i+1]
			if entry.OptionalArg() && next != "" && next[0] == '-' {
				ok := p.apply(a)
				*parsed = true
				return ok
			}
			v := next
			a.Value, a.ValueAt = &v, *i+1
			p.null(*i + 1)
			p.uses[*i+1] = UseValue
			*i++
		case entry.OptionalArg():
			ok := p.apply(a)
			*parsed = true
			return ok
		default:
			p.setError(start, "Missing argument for "+opt)
			return false
		}
		if !p.apply(a) {
			return false
		}
		*parsed = true
	}
	return true
}

// apply is parse_arg: it converts the value and stores it.
func (p *parser) apply(a Applied) bool {
	p.applied = append(p.applied, a)
	entry := &p.group(a.Ref.Group).Entries[a.Ref.Entry]
	v := p.values[a.Ref]
	before := v
	value := ""
	if a.Value != nil {
		value = *a.Value
	}
	switch entry.Arg {
	case ArgNone:
		v.Bool = entry.Flags&FlagReverse == 0
	case ArgString:
		if !p.convert(a.Element, value) {
			return false
		}
		s := value
		v.Str = &s
	case ArgStringArray:
		if !p.convert(a.Element, value) {
			return false
		}
		v.Strs = p.appendArray(a.Ref, v.Strs, value)
	case ArgFilename:
		s := value
		v.Str = &s
	case ArgFilenameArray:
		v.Strs = p.appendArray(a.Ref, v.Strs, value)
	case ArgInt:
		n, msg := parseInt(value, a.Name, 32)
		if msg != "" {
			p.setError(a.Element, msg)
			return false
		}
		v.Int = n
	case ArgInt64:
		n, msg := parseInt(value, a.Name, 64)
		if msg != "" {
			p.setError(a.Element, msg)
			return false
		}
		v.Int = n
	case ArgDouble:
		d, msg := parseDouble(value, a.Name)
		if msg != "" {
			p.setError(a.Element, msg)
			return false
		}
		v.Double = d
	case ArgCallback:
		return p.callback(a, entry, value)
	}
	p.record(a.Ref, entry.Arg, before)
	p.values[a.Ref] = v
	return true
}

// record mirrors GLib's change list, which says what a failed parse
// restores: a flag's previous value is never saved (it reverts to false), a
// number's is saved at each assignment, a string's or array's only at the
// first.
func (p *parser) record(r Ref, a Arg, before Value) {
	if p.prev == nil {
		p.prev = map[Ref]Value{}
	}
	_, seen := p.prev[r]
	switch a {
	case ArgNone:
		p.prev[r] = Value{}
	case ArgInt, ArgInt64, ArgDouble:
		p.prev[r] = before
	case ArgString, ArgFilename, ArgStringArray, ArgFilenameArray:
		if !seen {
			p.prev[r] = before
		}
	case ArgCallback:
	}
}

// appendArray adds a value to an array option. The first value of a parse
// replaces the array the variable held before: GLib builds a new array.
func (p *parser) appendArray(r Ref, cur []string, value string) []string {
	if p.arrays == nil {
		p.arrays = map[Ref]bool{}
	}
	if !p.arrays[r] {
		p.arrays[r] = true
		cur = nil
	}
	return append(append([]string{}, cur...), value)
}

// callback is parse_arg for callbacks. The callback itself always accepts
// its value: validating it is the linter's job, not the emulator's.
func (p *parser) callback(a Applied, entry *Entry, value string) bool {
	data := a.Value
	switch {
	case a.Value == nil && entry.Flags&FlagOptionalArg != 0, entry.Flags&FlagNoArg != 0:
		data = nil
	case entry.Flags&FlagFilename != 0:
	default:
		if !p.convert(a.Element, value) {
			// GLib only gives up when the value is required; an optional
			// value that fails to convert reaches the callback as NULL.
			if entry.Flags&(FlagNoArg|FlagOptionalArg) == 0 {
				return false
			}
			data = nil
		}
	}
	a.Value = data
	p.calls = append(p.calls, a)
	return true
}

// convert is g_locale_to_utf8's verdict on a value.
func (p *parser) convert(at int, value string) bool {
	ok := true
	switch p.c.Charset {
	case CharsetASCII:
		for i := 0; i < len(value); i++ {
			if value[i] >= 0x80 {
				ok = false
				break
			}
		}
	case CharsetUTF8:
		ok = utf8.ValidString(value)
	case CharsetOther:
	}
	if !ok {
		p.setError(at, "Invalid byte sequence in conversion input")
	}
	return ok
}
