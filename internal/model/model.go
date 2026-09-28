// Package model computes the effective configuration of a file: what mydumper
// and myloader actually apply, with the reason for every entry that has no
// effect (design §4.5).
//
// The model is what `inspect` prints and what the fixer's self-check compares:
// two files with equal projections configure mydumper identically.
package model

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/goption"
	"github.com/tomsihap/mydumper-lint/internal/keyfile"
	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// Health orders how far mydumper gets with a file.
type Health uint8

// Healths, from worst to best.
const (
	Rejected       Health = iota // GLib rejects the file: mydumper ignores it entirely (F1)
	FatalAtStartup               // the file loads but mydumper aborts while applying it
	OK
)

var healthNames = [...]string{Rejected: "rejected", FatalAtStartup: "fatal-at-startup", OK: "ok"}

func (h Health) String() string {
	if int(h) < len(healthNames) {
		return healthNames[h]
	}
	return "Health(" + strconv.Itoa(int(h)) + ")"
}

// GroupKind tells who reads a group (design §3.6).
type GroupKind uint8

// Group kinds.
const (
	GroupUnknown          GroupKind = iota // read by nobody
	GroupToolOptions                       // [mydumper], [myloader]
	GroupProductOptions                    // [mydumper_mysql_8_0]
	GroupSessionVariables                  // [mydumper_session_variables…]
	GroupGlobalVariables                   // [myloader_global_variables…]
	GroupClient                            // [client], read by libmysqlclient
	GroupTable                             // [`db`.`table`]
)

var groupKindNames = [...]string{
	GroupUnknown:          "unknown",
	GroupToolOptions:      "tool-options",
	GroupProductOptions:   "product-options",
	GroupSessionVariables: "session-variables",
	GroupGlobalVariables:  "global-variables",
	GroupClient:           "client",
	GroupTable:            "table",
}

func (k GroupKind) String() string {
	if int(k) < len(groupKindNames) {
		return groupKindNames[k]
	}
	return "GroupKind(" + strconv.Itoa(int(k)) + ")"
}

// Reason explains whether and why an entry has an effect.
type Reason uint8

// Reasons (design §4.5).
const (
	ReasonEffective          Reason = iota
	ReasonFileRejected              // the whole file is ignored (F1)
	ReasonUnknownGroup              // nobody reads the group
	ReasonLocalized                 // key[locale] filtered out by GLib (K14)
	ReasonShadowed                  // an earlier duplicate: its value is replaced (K13)
	ReasonConnectionKey             // host/user/password: read by libmysqlclient, not GOption (F3)
	ReasonUnknownOption             // not an option of the tool for the target version
	ReasonFatalAtStartup            // makes mydumper abort before dumping
	ReasonUnknownTableKey           // not a table-section key (F8)
	ReasonMasqueradeIdentity        // unknown masking function: column exported in plaintext (F10)
	ReasonConsumedAsValue           // swallowed as the value of an option parsed from a value (G8)
	ReasonAfterEndOfOptions         // after a "--" value that ended option parsing (G9)
)

var reasonNames = [...]string{
	ReasonEffective:          "effective",
	ReasonFileRejected:       "file-rejected",
	ReasonUnknownGroup:       "unknown-group",
	ReasonLocalized:          "localized",
	ReasonShadowed:           "shadowed-by-duplicate",
	ReasonConnectionKey:      "connection-key",
	ReasonUnknownOption:      "unknown-option",
	ReasonFatalAtStartup:     "fatal-at-startup",
	ReasonUnknownTableKey:    "unknown-table-key",
	ReasonMasqueradeIdentity: "masquerade-fallback-identity",
	ReasonConsumedAsValue:    "consumed-as-option-value",
	ReasonAfterEndOfOptions:  "after-end-of-options",
}

func (r Reason) String() string {
	if int(r) < len(reasonNames) {
		return reasonNames[r]
	}
	return "Reason(" + strconv.Itoa(int(r)) + ")"
}

