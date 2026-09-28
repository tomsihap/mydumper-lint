package goption

import (
	"strings"
	"unicode/utf8"
)

// Entry returns the entry a reference names.
func (c *Context) Entry(r Ref) *Entry {
	if r.Group < 0 {
		return &c.Main.Entries[r.Entry]
	}
	return &c.Groups[r.Group].Entries[r.Entry]
}

// Lookup finds the option that "--name" names, the way Parse does, without
// parsing anything: the main group, then the other groups, then
// --<group prefix>-<option> (alias is then true).
func (c *Context) Lookup(name string) (r Ref, alias, ok bool) {
	if c.Main != nil {
		if e, ok := c.Main.match(name, false); ok {
			return Ref{-1, e}, false, true
		}
	}
	for g, grp := range c.Groups {
		if e, ok := grp.match(name, false); ok {
			return Ref{g, e}, false, true
		}
	}
	if dash := strings.IndexByte(name, '-'); dash > 0 {
		for g, grp := range c.Groups {
			if !strings.HasPrefix(grp.Name, name[:dash]) {
				continue
			}
			if e, ok := grp.match(name[dash+1:], true); ok {
				return Ref{g, e}, true, true
			}
		}
	}
	return Ref{}, false, false
}

// match is parse_long_option's test: the long name, optionally followed by
// "=value".
func (g *Group) match(name string, aliased bool) (int, bool) {
	for e := range g.Entries {
		entry := &g.Entries[e]
		if entry.Long == "" || (aliased && entry.Flags&FlagNoAlias != 0) {
			continue
		}
		n := len(entry.Long)
		if strings.HasPrefix(name, entry.Long) && (len(name) == n || name[n] == '=') {
			return e, true
		}
	}
	return 0, false
}

// Check returns the error GLib reports when the option r, named name in
// messages ("--threads"), receives value; "" when parse_arg accepts it.
// Callbacks accept every value that converts: validating it is the
// callback's job. A callback with an optional value never fails here; see
// Converted.
func (c *Context) Check(r Ref, name, value string) string {
	e := c.Entry(r)
	switch e.Arg {
	case ArgNone, ArgFilename, ArgFilenameArray:
	case ArgString, ArgStringArray:
		if !Converts(value, c.Charset) {
			return errConversion
		}
	case ArgInt:
		_, msg := parseInt(value, name, 32)
		return msg
	case ArgInt64:
		_, msg := parseInt(value, name, 64)
		return msg
	case ArgDouble:
		_, msg := parseDouble(value, name)
		return msg
	case ArgCallback:
		if e.Converted() && !Converts(value, c.Charset) && e.Flags&FlagOptionalArg == 0 {
			return errConversion
		}
	}
	return ""
}

// Converted reports whether GLib converts the option's values from the
// locale's charset (g_locale_to_utf8): strings, string arrays, and
// callbacks that take a value and are not flagged filename.
func (e *Entry) Converted() bool {
	switch e.Arg {
	case ArgString, ArgStringArray:
		return true
	case ArgCallback:
		return e.Flags&(FlagNoArg|FlagFilename) == 0
	case ArgNone, ArgInt, ArgFilename, ArgFilenameArray, ArgDouble, ArgInt64:
	}
	return false
}

// Converts reports whether g_locale_to_utf8 accepts value in charset cs.
func Converts(value string, cs Charset) bool {
	switch cs {
	case CharsetASCII:
		for i := 0; i < len(value); i++ {
			if value[i] >= 0x80 {
				return false
			}
		}
	case CharsetUTF8:
		return utf8.ValidString(value)
	case CharsetOther:
	}
	return true
}

const errConversion = "Invalid byte sequence in conversion input"
