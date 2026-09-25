package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Schema returns the JSON Schema (draft 2020-12) of the configuration file,
// derived from the Go types so that it cannot drift from the decoder.
func Schema() ([]byte, error) {
	s := typeSchema(reflect.TypeFor[Config]())
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["$id"] = "https://github.com/tomsihap/mydumper-lint/blob/main/schemas/config.v1.json"
	s["title"] = "mydumper-lint configuration (.mydumper-lint.yaml)"
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func typeSchema(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeFor[Toggle]() {
		return map[string]any{"oneOf": []any{
			map[string]any{"const": "off"},
			typeSchema(reflect.TypeFor[Conventions]()),
		}}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": typeSchema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": typeSchema(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if key == "" || key == "-" {
				continue
			}
			p := typeSchema(f.Type)
			if d := f.Tag.Get("desc"); d != "" {
				p["description"] = d
			}
			if e := f.Tag.Get("enum"); e != "" {
				vals := strings.Split(e, ",")
				switch p["type"] {
				case "object":
					p["additionalProperties"] = map[string]any{"enum": vals}
				default:
					p["enum"] = vals
				}
			}
			props[key] = p
		}
		return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	}
	return map[string]any{}
}