// Target is what the model needs to know about the mydumper version and
// build. *optionsdb.View implements it. A nil Target limits the model to what
// GLib does (no option semantics).
type Target interface {
	IgnoreUnknownOptions() bool
	Option(tool, name string) (optionsdb.OptionSpan, bool)
	OptionNames(tool string) []string
	TableKey(key string) bool
	MasqueradeFunctions() []string
	Products() []string
	ProductOptionGroups() []string
}

// Options configures Build.
type Options struct {
	// Languages is the runtime language list of the mydumper process (K14);
	// see keyfile.LanguageNames. Nil means ["C"].
	Languages []string
	Target    Target
	// Charset is the character set of mydumper's locale, which decides
	// whether GOption accepts non-ASCII values. The zero value is the C
	// locale (no LANG), like the Languages default: the official images and
	// cron jobs run that way.
	Charset goption.Charset
}

// Entry is one key of a group, in file order.
type Entry struct {
	Key, Value string
	Line       int
	Effective  bool
	Reason     Reason
	// Element is the index of "--key" in the group's GOption vector
	// (Group.GOption.Argv), the value following it; 0 when the key is not
	// passed to GOption.
	Element int
	// Shadowed is set for an earlier occurrence of a duplicate key: its
	// value is replaced by the last one (K13), even in a rejected file.
	Shadowed bool
	// LastLine is the line of the occurrence whose value counts (the last
	// visible one), 0 when none is visible.
	LastLine int
	passed   bool // GLib lists it and mydumper passes it to GOption
}

// Group is one group as GLib returns it (duplicate headers merged).
type Group struct {
	Name    string
	Kind    GroupKind
	Tool    string // mydumper or myloader, for option and variable groups
	Lines   []int  // header lines
	Entries []Entry
	// GOption is how GLib parses an option group when the target is known;
	// nil otherwise.
	GOption *GOptionRun
}

// Model is the effective configuration of one file.
type Model struct {
	Health Health
	Fatal  string // the startup error when Health is FatalAtStartup, prefixed with the tool
	Groups []Group
}

// Readers is what decides who reads a group besides its name (design §3.6):
// the product names of per-product groups, and the tools that read their
// per-product option groups (F16).
type Readers struct {
	Products       []string // lowercase product names
	ProductOptions []string // tools that read [<tool>_<product>…] option groups
}

// DefaultReaders applies when no target is set: the products and readers of
// the newest versions.
var DefaultReaders = Readers{
	Products:       []string{"mysql", "percona", "mariadb", "tidb", "rds", "google", "clickhouse", "dolt", "unknown"},
	ProductOptions: []string{"mydumper"},
}

// ReadersOf returns the readers of a target, or DefaultReaders for nil.
func ReadersOf(t Target) Readers {
	if t == nil {
		return DefaultReaders
	}
	return Readers{Products: t.Products(), ProductOptions: t.ProductOptionGroups()}
}

var (
	productGroupRE  = regexp.MustCompile(`^(mydumper|myloader)_([a-z]+)(?:_[0-9]+(?:_[0-9]+(?:_[0-9]+)?)?)?$`)
	variableGroupRE = regexp.MustCompile(`^(mydumper|myloader)_(session|global)_variables(?:_([a-z]+)(?:_[0-9]+(?:_[0-9]+(?:_[0-9]+)?)?)?)?$`)
)

