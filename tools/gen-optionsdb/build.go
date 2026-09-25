package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/optionsdb"
)

// release is one embedded version before extraction.
type release struct {
	tag        string
	parsed     optionsdb.Tag
	commit     string
	hasRelease bool   // a (non-draft) GitHub release object exists
	prerelease bool   // GitHub's flag; true when there is no release object
	date       string // YYYY-MM-DD of the GitHub release
	image      bool   // official image known to exist (checked for tags without a release)
}

// buildDB merges the per-version extractions into the consolidated history
// (design §5.2). rels[i] is xs[i]; verified[tag] is image_verified.
func buildDB(rels []release, xs []*extraction, verified map[string]bool) (*optionsdb.DB, error) {
	if len(rels) != len(xs) || len(rels) == 0 {
		return nil, fmt.Errorf("buildDB: %d versions, %d extractions", len(rels), len(xs))
	}
	db := &optionsdb.DB{Schema: optionsdb.SchemaVersion}
	for i, r := range rels {
		db.Versions = append(db.Versions, optionsdb.Version{
			Tag:                  r.tag,
			Commit:               r.commit,
			Date:                 r.date,
			Prerelease:           r.prerelease,
			IgnoreUnknownOptions: xs[i].ignoreUnknown,
			LoaderFingerprint:    xs[i].fingerprint,
			Preprocessor:         xs[i].preprocessor,
			ImageVerified:        verified[r.tag],
		})
	}
	last := len(rels) - 1
	rangeOf := func(from, to int) optionsdb.Range {
		r := optionsdb.Range{From: rels[from].tag}
		if to != last {
			r.To = rels[to].tag
		}
		return r
	}

	for _, tool := range tools {
		names := map[string]bool{}
		for _, x := range xs {
			for n := range x.options[tool] {
				names[n] = true
			}
		}
		for _, name := range sortedKeys(names) {
			type occurrence struct {
				vi int
				v  optVariant
			}
			byKey := map[string][]occurrence{}
			var keys []string
			for vi, x := range xs {
				for _, v := range x.options[tool][name] {
					k := variantKey(v)
					if _, ok := byKey[k]; !ok {
						keys = append(keys, k)
					}
					byKey[k] = append(byKey[k], occurrence{vi, v})
				}
			}
			type spanAt struct {
				from int
				key  string
				span optionsdb.OptionSpan
			}
			var spans []spanAt
			for _, k := range keys {
				occ := byKey[k]
				for i := 0; i < len(occ); {
					j := i
					for j+1 < len(occ) && occ[j+1].vi == occ[j].vi+1 {
						j++
					}
					v := occ[i].v
					spans = append(spans, spanAt{from: occ[i].vi, key: k, span: optionsdb.OptionSpan{
						Range:            rangeOf(occ[i].vi, occ[j].vi),
						Short:            v.def.Short,
						Arg:              v.def.Arg,
						Flags:            v.def.Flags,
						Group:            v.def.Group,
						Condition:        v.cond,
						IsRegex:          v.isRegex,
						Values:           v.values,
						ValuesIgnoreCase: v.valuesIgnoreCase,
						Source:           v.source,
					}})
					i = j + 1
				}
			}
			slices.SortFunc(spans, func(a, b spanAt) int {
				return cmp.Or(cmp.Compare(a.from, b.from), strings.Compare(a.span.Condition, b.span.Condition), strings.Compare(a.key, b.key))
			})
			o := optionsdb.Option{Name: name, Tool: tool}
			for _, s := range spans {
				o.Spans = append(o.Spans, s.span)
			}
			db.Options = append(db.Options, o)
		}
	}

	facts := func(get func(*extraction) []factName) []optionsdb.Fact {
		type k struct {
			name   string
			prefix bool
		}
		at := map[k][]int{}
		for vi, x := range xs {
			for _, f := range get(x) {
				key := k(f)
				at[key] = append(at[key], vi)
			}
		}
		keys := make([]k, 0, len(at))
		for key := range at {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, func(a, b k) int {
			if c := strings.Compare(a.name, b.name); c != 0 {
				return c
			}
			switch {
			case a.prefix == b.prefix:
				return 0
			case !a.prefix:
				return -1
			}
			return 1
		})
		var out []optionsdb.Fact
		for _, key := range keys {
			f := optionsdb.Fact{Name: key.name, Prefix: key.prefix}
			vis := at[key]
			for i := 0; i < len(vis); {
				j := i
				for j+1 < len(vis) && vis[j+1] == vis[j]+1 {
					j++
				}
				f.Spans = append(f.Spans, rangeOf(vis[i], vis[j]))
				i = j + 1
			}
			out = append(out, f)
		}
		return out
	}
	db.TableKeys = facts(func(x *extraction) []factName { return x.tableKeys })
	db.MasqueradeFunctions = facts(func(x *extraction) []factName { return x.masquerade })
	db.Products = facts(func(x *extraction) []factName {
		out := make([]factName, 0, len(x.products))
		for _, p := range x.products {
			out = append(out, factName{name: p})
		}
		return out
	})
	return db, nil
}

