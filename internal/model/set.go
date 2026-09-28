package model

import (
	"slices"

	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
)

// Origin is the file of a load set an entry comes from.
type Origin uint8

// Origins.
const (
	FromDefaults Origin = iota // --defaults-file
	FromExtra                  // --defaults-extra-file
)

func (o Origin) String() string {
	if o == FromExtra {
		return "extra"
	}
	return "defaults"
}

// SetEntry is one key of a group of a load set.
type SetEntry struct {
	Key, Value string
	Origin     Origin
	Line       int // in the file of Origin
	Effective  bool
	Reason     Reason
}

// SetGroup is one group of a load set, the headers of both files merged.
type SetGroup struct {
	Name          string
	Kind          GroupKind
	Tool          string
	DefaultsLines []int // header lines in the defaults file
	ExtraLines    []int // header lines in the extra file
	Entries       []SetEntry
}

// Connection is what the MySQL client library reads when a tool connects
// (F12–F14): nothing when Read is false, otherwise the [client] section of
// one file, plus the tool's group when Group is set (C1).
type Connection struct {
	Tool  string
	Read  bool
	From  Origin
	Group string
}

// Set is the effective configuration of one invocation that loads a defaults
// file and an extra file (design §9.3).
type Set struct {
	Health      Health // Rejected only when GLib rejects both files
	Fatal       string
	Groups      []SetGroup
	Connections []Connection // one per tool
}

// mergedKind reports whether mydumper reads a kind of group from the merged
// file (F15) rather than from each file on its own.
func mergedKind(k GroupKind) bool {
	switch k {
	case GroupProductOptions, GroupSessionVariables, GroupGlobalVariables, GroupTable:
		return true
	case GroupUnknown, GroupToolOptions, GroupClient:
	}
	return false
}

// BuildSet computes what one invocation applies from a defaults file and an
// extra file (F2, F14, F15). Each file's tool groups are parsed on their
// own, the defaults file first: an option the extra file sets again replaces
// the defaults file's value. The extra file is then merged into the defaults
// file key by key, and the other groups are read from the merged file only:
// when GLib rejects the defaults file, the extra file's product, variable and
// table groups are lost, while its tool groups still apply.
func BuildSet(defaults, extra *keyfile.Result, opt Options) *Set {
	languages := opt.Languages
	if languages == nil {
		languages = []string{"C"}
	}
	dm, xm := Build(defaults, opt), Build(extra, opt)
	s := &Set{Health: OK}
	if dm.Health == Rejected && xm.Health == Rejected {
		s.Health = Rejected
	}
	var mm *Model
	var from []int
	if defaults.Loadable {
		var mk *keyfile.Result
		mk, from = mergeKeyFiles(defaults, extra, languages)
		mm = Build(mk, opt)
	}

	// The merged entry whose reason counts for each extra-file entry: the
	// last occurrence of its key in the merged file.
	counts := map[int]*Entry{}
	mergedAt := map[int]*Entry{}
	if mm != nil {
		for gi := range mm.Groups {
			for ei := range mm.Groups[gi].Entries {
				e := &mm.Groups[gi].Entries[ei]
				mergedAt[e.src] = e
				if x := from[e.src]; x >= 0 && !e.Shadowed {
					counts[x] = e
				}
			}
		}
	}
	overridden := overriddenOptions(dm, xm, opt)

	byName := map[string]int{}
	group := func(g *Group) *SetGroup {
		i, ok := byName[g.Name]
		if !ok {
			i = len(s.Groups)
			byName[g.Name] = i
			s.Groups = append(s.Groups, SetGroup{Name: g.Name, Kind: g.Kind, Tool: g.Tool})
		}
		return &s.Groups[i]
	}
	for gi := range dm.Groups {
		g := &dm.Groups[gi]
		sg := group(g)
		sg.DefaultsLines = g.Lines
		for _, e := range g.Entries {
			r := e.Reason
			switch {
			case mergedKind(g.Kind) && mm != nil:
				r = mergedAt[e.src].Reason
				if from[e.src] >= 0 && r != ReasonShadowed {
					r = ReasonOverriddenByExtraFile
				}
			case overridden[e.src]:
				r = ReasonOverriddenByExtraFile
			}
			sg.Entries = append(sg.Entries, SetEntry{Key: e.Key, Value: e.Value, Origin: FromDefaults, Line: e.Line, Effective: r == ReasonEffective, Reason: r})
		}
	}
	for gi := range xm.Groups {
		g := &xm.Groups[gi]
		sg := group(g)
		sg.ExtraLines = g.Lines
		for _, e := range g.Entries {
			r := e.Reason
			if mergedKind(g.Kind) && xm.Health != Rejected && r != ReasonLocalized && r != ReasonShadowed {
				if me, ok := counts[e.src]; ok {
					r = me.Reason
				} else {
					r = ReasonDefaultsFileRejected
				}
			}
			sg.Entries = append(sg.Entries, SetEntry{Key: e.Key, Value: e.Value, Origin: FromExtra, Line: e.Line, Effective: r == ReasonEffective, Reason: r})
		}
	}

	// The first startup failure: the defaults file's tool groups, the extra
	// file's, then the per-product groups of the merged file.
	fatal := ""
	for _, m := range []*Model{dm, xm} {
		if m.Health != Rejected {
			fatal = firstFatal(fatal, m, GroupToolOptions)
		}
	}
	if mm != nil {
		fatal = firstFatal(fatal, mm, GroupProductOptions)
	}
	if fatal != "" && s.Health == OK {
		s.Health, s.Fatal = FatalAtStartup, fatal
	}
	for _, tool := range []string{"mydumper", "myloader"} {
		s.Connections = append(s.Connections, connection(tool, dm, xm))
	}
	return s
}