// ClassifyGroup tells who reads the group called name, and for which tool.
// A per-product option group of a tool that does not read them
// ([myloader_mysql], or [mydumper_mysql] before v0.21.2-2) is read by nobody
// (F16).
func ClassifyGroup(name string, r Readers) (GroupKind, string) {
	products := r.Products
	switch {
	case name == "mydumper" || name == "myloader":
		return GroupToolOptions, name
	case name == "client":
		return GroupClient, ""
	case IsTableGroup(name):
		return GroupTable, ""
	}
	if m := variableGroupRE.FindStringSubmatch(name); m != nil {
		if m[3] != "" && !contains(products, m[3]) {
			return GroupUnknown, ""
		}
		if m[2] == "session" {
			return GroupSessionVariables, m[1]
		}
		return GroupGlobalVariables, m[1]
	}
	if tool, ok := ProductOptionGroup(name, products); ok && contains(r.ProductOptions, tool) {
		return GroupProductOptions, tool
	}
	return GroupUnknown, ""
}

// ProductOptionGroup reports whether name is a per-product option group of a
// known product ([mydumper_mysql], [myloader_mariadb_10_6]) and for which
// tool, whether or not that tool reads it.
func ProductOptionGroup(name string, products []string) (string, bool) {
	if m := productGroupRE.FindStringSubmatch(name); len(m) > 2 && contains(products, m[2]) {
		return m[1], true
	}
	return "", false
}

