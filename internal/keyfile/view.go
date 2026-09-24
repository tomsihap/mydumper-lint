package keyfile

// ViewEntry is one key as returned by g_key_file_get_keys, with the value
// g_key_file_get_value returns for it.
type ViewEntry struct {
	Key, Value string
}

// ViewGroup is one group as returned by g_key_file_get_groups.
type ViewGroup struct {
	Name    string
	Entries []ViewEntry
}

// GLibView returns what mydumper gets from GLib for a loadable file: groups in
// order of first appearance (duplicates merged, K12), keys in file order with
// duplicates listed twice and both carrying the last value (K13), and
// localized keys only when their locale is in languages (K14). It returns nil
// for a file GLib rejects: mydumper ignores such a file entirely (F1).
func (r *Result) GLibView(languages []string) []ViewGroup {
	if !r.Loadable {
		return nil
	}
	var view []ViewGroup
	pos := map[string]int{}
	for _, g := range r.Groups {
		if _, seen := pos[g.Name]; g.Valid && !seen {
			pos[g.Name] = len(view)
			view = append(view, ViewGroup{Name: g.Name})
		}
	}
	type key struct{ group, key string }
	last := map[key]string{}
	for _, e := range r.Entries {
		g := r.Groups[e.Group]
		if !g.Valid || (e.Locale != "" && !IsInterestingLocale(e.Locale, languages)) {
			continue
		}
		i := pos[g.Name]
		view[i].Entries = append(view[i].Entries, ViewEntry{Key: e.Key})
		last[key{g.Name, e.Key}] = e.Value
	}
	for i := range view {
		for j := range view[i].Entries {
			view[i].Entries[j].Value = last[key{view[i].Name, view[i].Entries[j].Key}]
		}
	}
	return view
}
