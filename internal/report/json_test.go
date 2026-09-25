package report

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tomsihap/mydumper-lint/internal/diag"
)

// schemaChecker validates a decoded JSON value against the subset of JSON
// Schema that schemas/output.v1.json uses: $ref to $defs, type, const, enum,
// pattern, minimum, required, properties, items and anyOf. It is stricter
// than the schema: a property the schema does not declare is reported, so
// the schema documents every field the format writes.
type schemaChecker struct {
	defs map[string]any
	errs []string
}

func (c *schemaChecker) fail(path, format string, args ...any) {
	c.errs = append(c.errs, path+": "+fmt.Sprintf(format, args...))
}

func (c *schemaChecker) check(path string, schema map[string]any, v any) {
	if ref, ok := schema["$ref"].(string); ok {
		def, ok := c.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if !ok {
			c.fail(path, "unresolved $ref %q", ref)
			return
		}
		c.check(path, def, v) // the schema never puts keywords next to $ref
		return
	}
	if alts, ok := schema["anyOf"].([]any); ok && !slices.ContainsFunc(alts, func(alt any) bool {
		sub := &schemaChecker{defs: c.defs}
		sub.check(path, alt.(map[string]any), v)
		return len(sub.errs) == 0
	}) {
		c.fail(path, "matches no alternative of anyOf")
	}
	if t, ok := schema["type"].(string); ok && !hasJSONType(v, t) {
		c.fail(path, "%v is not of type %s", v, t)
		return
	}
	if cv, ok := schema["const"]; ok && !reflect.DeepEqual(cv, v) {
		c.fail(path, "%v is not %v", v, cv)
	}
	if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, v) {
		c.fail(path, "%v is not one of %v", v, enum)
	}
	if p, ok := schema["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(v.(string)) {
		c.fail(path, "%q does not match %s", v, p)
	}
	if m, ok := schema["minimum"].(float64); ok && v.(float64) < m {
		c.fail(path, "%v is below %v", v, m)
	}
	switch v := v.(type) {
	case map[string]any:
		required, _ := schema["required"].([]any)
		for _, k := range required {
			if _, ok := v[k.(string)]; !ok {
				c.fail(path, "missing required property %q", k)
			}
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			return // e.g. an anyOf node: its alternatives check the properties
		}
		for k, val := range v {
			ps, ok := props[k].(map[string]any)
			if !ok {
				c.fail(path, "property %q is not declared in the schema", k)
				continue
			}
			c.check(path+"."+k, ps, val)
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range v {
				c.check(fmt.Sprintf("%s[%d]", path, i), items, e)
			}
		}
	}
}

func hasJSONType(v any, t string) bool {
	switch v := v.(type) {
	case nil:
		return t == "null"
	case bool:
		return t == "boolean"
	case string:
		return t == "string"
	case float64:
		return t == "number" || (t == "integer" && v == float64(int64(v)))
	case []any:
		return t == "array"
	case map[string]any:
		return t == "object"
	}
	return false
}

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../schemas/output.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("schemas/output.v1.json: %v", err)
	}
	return schema
}

func decodeJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, s)
	}
	return v
}

func TestJSONMatchesSchema(t *testing.T) {
	schema := loadSchema(t)
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v, want draft 2020-12", schema["$schema"])
	}
	defs := schema["$defs"].(map[string]any)
	for _, sc := range scenarios() {
		c := &schemaChecker{defs: defs}
		c.check("$", schema, decodeJSON(t, render(t, "json", sc.results, testOptions())))
		for _, e := range c.errs {
			t.Errorf("%s: %s", sc.name, e)
		}
	}
}

// TestSchemaCheckerRejects makes sure the checker is not vacuous.
func TestSchemaCheckerRejects(t *testing.T) {
	schema := loadSchema(t)
	bad := map[string]string{
		"missing key":   `{"version":1,"tool":{"name":"mydumper-lint","version":""},"files":[]}`,
		"wrong const":   `{"version":2,"tool":{"name":"mydumper-lint","version":""},"files":[],"summary":{}}`,
		"wrong type":    `{"version":1,"tool":{"name":"mydumper-lint","version":1},"files":{},"summary":{}}`,
		"undeclared":    `{"version":1,"tool":{"name":"mydumper-lint","version":""},"files":[],"summary":{},"extra":0}`,
		"null array":    `{"version":1,"tool":{"name":"mydumper-lint","version":""},"files":null,"summary":{}}`,
		"bad fix":       `{"version":1,"tool":{"name":"mydumper-lint","version":""},"summary":{},"files":[{"path":"","loadable":true,"mydumper_version":"","fixed":0,"diagnostics":[{"id":"MDL1","name":"","severity":"fatal","message":"","consequence":"","range":{"start":{"line":0,"col":1,"offset":0},"end":{"line":1,"col":1,"offset":0}},"related":[],"fix":{}}]}]}`,
		"unknown $ref":  `{"version":1,"tool":{"name":"mydumper-lint","version":""},"files":[],"summary":{}}`,
		"not an object": `[]`,
	}
	defs := schema["$defs"].(map[string]any)
	for name, doc := range bad {
		c := &schemaChecker{defs: defs}
		if name == "unknown $ref" {
			c.defs = map[string]any{}
		}
		c.check("$", schema, decodeJSON(t, doc))
		if len(c.errs) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestJSONArraysAreNeverNull checks that every array is written as [] when
// empty; the only null is an absent fix.
func TestJSONArraysAreNeverNull(t *testing.T) {
	r := FileResult{Path: "a.cnf", Source: []byte("k=v\n"), Diagnostics: []diag.Diagnostic{
		{RuleID: "MDL301", Severity: diag.Warning},
		{RuleID: "MDL302", Severity: diag.Error, Fix: &diag.Fix{Applicability: diag.Unsafe}},
	}}
	for _, results := range [][]FileResult{nil, {{Path: "b.cnf"}}, {r}} {
		out := render(t, "json", results, Options{})
		var walk func(path string, v any)
		walk = func(path string, v any) {
			switch v := v.(type) {
			case nil:
				if !strings.HasSuffix(path, ".fix") {
					t.Errorf("null at %s in\n%s", path, out)
				}
			case map[string]any:
				for k, e := range v {
					walk(path+"."+k, e)
				}
			case []any:
				for i, e := range v {
					walk(fmt.Sprintf("%s[%d]", path, i), e)
				}
			}
		}
		walk("$", decodeJSON(t, out))
	}
	out := render(t, "json", []FileResult{r}, Options{})
	for _, want := range []string{`"files": [`, `"related": []`, `"fix": null`, `"edits": []`, `"applicability": "unsafe"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	if out := render(t, "json", nil, Options{}); !strings.Contains(out, `"files": []`) {
		t.Errorf("no files: %s", out)
	}
}

func TestJSONDoesNotEscapeHTML(t *testing.T) {
	r := FileResult{Path: "a&b.cnf", Diagnostics: []diag.Diagnostic{{RuleID: "MDL508", Severity: diag.Warning, Message: "<file x> not found"}}}
	out := render(t, "json", []FileResult{r}, Options{})
	if !strings.Contains(out, `"message": "<file x> not found"`) || !strings.Contains(out, `"path": "a&b.cnf"`) {
		t.Errorf("HTML characters escaped:\n%s", out)
	}
}
