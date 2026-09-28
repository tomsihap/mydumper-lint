package optionsdb

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// View is the knowledge base seen from one version and one build: the facts
// a linter needs for its target. A View is immutable and safe for concurrent
// use.
type View struct {
	db       *DB
	version  Version
	build    Build
	options  map[string]map[string]*OptionSpan // tool → name → span
	shorts   map[string]map[string]string      // tool → short name → long name
	ignore   bool
	keys     []Fact // table keys of the version
	masq     []string
	products []string
}

// View returns the facts of an embedded version (an exact tag, as returned
// by Resolve) for a build.
func (db *DB) View(tag string, b Build) (*View, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	vi, ok := db.idx.pos[tag]
	if !ok {
		return nil, fmt.Errorf("mydumper %s is not an embedded version (resolve it first)", tag)
	}
	v := &View{
		db:      db,
		version: db.Versions[vi],
		build:   b,
		options: map[string]map[string]*OptionSpan{},
		shorts:  map[string]map[string]string{},
		ignore:  db.Versions[vi].IgnoreUnknownOptions,
	}
	for _, tool := range Tools {
		v.options[tool] = map[string]*OptionSpan{}
		v.shorts[tool] = map[string]string{}
	}
	for oi := range db.Options {
		o := &db.Options[oi]
		for si := range o.Spans {
			s := &o.Spans[si]
			if db.inRange(s.Range, vi) && db.idx.conds[s.Condition].holds(b) {
				v.options[o.Tool][o.Name] = s
				if s.Short != "" {
					v.shorts[o.Tool][s.Short] = o.Name
				}
			}
		}
	}
	for _, f := range db.TableKeys {
		if db.factHolds(f, vi) {
			v.keys = append(v.keys, f)
		}
	}
	for _, f := range db.MasqueradeFunctions {
		if db.factHolds(f, vi) {
			v.masq = append(v.masq, f.Name)
		}
	}
	for _, f := range db.Products {
		if db.factHolds(f, vi) {
			v.products = append(v.products, f.Name)
		}
	}
	return v, nil
}

func (db *DB) factHolds(f Fact, vi int) bool {
	for _, r := range f.Spans {
		if db.inRange(r, vi) {
			return true
		}
	}
	return false
}

// Version returns the version the view describes.
func (v *View) Version() Version { return v.version }

// Build returns the build the view describes.
func (v *View) Build() Build { return v.build }

// IgnoreUnknownOptions reports whether this version ignores unknown options
// in config files instead of aborting (F6).
func (v *View) IgnoreUnknownOptions() bool { return v.ignore }

// Option returns the definition of --name for tool ("mydumper" or
// "myloader"), or false when the option does not exist in this version and
// build.
func (v *View) Option(tool, name string) (OptionSpan, bool) {
	s, ok := v.options[tool][name]
	if !ok {
		return OptionSpan{}, false
	}
	return cloneSpan(*s), true
}

// History returns every span of tool's option name across the embedded
// versions and builds (DB.History), for messages such as "added in
// v0.20.1-1".
func (v *View) History(tool, name string) []OptionSpan { return v.db.History(tool, name) }

// OptionByShort returns the long name of the option whose short name is
// short (one character, without the dash).
func (v *View) OptionByShort(tool, short string) (name string, ok bool) {
	name, ok = v.shorts[tool][short]
	return name, ok
}

// OptionNames returns the long names of tool's options, sorted.
func (v *View) OptionNames(tool string) []string {
	out := make([]string, 0, len(v.options[tool]))
	for n := range v.options[tool] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TableKey reports whether key is recognized in a table section (F8). Keys
// matched by prefix, such as columns_on_select_replace_`col`, are honored.
func (v *View) TableKey(key string) bool {
	for _, f := range v.keys {
		if key == f.Name || (f.Prefix && strings.HasPrefix(key, f.Name)) {
			return true
		}
	}
	return false
}

// TableKeys returns the table-section keys of this version, sorted. A key
// matched by prefix appears once, as its prefix.
func (v *View) TableKeys() []string {
	out := make([]string, 0, len(v.keys))
	for _, f := range v.keys {
		if !slices.Contains(out, f.Name) {
			out = append(out, f.Name)
		}
	}
	return out
}

// MasqueradeFunctions returns the masking function names of this version,
// sorted (F10). mydumper selects them by prefix of the value.
func (v *View) MasqueradeFunctions() []string { return slices.Clone(v.masq) }

// Products returns the lowercase product names used in per-product groups
// (§3.6), sorted.
func (v *View) Products() []string { return slices.Clone(v.products) }

// ProductOptionGroups returns the tools that read their per-product option
// groups ([mydumper_mysql_8_0], F16): mydumper from v0.21.2-2, myloader never.
func (v *View) ProductOptionGroups() []string { return slices.Clone(v.version.ProductOptionGroups) }

// TableSectionsIgnored returns the tools that lose every table section
// because they load them before creating their store (F17): mydumper
// v0.21.2-2 and v0.21.2-3.
func (v *View) TableSectionsIgnored() []string { return slices.Clone(v.version.TableSectionsIgnored) }
