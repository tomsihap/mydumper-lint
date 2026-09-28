package rules

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tomsihap/mydumper-lint/internal/diag"
	"github.com/tomsihap/mydumper-lint/internal/model"
)

// Conventions are a team's rules for its configuration files (design §9.2,
// MDL901-MDL905). Templates may use {group} placeholders, filled from the
// named groups of FilenamePattern.
type Conventions struct {
	FilenamePattern string              // Go regular expression on the base name, with named groups
	Values          map[string]string   // "group.key" -> value template (a regular expression)
	TableSchema     string              // template of the database of table sections
	Required        map[string][]string // group -> keys that must be present
	Forbidden       map[string][]string // group ("*" for every group) -> keys; "groups" -> group names
}

// captures matches the file name against the pattern and returns the named
// groups, or ok false when the pattern does not match (or is invalid).
func (c *Conventions) captures(path string) (map[string]string, bool) {
	if c.FilenamePattern == "" {
		return map[string]string{}, true
	}
	re, err := regexp.Compile(c.FilenamePattern)
	if err != nil {
		return nil, false
	}
	m := re.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return nil, false
	}
	out := map[string]string{}
	for i, name := range re.SubexpNames() {
		if name != "" {
			out[name] = m[i]
		}
	}
	return out, true
}

var placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// fill substitutes {name} placeholders; quoted escapes the values for use
// in a regular expression. Unknown placeholders are left as they are.
func fill(template string, caps map[string]string, quoted bool) string {
	return placeholder.ReplaceAllStringFunc(template, func(m string) string {
		v, ok := caps[m[1:len(m)-1]]
		if !ok {
			return m
		}
		if quoted {
			return regexp.QuoteMeta(v)
		}
		return v
	})
}

func init() {
	convention := func(id, name, summary, why string, check func(p *Pass, c *Conventions, caps map[string]string)) {
		register(&Rule{
			Meta: Meta{
				ID: id, Name: name, Family: FamilyConventions, Severity: diag.Error,
				Summary: summary, Why: why + " The rule does nothing unless the `conventions` section of " +
					"the configuration sets it up.",
			},
			Check: func(p *Pass) {
				if p.Conventions == nil {
					return
				}
				caps, ok := p.Conventions.captures(p.File.Path)
				if !ok && id != "MDL901" {
					caps = map[string]string{}
				}
				check(p, p.Conventions, caps)
			},
		})
	}
	convention("MDL901", "filename-pattern", "The file name does not follow the naming convention.",
		"A naming convention keeps each configuration tied to what it dumps.",
		func(p *Pass, c *Conventions, _ map[string]string) {
			if c.FilenamePattern == "" {
				return
			}
			if _, ok := c.captures(p.File.Path); !ok {
				p.Report(diag.Diagnostic{
					Span:    diag.Span{},
					Message: fmt.Sprintf("the file name %q does not match %s", filepath.Base(p.File.Path), c.FilenamePattern),
				})
			}
		})
	convention("MDL902", "value-template", "A value does not match its template.",
		"Templates catch a file copied for another database whose `outputdir` or `regex` still "+
			"points at the first one: two dumps would overwrite each other.",
		func(p *Pass, c *Conventions, caps map[string]string) {
			keys := make([]string, 0, len(c.Values))
			for k := range c.Values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, gk := range keys {
				group, key, ok := strings.Cut(gk, ".")
				if !ok {
					continue
				}
				re, err := regexp.Compile(fill(c.Values[gk], caps, true))
				if err != nil {
					continue
				}
				for _, e := range p.KF.Entries {
					if p.KF.Groups[e.Group].Name == group && e.Key == key && !re.MatchString(e.Value) {
						p.Report(diag.Diagnostic{
							Span:    e.ValueSpan,
							Message: fmt.Sprintf("%s.%s is %s, which does not match %s", group, key, quote([]byte(e.Value)), re),
						})
					}
				}
			}
		})
	convention("MDL903", "table-schema-template", "A table section names another database than the template.",
		"A table section copied from another configuration keeps masking or filtering a database "+
			"this file does not dump.",
		func(p *Pass, c *Conventions, caps map[string]string) {
			if c.TableSchema == "" {
				return
			}
			want := fill(c.TableSchema, caps, false)
			for _, g := range validHeaders(p) {
				if !model.IsTableGroup(g.Name) {
					continue
				}
				db := strings.Trim(strings.SplitN(g.Name, "`.`", 2)[0], "`")
				if db != "" && db != want {
					p.Report(diag.Diagnostic{
						Span:    g.NameSpan,
						Message: fmt.Sprintf("the table section is in database %q, not %q", db, want),
					})
				}
			}
		})
	convention("MDL904", "required-key", "A key the conventions require is missing.",
		"Required keys make every configuration state, for instance, where its dump goes.",
		func(p *Pass, c *Conventions, _ map[string]string) {
			groups := make([]string, 0, len(c.Required))
			for g := range c.Required {
				groups = append(groups, g)
			}
			sort.Strings(groups)
			for _, group := range groups {
				have := map[string]bool{}
				header := diag.Span{}
				found := false
				for gi, g := range p.KF.Groups {
					if g.Name != group || !g.Valid {
						continue
					}
					if !found {
						header, found = g.NameSpan, true
					}
					for _, e := range p.KF.Entries {
						if e.Group == gi {
							have[e.Key] = true
						}
					}
				}
				for _, key := range c.Required[group] {
					if have[key] {
						continue
					}
					msg := fmt.Sprintf("[%s] must set %s", group, key)
					if !found {
						msg = fmt.Sprintf("the file must have a [%s] section setting %s", group, key)
					}
					p.Report(diag.Diagnostic{Span: header, Message: msg})
				}
			}
		})
	convention("MDL905", "forbidden-key", "A key or group the conventions forbid is present.",
		"Forbidden keys keep secrets or dangerous settings out of versioned files.",
		func(p *Pass, c *Conventions, _ map[string]string) {
			for _, g := range validHeaders(p) {
				for _, name := range c.Forbidden["groups"] {
					if g.Name == name {
						p.Report(diag.Diagnostic{Span: g.NameSpan, Message: fmt.Sprintf("the [%s] group is forbidden by the conventions", g.Name)})
					}
				}
			}
			for _, e := range p.KF.Entries {
				group := p.KF.Groups[e.Group].Name
				for _, k := range append(append([]string(nil), c.Forbidden["*"]...), c.Forbidden[group]...) {
					if e.Key == k && group != "groups" {
						p.Report(diag.Diagnostic{Span: e.KeySpan, Message: fmt.Sprintf("%s is forbidden in [%s] by the conventions", e.Key, group)})
					}
				}
			}
		})
}
