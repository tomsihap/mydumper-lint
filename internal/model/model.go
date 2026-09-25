// Package model computes the effective configuration of a file: what mydumper
// and myloader actually apply, with the reason for every entry that has no
// effect (design §4.5).
//
// The model is what `inspect` prints and what the fixer's self-check compares:
// two files with equal projections configure mydumper identically.
package model

import (
	"regexp"
	"strconv"
	"strings"

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
	TableKey(key string) bool
	MasqueradeFunctions() []string
	Products() []string
}

// Options configures Build.
type Options struct {
	// Languages is the runtime language list of the mydumper process (K14);
	// see keyfile.LanguageNames. Nil means ["C"].
	Languages []string
	Target    Target
}

// Entry is one key of a group, in file order.
type Entry struct {
	Key, Value string
	Line       int
	Effective  bool
	Reason     Reason
}

// Group is one group as GLib returns it (duplicate headers merged).
type Group struct {
	Name    string
	Kind    GroupKind
	Tool    string // mydumper or myloader, for option and variable groups
	Lines   []int  // header lines
	Entries []Entry
}

// Model is the effective configuration of one file.
type Model struct {
	Health Health
	Fatal  string // the startup error when Health is FatalAtStartup
	Groups []Group
}

// defaultProducts is used when no target is set (design §3.6).
var defaultProducts = []string{"mysql", "percona", "mariadb", "tidb", "rds", "google", "clickhouse", "dolt", "unknown"}

var (
	productGroupRE  = regexp.MustCompile(`^(mydumper|myloader)_([a-z]+)(?:_[0-9]+(?:_[0-9]+(?:_[0-9]+)?)?)?$`)
	variableGroupRE = regexp.MustCompile(`^(mydumper|myloader)_(session|global)_variables(?:_([a-z]+)(?:_[0-9]+(?:_[0-9]+(?:_[0-9]+)?)?)?)?$`)
)

// ClassifyGroup tells who reads the group called name, and for which tool.
// products is the list of lowercase product names (nil: the known list).
func ClassifyGroup(name string, products []string) (GroupKind, string) {
	if products == nil {
		products = defaultProducts
	}
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
	if m := productGroupRE.FindStringSubmatch(name); len(m) > 2 && contains(products, m[2]) {
		return GroupProductOptions, m[1]
	}
	return GroupUnknown, ""
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
	var products []string
	if opt.Target != nil {
		products = opt.Target.Products()
	}
	m := &Model{Health: OK}
	if !kf.Loadable {
		m.Health = Rejected
	}
	pos := map[string]int{}
	lastLine := map[[2]string]int{} // (group, key) -> line of the last visible occurrence
	for _, e := range kf.Entries {
		g := kf.Groups[e.Group]
		if g.Valid && visible(e, languages) {
			lastLine[[2]string{g.Name, e.Key}] = e.Line
		}
	}
	lastValue := map[[2]string]string{}
	for _, e := range kf.Entries {
		lastValue[[2]string{kf.Groups[e.Group].Name, e.Key}] = e.Value
	}
	for gi, g := range kf.Groups {
		if !g.Valid && kf.Loadable {
			continue
		}
		i, seen := pos[g.Name]
		if !seen {
			kind, tool := ClassifyGroup(g.Name, products)
			i = len(m.Groups)
			pos[g.Name] = i
			m.Groups = append(m.Groups, Group{Name: g.Name, Kind: kind, Tool: tool})
		}
		m.Groups[i].Lines = append(m.Groups[i].Lines, g.Line)
		for _, e := range kf.Entries {
			if e.Group != gi {
				continue
			}
			k := [2]string{g.Name, e.Key}
			entry := Entry{Key: e.Key, Value: lastValue[k], Line: e.Line}
			entry.Reason = reason(m, m.Groups[i], e, languages, lastLine[k])
			entry.Effective = entry.Reason == ReasonEffective
			m.Groups[i].Entries = append(m.Groups[i].Entries, entry)
		}
	}
	return m
}

func visible(e keyfile.Entry, languages []string) bool {
	return e.Locale == "" || keyfile.IsInterestingLocale(e.Locale, languages)
}

func reason(m *Model, g Group, e keyfile.Entry, languages []string, lastLine int) Reason {
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
	}
	return ReasonEffective
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