// IsTableGroup is mydumper's table-section test (F7): the name starts with a
// backtick, contains "`.`" and ends with a backtick.
func IsTableGroup(name string) bool {
	return len(name) >= 2 && name[0] == '`' && name[len(name)-1] == '`' && strings.Contains(name, "`.`")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// connectionKeys are read by libmysqlclient instead of GOption (F3).
var connectionKeys = map[string]bool{"host": true, "user": true, "password": true}

// Build computes the model of a parsed file.
func Build(kf *keyfile.Result, opt Options) *Model {
	languages := opt.Languages
	if languages == nil {
		languages = []string{"C"}
	}
	readers := ReadersOf(opt.Target)
	m := &Model{Health: OK}
	if !kf.Loadable {
		m.Health = Rejected
	}
	pos := map[string]int{}
	lastLine, lastValue := lastOccurrences(kf, languages)
	byGroup := make([][]int, len(kf.Groups)) // entry indexes of each header
	for i, e := range kf.Entries {
		byGroup[e.Group] = append(byGroup[e.Group], i)
	}
	for gi, g := range kf.Groups {
		if !g.Valid && kf.Loadable {
			continue
		}
		i, seen := pos[g.Name]
		if !seen {
			kind, tool := ClassifyGroup(g.Name, readers)
			i = len(m.Groups)
			pos[g.Name] = i
			m.Groups = append(m.Groups, Group{Name: g.Name, Kind: kind, Tool: tool})
		}
		m.Groups[i].Lines = append(m.Groups[i].Lines, g.Line)
		if m.Groups[i].Entries == nil {
			m.Groups[i].Entries = make([]Entry, 0, len(byGroup[gi]))
		}
		for _, ei := range byGroup[gi] {
			e := kf.Entries[ei]
			last := lastLine[ei]
			entry := Entry{Key: e.Key, Value: lastValue[ei], Line: e.Line, LastLine: last, Shadowed: last != 0 && e.Line != last}
			entry.Reason = reason(m, m.Groups[i], e, languages, last, opt.Target)
			kind := m.Groups[i].Kind
			entry.passed = (kind == GroupToolOptions || kind == GroupProductOptions) &&
				visible(e, languages) && !connectionKeys[e.Key]
			entry.Effective = entry.Reason == ReasonEffective
			m.Groups[i].Entries = append(m.Groups[i].Entries, entry)
		}
	}
	if opt.Target != nil {
		applyGOption(m, opt.Target, opt.Charset)
	}
	return m
}

func visible(e keyfile.Entry, languages []string) bool {
	return e.Locale == "" || keyfile.IsInterestingLocale(e.Locale, languages)
}

func reason(m *Model, g Group, e keyfile.Entry, languages []string, lastLine int, t Target) Reason {
	switch {
	case m.Health == Rejected:
		return ReasonFileRejected
	case g.Kind == GroupUnknown:
		return ReasonUnknownGroup
	case !visible(e, languages):
		return ReasonLocalized
	case e.Line != lastLine:
		return ReasonShadowed
	case (g.Kind == GroupToolOptions || g.Kind == GroupProductOptions) && connectionKeys[e.Key]:
		return ReasonConnectionKey
	case g.Kind == GroupTable && t != nil:
		return tableReason(e, t)
	}
	return ReasonEffective
}

// tableReason is the reason of a key of a table section (§3.5): a known
// table key, a masked column whose value names a masking function, or
// neither.
func tableReason(e keyfile.Entry, t Target) Reason {
	if IsMaskedColumn(e.Key) {
		if MasqueradeFunction(e.Value, t.MasqueradeFunctions()) == "" {
			return ReasonMasqueradeIdentity
		}
		return ReasonEffective
	}
	if t.TableKey(e.Key) {
		return ReasonEffective
	}
	return ReasonUnknownTableKey
}

// IsMaskedColumn reports whether a key of a table section declares a masked
// column (F9): it starts with a backtick and contains a second one.
func IsMaskedColumn(key string) bool {
	return len(key) > 1 && key[0] == '`' && strings.IndexByte(key[1:], '`') >= 0
}

// MasqueradeFunction returns the masking function mydumper selects for a
// masked column's value: the known name the value starts with (F10), or ""
// when none matches and mydumper falls back to identity.
func MasqueradeFunction(value string, functions []string) string {
	for _, f := range functions {
		if strings.HasPrefix(value, f) {
			return f
		}
	}
	return ""
}

// Projection is a canonical text of everything mydumper applies from the
// file: the groups it reads and their effective entries (connection keys
// included, since libmysqlclient reads them). A rejected file applies
// nothing. Safe fixes must preserve the projection of the recovered file.
func (m *Model) Projection() string {
	if m.Health == Rejected {
		return ""
	}
	var b strings.Builder
	for _, g := range m.Groups {
		if g.Kind == GroupUnknown {
			continue
		}
		b.WriteString("[" + g.Name + "]\n")
		for _, e := range g.Entries {
			if e.Effective || e.Reason == ReasonConnectionKey {
				b.WriteString(strconv.Quote(e.Key) + "=" + strconv.Quote(e.Value) + "\n")
			}
		}
	}
	if m.Health == FatalAtStartup {
		b.WriteString("fatal: " + m.Fatal + "\n")
	}
	return b.String()
}

// lastOccurrences returns, for each entry of kf, the line of the last
// visible occurrence of its (group, key) in a valid group (0 when none), and
// the value of its last occurrence, which GLib returns for every occurrence
// (K13). Entries are sorted by (group, key) instead of hashed: large files
// have thousands of keys.
func lastOccurrences(kf *keyfile.Result, languages []string) (lastLine []int, lastValue []string) {
	n := len(kf.Entries)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	name := func(i int) string { return kf.Groups[kf.Entries[i].Group].Name }
	sort.SliceStable(order, func(a, b int) bool {
		ea, eb := &kf.Entries[order[a]], &kf.Entries[order[b]]
		if na, nb := name(order[a]), name(order[b]); na != nb {
			return na < nb
		}
		return ea.Key < eb.Key
	})
	lastLine, lastValue = make([]int, n), make([]string, n)
	for start := 0; start < n; {
		end := start + 1
		for end < n && name(order[end]) == name(order[start]) && kf.Entries[order[end]].Key == kf.Entries[order[start]].Key {
			end++
		}
		line, value := 0, kf.Entries[order[end-1]].Value
		for _, i := range order[start:end] {
			e := &kf.Entries[i]
			if kf.Groups[e.Group].Valid && visible(*e, languages) {
				line = e.Line
			}
		}
		for _, i := range order[start:end] {
			lastLine[i], lastValue[i] = line, value
		}
		start = end
	}
	return lastLine, lastValue
}
