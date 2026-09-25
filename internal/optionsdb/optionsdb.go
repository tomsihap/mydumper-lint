package optionsdb

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

//go:embed data/optionsdb.json
var embedded []byte

var (
	loadOnce sync.Once
	loadedDB *DB
	errLoad  error
)

// Load returns the embedded knowledge base. It is parsed and validated once;
// later calls return the same *DB, which must be treated as read-only.
func Load() (*DB, error) {
	loadOnce.Do(func() { loadedDB, errLoad = Parse(embedded) })
	return loadedDB, errLoad
}

// Tools are the programs the knowledge base describes.
var Tools = []string{"mydumper", "myloader"}

// Argument types and flags the schema allows (GLib's GOptionArg and
// GOptionFlags, lowercased).
var (
	argTypes = []string{"none", "string", "int", "callback", "filename", "string_array", "filename_array", "double", "int64"}
	flagSet  = []string{"deprecated", "filename", "hidden", "in_main", "no_arg", "noalias", "optional_arg", "reverse"}
)

var (
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	dateRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// index holds what Parse precomputes.
type index struct {
	tags    []Tag          // parsed Versions[i].Tag
	pos     map[string]int // tag → index in Versions
	options map[string]int // tool + "\x00" + name → index in Options
	conds   map[string]condition
}

// Parse decodes and validates a knowledge base. Unknown JSON fields are
// errors, so a newer generator cannot silently lose information.
func Parse(data []byte) (*DB, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	db := &DB{}
	if err := dec.Decode(db); err != nil {
		return nil, fmt.Errorf("optionsdb: %w", err)
	}
	if dec.More() {
		return nil, errors.New("optionsdb: trailing data after the JSON document")
	}
	if err := db.build(); err != nil {
		return nil, fmt.Errorf("optionsdb: %w", err)
	}
	return db, nil
}

// build validates the content and fills the index.
func (db *DB) build() error {
	if db.Schema != SchemaVersion {
		return fmt.Errorf("schema %d, want %d", db.Schema, SchemaVersion)
	}
	if len(db.Versions) == 0 {
		return errors.New("no version")
	}
	ix := &index{pos: map[string]int{}, options: map[string]int{}, conds: map[string]condition{}}
	for i, v := range db.Versions {
		t, err := ParseTag(v.Tag)
		if err != nil {
			return err
		}
		if t.String() != v.Tag {
			return fmt.Errorf("version %q is not in canonical form %s", v.Tag, t)
		}
		if i > 0 && !ix.tags[i-1].Less(t) {
			return fmt.Errorf("versions not in ascending order at %s", v.Tag)
		}
		if !commitRE.MatchString(v.Commit) {
			return fmt.Errorf("%s: invalid commit %q", v.Tag, v.Commit)
		}
		if v.Date != "" && !dateRE.MatchString(v.Date) {
			return fmt.Errorf("%s: invalid date %q", v.Tag, v.Date)
		}
		if !sha256RE.MatchString(v.LoaderFingerprint) {
			return fmt.Errorf("%s: invalid loader fingerprint %q", v.Tag, v.LoaderFingerprint)
		}
		ix.tags = append(ix.tags, t)
		ix.pos[v.Tag] = i
	}
	db.idx = ix

	for i, o := range db.Options {
		if !slices.Contains(Tools, o.Tool) {
			return fmt.Errorf("option %q: unknown tool %q", o.Name, o.Tool)
		}
		if o.Name == "" || strings.HasPrefix(o.Name, "-") {
			return fmt.Errorf("%s: invalid option name %q", o.Tool, o.Name)
		}
		key := o.Tool + "\x00" + o.Name
		if i > 0 {
			prev := db.Options[i-1]
			if prev.Tool+"\x00"+prev.Name >= key {
				return fmt.Errorf("options not sorted by (tool, name) at %s %s", o.Tool, o.Name)
			}
		}
		ix.options[key] = i
		if len(o.Spans) == 0 {
			return fmt.Errorf("%s %s: no span", o.Tool, o.Name)
		}
		for _, s := range o.Spans {
			if err := db.checkSpan(o, s); err != nil {
				return err
			}
		}
		// at most one span per version and build
		for vi := range db.Versions {
			for _, b := range allBuilds {
				n := 0
				for _, s := range o.Spans {
					if db.inRange(s.Range, vi) && ix.conds[s.Condition].holds(b) {
						n++
					}
				}
				if n > 1 {
					return fmt.Errorf("%s %s: %d spans apply to %s with build %s", o.Tool, o.Name, n, db.Versions[vi].Tag, b)
				}
			}
		}
	}
	for _, facts := range []struct {
		what string
		list []Fact
	}{{"table key", db.TableKeys}, {"masquerade function", db.MasqueradeFunctions}, {"product", db.Products}} {
		for i, f := range facts.list {
			if f.Name == "" {
				return fmt.Errorf("empty %s name", facts.what)
			}
			if i > 0 {
				p := facts.list[i-1]
				if p.Name > f.Name || (p.Name == f.Name && (p.Prefix || !f.Prefix)) {
					return fmt.Errorf("%ss not sorted by (name, prefix) at %q", facts.what, f.Name)
				}
			}
			if len(f.Spans) == 0 {
				return fmt.Errorf("%s %q: no span", facts.what, f.Name)
			}
			for j, r := range f.Spans {
				if err := db.checkRange(r); err != nil {
					return fmt.Errorf("%s %q: %w", facts.what, f.Name, err)
				}
				if j > 0 && db.rangeEnd(f.Spans[j-1]) >= db.idx.pos[r.From] {
					return fmt.Errorf("%s %q: spans overlap or are out of order", facts.what, f.Name)
				}
			}
		}
	}
	return nil
}

func (db *DB) checkRange(r Range) error {
	from, ok := db.idx.pos[r.From]
	if !ok {
		return fmt.Errorf("span starts at unknown version %q", r.From)
	}
	if r.To != "" {
		to, ok := db.idx.pos[r.To]
		if !ok {
			return fmt.Errorf("span ends at unknown version %q", r.To)
		}
		if to < from {
			return fmt.Errorf("span %s..%s is reversed", r.From, r.To)
		}
		if to == len(db.Versions)-1 {
			return fmt.Errorf("span %s..%s must leave to empty: it reaches the newest version", r.From, r.To)
		}
	}
	return nil
}

func (db *DB) checkSpan(o Option, s OptionSpan) error {
	where := fmt.Sprintf("%s %s span %s", o.Tool, o.Name, s.From)
	if err := db.checkRange(s.Range); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if len(s.Short) > 1 {
		return fmt.Errorf("%s: short name %q is not one character", where, s.Short)
	}
	if !slices.Contains(argTypes, s.Arg) {
		return fmt.Errorf("%s: unknown argument type %q", where, s.Arg)
	}
	for i, f := range s.Flags {
		if !slices.Contains(flagSet, f) {
			return fmt.Errorf("%s: unknown flag %q", where, f)
		}
		if i > 0 && s.Flags[i-1] >= f {
			return fmt.Errorf("%s: flags not sorted", where)
		}
	}
	if s.Description != "" {
		return fmt.Errorf("%s: descriptions must stay empty (upstream help texts are GPL)", where)
	}
	if len(s.Values) == 0 && s.ValuesIgnoreCase {
		return fmt.Errorf("%s: values_ignore_case without values", where)
	}
	if _, ok := db.idx.conds[s.Condition]; !ok {
		c, err := parseCondition(s.Condition)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		db.idx.conds[s.Condition] = c
	}
	return nil
}

// inRange reports whether the version at index vi is in r.
func (db *DB) inRange(r Range, vi int) bool {
	return db.idx.pos[r.From] <= vi && vi <= db.rangeEnd(r)
}

// rangeEnd returns the index of the last version in r.
func (db *DB) rangeEnd(r Range) int {
	if r.To == "" {
		return len(db.Versions) - 1
	}
	return db.idx.pos[r.To]
}

// Lookup returns the embedded version with this exact tag.
func (db *DB) Lookup(tag string) (*Version, bool) {
	i, ok := db.idx.pos[tag]
	if !ok {
		return nil, false
	}
	return &db.Versions[i], true
}

// Oldest and Newest return the first and last embedded versions.
func (db *DB) Oldest() *Version { return &db.Versions[0] }

// Newest returns the newest embedded version, pre-release or not.
func (db *DB) Newest() *Version { return &db.Versions[len(db.Versions)-1] }

// History returns every span of an option across versions and builds, in
// version order: the basis of messages such as "added in v0.20.1-1". It
// returns nil for an option that never existed.
func (db *DB) History(tool, name string) []OptionSpan {
	i, ok := db.idx.options[tool+"\x00"+name]
	if !ok {
		return nil
	}
	out := make([]OptionSpan, 0, len(db.Options[i].Spans))
	for _, s := range db.Options[i].Spans {
		out = append(out, cloneSpan(s))
	}
	return out
}

func cloneSpan(s OptionSpan) OptionSpan {
	s.Flags = slices.Clone(s.Flags)
	s.Values = slices.Clone(s.Values)
	return s
}