// variantKey identifies a variant up to its source location: two versions
// with the same key belong to the same span.
func variantKey(v optVariant) string {
	b, _ := json.Marshal([]any{v.def.Short, v.def.Arg, v.def.Flags, v.def.Group, v.cond, v.isRegex, v.values, v.valuesIgnoreCase})
	return string(b)
}

// encodeDB renders the knowledge base as committed: 2-space indentation and a
// final newline. Field order is the struct order; every list is sorted by
// buildDB, so the output is deterministic.
func encodeDB(db *optionsdb.DB) ([]byte, error) {
	return encodeJSON(db)
}

// changes describes, version by version, what differs from the previous
// embedded version: the summary a reviewer of an upstream sync needs.
func changes(rels []release, xs []*extraction) []string {
	var out []string
	for i := 1; i < len(xs); i++ {
		prev, cur := xs[i-1], xs[i]
		var parts []string
		for _, tool := range tools {
			var added, removed, changed []string
			for _, n := range sortedKeys(cur.options[tool]) {
				p, ok := prev.options[tool][n]
				switch {
				case !ok:
					added = append(added, n)
				case !sameVariants(p, cur.options[tool][n]):
					changed = append(changed, n)
				}
			}
			for _, n := range sortedKeys(prev.options[tool]) {
				if _, ok := cur.options[tool][n]; !ok {
					removed = append(removed, n)
				}
			}
			if len(added)+len(removed)+len(changed) == 0 {
				continue
			}
			var s []string
			if len(added) > 0 {
				s = append(s, "added "+strings.Join(added, " "))
			}
			if len(removed) > 0 {
				s = append(s, "removed "+strings.Join(removed, " "))
			}
			if len(changed) > 0 {
				s = append(s, "changed "+strings.Join(changed, " "))
			}
			parts = append(parts, tool+": "+strings.Join(s, "; "))
		}
		if prev.ignoreUnknown != cur.ignoreUnknown {
			parts = append(parts, fmt.Sprintf("ignore_unknown_options %v -> %v", prev.ignoreUnknown, cur.ignoreUnknown))
		}
		for _, fn := range loaderFunctions {
			if prev.funcPrints[fn] != cur.funcPrints[fn] {
				parts = append(parts, "loader fingerprint changed: "+fn+" (review the emulators)")
			}
		}
		if !slices.Equal(prev.tableKeys, cur.tableKeys) {
			parts = append(parts, "table keys: "+factList(cur.tableKeys))
		}
		if !slices.Equal(prev.masquerade, cur.masquerade) {
			parts = append(parts, "masking functions: "+factList(cur.masquerade))
		}
		if !slices.Equal(prev.products, cur.products) {
			parts = append(parts, "products: "+strings.Join(cur.products, " "))
		}
		for _, p := range parts {
			out = append(out, rels[i].tag+": "+p)
		}
	}
	return out
}

func sameVariants(a, b []optVariant) bool {
	return slices.EqualFunc(a, b, func(x, y optVariant) bool { return variantKey(x) == variantKey(y) })
}