// firstFatal returns fatal, or when it is empty the startup error of the
// first group of that kind GOption fails to parse.
func firstFatal(fatal string, m *Model, kind GroupKind) string {
	if fatal != "" {
		return fatal
	}
	for _, g := range m.Groups {
		if g.Kind == kind && g.GOption != nil && !g.GOption.Result.OK {
			return g.Tool + ": option parsing failed: " + g.GOption.Result.Error + ", try --help"
		}
	}
	return ""
}

// connection is mydumper's choice of the client library's defaults file
// (F12–F14): each file in turn takes over when it has the tool's group or
// [client], or when GLib rejects it; [client] resets the group to none.
func connection(tool string, dm, xm *Model) Connection {
	c := Connection{Tool: tool}
	for _, f := range []struct {
		m    *Model
		from Origin
	}{{dm, FromDefaults}, {xm, FromExtra}} {
		if f.m.Health == Rejected {
			c = Connection{Tool: tool, Read: true, From: f.from}
			continue
		}
		if hasGroup(f.m, tool) {
			c = Connection{Tool: tool, Read: true, From: f.from, Group: tool}
		}
		if hasGroup(f.m, "client") {
			c = Connection{Tool: tool, Read: true, From: f.from}
		}
	}
	return c
}

func hasGroup(m *Model, name string) bool {
	for _, g := range m.Groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

// overriddenOptions returns the defaults-file entries (keyfile indexes) of
// tool groups whose option an effective entry of the extra file's same group
// sets again. Callbacks are left out: GOption calls them for both values, and
// what the second call does to the first is the callback's business.
func overriddenOptions(dm, xm *Model, opt Options) map[int]bool {
	out := map[int]bool{}
	if opt.Target == nil || dm.Health == Rejected || xm.Health == Rejected {
		return out
	}
	for _, xg := range xm.Groups {
		if xg.Kind != GroupToolOptions {
			continue
		}
		ctx := OptionContext(opt.Target, xg.Tool, opt.Charset)
		var set []goption.Ref
		for _, e := range xg.Entries {
			if r, ok := optionOf(ctx, e); ok {
				set = append(set, r)
			}
		}
		for _, dg := range dm.Groups {
			if dg.Name != xg.Name {
				continue
			}
			for _, e := range dg.Entries {
				if r, ok := optionOf(ctx, e); ok && slices.Contains(set, r) {
					out[e.src] = true
				}
			}
		}
	}
	return out
}

// optionOf returns the option an effective entry sets, unless it is a
// callback.
func optionOf(ctx *goption.Context, e Entry) (goption.Ref, bool) {
	if !e.Effective || e.Element == 0 {
		return goption.Ref{}, false
	}
	r, _, ok := ctx.Lookup(e.Key)
	if !ok || ctx.Entry(r).Arg == goption.ArgCallback {
		return goption.Ref{}, false
	}
	return r, true
}

// mergeKeyFiles is mydumper's m_key_file_merge: g_key_file_set_value into
// the defaults file for every key the extra file lists (F15). A key present
// in both takes the extra file's value (every occurrence, since GLib returns
// the last value for all of them, K13); a new key is appended to the last
// header of its group, a new group to the file. from[i] is the extra-file
// entry whose value entry i of the result holds, or -1. A rejected extra
// file merges nothing.
func mergeKeyFiles(d, x *keyfile.Result, languages []string) (*keyfile.Result, []int) {
	m := &keyfile.Result{Loadable: true, Lines: d.Lines, Groups: slices.Clone(d.Groups), Entries: slices.Clone(d.Entries)}
	from := make([]int, len(m.Entries))
	for i := range from {
		from[i] = -1
	}
	if !x.Loadable {
		return m, from
	}
	type groupKey struct{ group, key string }
	lastHeader := map[string]int{}
	for i, g := range m.Groups {
		lastHeader[g.Name] = i
	}
	occurrences := map[groupKey][]int{}
	for i, e := range m.Entries {
		if visible(e, languages) {
			k := groupKey{m.Groups[e.Group].Name, e.Key}
			occurrences[k] = append(occurrences[k], i)
		}
	}
	// get_value returns the last occurrence's value (K13).
	last := map[groupKey]int{}
	for i, e := range x.Entries {
		if visible(e, languages) {
			last[groupKey{x.Groups[e.Group].Name, e.Key}] = i
		}
	}
	// get_groups lists groups by first appearance, get_keys in file order.
	var names []string
	seen := map[string]bool{}
	for _, g := range x.Groups {
		if !seen[g.Name] {
			seen[g.Name] = true
			names = append(names, g.Name)
		}
	}
	for _, name := range names {
		for _, e := range x.Entries {
			if x.Groups[e.Group].Name != name || !visible(e, languages) {
				continue
			}
			k := groupKey{name, e.Key}
			xi := last[k]
			if occ, ok := occurrences[k]; ok {
				for _, i := range occ {
					m.Entries[i].Value, from[i] = x.Entries[xi].Value, xi
				}
				continue
			}
			gi, ok := lastHeader[name]
			if !ok {
				hdr := x.Groups[x.Entries[xi].Group]
				gi = len(m.Groups)
				m.Groups = append(m.Groups, keyfile.Group{Name: name, Line: hdr.Line, Valid: true})
				lastHeader[name] = gi
			}
			ne := x.Entries[xi]
			ne.Group = gi
			m.Entries = append(m.Entries, ne)
			from = append(from, xi)
			occurrences[k] = []int{len(m.Entries) - 1}
		}
	}
	return m, from
}
