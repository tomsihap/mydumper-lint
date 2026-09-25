package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// overlayFile is tools/gen-optionsdb/overlay.json: facts the extractor cannot
// infer, maintained by hand. Every entry covers an explicit version range and
// cites its evidence, so that a new upstream version never inherits a fact
// nobody checked: the generator warns about an entry that stops before a
// version where the option still exists.
type overlayFile struct {
	Comment []string       `json:"comment,omitempty"`
	Options []overlayEntry `json:"options"`
}

type overlayEntry struct {
	Tools            []string `json:"tools"`
	Name             string   `json:"name"`
	From             string   `json:"from"`
	To               string   `json:"to"`
	IsRegex          bool     `json:"is_regex,omitempty"`
	Values           []string `json:"values,omitempty"`
	ValuesIgnoreCase bool     `json:"values_ignore_case,omitempty"`
	Evidence         string   `json:"evidence"`
}

func (e overlayEntry) String() string {
	return fmt.Sprintf("--%s (%s) %s..%s", e.Name, joinTools(e.Tools), e.From, e.To)
}

func joinTools(ts []string) string {
	out := ""
	for i, t := range ts {
		if i > 0 {
			out += ","
		}
		out += t
	}
	return out
}

func loadOverlay(path string) (*overlayFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseOverlay(b)
}

func parseOverlay(b []byte) (*overlayFile, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var o overlayFile
	if err := dec.Decode(&o); err != nil {
		return nil, fmt.Errorf("overlay: %w", err)
	}
	return &o, nil
}

// apply validates the overlay against the extracted versions (tags[i] is
// xs[i]) and sets is_regex and values on every variant it covers. It
// returns warnings about ranges that end before the option does.
func (o *overlayFile) apply(tags []string, xs []*extraction) ([]string, error) {
	pos := map[string]int{}
	for i, t := range tags {
		pos[t] = i
	}
	covered := map[string]overlayEntry{} // tool/name/version → entry
	key := func(tool, name string, vi int) string { return tool + "/" + name + "/" + tags[vi] }
	for _, e := range o.Options {
		if len(e.Tools) == 0 || e.Name == "" || e.Evidence == "" {
			return nil, fmt.Errorf("overlay %s: tools, name and evidence are required", e)
		}
		if !e.IsRegex && len(e.Values) == 0 {
			return nil, fmt.Errorf("overlay %s: sets neither is_regex nor values", e)
		}
		if e.ValuesIgnoreCase && len(e.Values) == 0 {
			return nil, fmt.Errorf("overlay %s: values_ignore_case without values", e)
		}
		for i, v := range e.Values {
			if v == "" || (i > 0 && e.Values[i-1] >= v) {
				return nil, fmt.Errorf("overlay %s: values must be non-empty, sorted and unique", e)
			}
		}
		from, okFrom := pos[e.From]
		to, okTo := pos[e.To]
		if !okFrom || !okTo {
			return nil, fmt.Errorf("overlay %s: from and to must be embedded versions", e)
		}
		if to < from {
			return nil, fmt.Errorf("overlay %s: reversed range", e)
		}
		for ti, tool := range e.Tools {
			if !slices.Contains(tools, tool) || slices.Contains(e.Tools[:ti], tool) {
				return nil, fmt.Errorf("overlay %s: unknown or repeated tool %q", e, tool)
			}
			for vi := from; vi <= to; vi++ {
				vars := xs[vi].options[tool][e.Name]
				if len(vars) == 0 {
					return nil, fmt.Errorf("overlay %s: %s has no option --%s in %s", e, tool, e.Name, tags[vi])
				}
				k := key(tool, e.Name, vi)
				if prev, dup := covered[k]; dup {
					return nil, fmt.Errorf("overlay %s overlaps %s at %s", e, prev, tags[vi])
				}
				covered[k] = e
				for i := range vars {
					vars[i].isRegex = e.IsRegex
					vars[i].values = slices.Clone(e.Values)
					vars[i].valuesIgnoreCase = e.ValuesIgnoreCase
				}
			}
		}
	}
	var warnings []string
	for _, e := range o.Options {
		next := pos[e.To] + 1
		if next >= len(tags) {
			continue
		}
		for _, tool := range e.Tools {
			if _, ok := covered[key(tool, e.Name, next)]; ok || len(xs[next].options[tool][e.Name]) == 0 {
				continue
			}
			warnings = append(warnings, fmt.Sprintf(
				"overlay %s stops at %s, but %s --%s still exists in %s: check the evidence in that version and extend the range",
				e, e.To, tool, e.Name, tags[next]))
		}
	}
	return warnings, nil
}
