// Package schematest is the test support for the hand-written JSON Schemas in
// schemas/ (issue #268). It holds a deliberately small validator and a
// recursive comparison of a schema with a Go struct. It supports only the
// keywords the schemas use and fails on any other, so a schema cannot carry a
// constraint that nothing checks. It is not linked into the product.
package schematest

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Schema is a parsed JSON Schema document.
type Schema = map[string]any

type object = Schema

// annotations are keywords that describe and never constrain.
var annotations = []string{"$schema", "$id", "title", "description", "$comment", "default", "examples", "$defs"}

// Load reads and parses a schema file.
func Load(t testing.TB, path string) Schema {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test input
	if err != nil {
		t.Fatal(err)
	}
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return s
}

// Validate returns what is wrong with the JSON document raw, one line per
// problem with its path, against the schema root.
func Validate(root Schema, raw []byte) []string {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []string{"not JSON: " + err.Error()}
	}
	var problems []string
	validate(root, root, doc, "$", &problems)
	return problems
}

func resolve(root, s object) object {
	ref, ok := s["$ref"].(string)
	if !ok {
		return s
	}
	name, found := strings.CutPrefix(ref, "#/$defs/")
	defs, _ := root["$defs"].(object)
	target, _ := defs[name].(object)
	if !found || target == nil {
		panic("schematest: unresolved $ref " + ref)
	}
	return target
}

func validate(root, s object, v any, path string, out *[]string) {
	s = resolve(root, s)
	bad := func(format string, args ...any) { *out = append(*out, path+": "+fmt.Sprintf(format, args...)) }
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if slices.Contains(annotations, k) {
			continue
		}
		switch k {
		case "type":
			if !typeMatches(s[k].(string), v) {
				bad("want %s, got %s", s[k], jsonType(v))
				return
			}
		case "enum":
			if !slices.ContainsFunc(s[k].([]any), func(e any) bool { return reflect.DeepEqual(e, v) }) {
				bad("%v is not one of %v", v, s[k])
			}
		case "minimum", "maximum":
			if n, ok := v.(float64); ok && (k == "minimum" && n < s[k].(float64) || k == "maximum" && n > s[k].(float64)) {
				bad("%v violates %s %v", n, k, s[k])
			}
		case "maxLength":
			if str, ok := v.(string); ok && float64(len(str)) > s[k].(float64) {
				bad("longer than %v", s[k])
			}
		case "pattern":
			if str, ok := v.(string); ok && !regexp.MustCompile(s[k].(string)).MatchString(str) {
				bad("%q does not match %s", str, s[k])
			}
		case "minItems":
			if a, ok := v.([]any); ok && float64(len(a)) < s[k].(float64) {
				bad("fewer than %v items", s[k])
			}
		case "items":
			if a, ok := v.([]any); ok {
				for i, e := range a {
					validate(root, s[k].(object), e, fmt.Sprintf("%s[%d]", path, i), out)
				}
			}
		case "anyOf":
			ok := false
			for _, alt := range s[k].([]any) {
				var sub []string
				validate(root, alt.(object), v, path, &sub)
				ok = ok || len(sub) == 0
			}
			if !ok {
				bad("matches none of the alternatives")
			}
		case "required":
			if m, isObj := v.(object); isObj {
				for _, name := range s[k].([]any) {
					if _, has := m[name.(string)]; !has {
						bad("missing required key %q", name)
					}
				}
			}
		case "additionalProperties":
			if m, isObj := v.(object); isObj && s[k] == false {
				props, _ := s["properties"].(object)
				for name := range m {
					if _, known := props[name]; !known {
						bad("unknown key %q", name)
					}
				}
			}
		case "properties":
			if m, isObj := v.(object); isObj {
				for name, sub := range s[k].(object) {
					if val, has := m[name]; has {
						validate(root, sub.(object), val, path+"."+name, out)
					}
				}
			}
		case "$ref":
		default:
			panic("schematest: unsupported keyword " + k)
		}
	}
}

func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == float64(int64(x)) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	}
	return "object"
}

func typeMatches(want string, v any) bool {
	got := jsonType(v)
	return got == want || want == "number" && got == "integer"
}

// CompareStruct fails t for every difference between the schema and the Go
// type: a key on one side only, another type, another optionality, or an object
// schema that does not forbid unknown keys. A field is optional in the schema
// exactly when its tag says omitempty or omitzero; extraOptional lists the
// other optional keys by path with the reason, and rawAny the paths of
// json.RawMessage fields, which only need an entry. It recurses through
// nested objects, arrays and pointers.
func CompareStruct(t testing.TB, root Schema, typ reflect.Type, extraOptional map[string]string, rawAny ...string) {
	t.Helper()
	compare(t, root, root, typ, "$", extraOptional, rawAny)
}

var rawMessage = reflect.TypeFor[json.RawMessage]()

func compare(t testing.TB, root, s object, typ reflect.Type, path string, extra map[string]string, raw []string) {
	t.Helper()
	s = resolve(root, s)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if slices.Contains(raw, path) && typ == rawMessage {
		return
	}
	var want string
	switch typ.Kind() { //nolint:exhaustive // the kinds the configuration uses
	case reflect.String:
		want = "string"
	case reflect.Bool:
		want = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		want = "integer"
	case reflect.Float32, reflect.Float64:
		want = "number"
	case reflect.Slice:
		want = "array"
	case reflect.Struct:
		want = "object"
	default:
		t.Errorf("%s: Go kind %s is not supported by the comparison", path, typ.Kind())
		return
	}
	if got, _ := s["type"].(string); got != want {
		t.Errorf("%s: the Go type %s is a JSON %s, the schema says %q", path, typ, want, got)
		return
	}
	switch want {
	case "array":
		items, ok := s["items"].(object)
		if !ok {
			t.Errorf("%s: array schema has no items", path)
			return
		}
		compare(t, root, items, typ.Elem(), path+"[]", extra, raw)
	case "object":
		compareObject(t, root, s, typ, path, extra, raw)
	}
}

func compareObject(t testing.TB, root, s object, typ reflect.Type, path string, extra map[string]string, raw []string) {
	t.Helper()
	if s["additionalProperties"] != false {
		t.Errorf("%s: the schema must set additionalProperties to false", path)
	}
	props, _ := s["properties"].(object)
	required := map[string]bool{}
	for _, name := range asStrings(s["required"]) {
		required[name] = true
	}
	seen := map[string]bool{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		seen[name] = true
		key := path + "." + name
		sub, ok := props[name].(object)
		if !ok {
			t.Errorf("%s: the Go struct %s has this key, the schema has no entry", key, typ)
			continue
		}
		optional := strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero")
		_, listed := extra[key]
		switch {
		case optional && required[name]:
			t.Errorf("%s: omitempty in Go but required in the schema", key)
		case !optional && !listed && !required[name]:
			t.Errorf("%s: always written in Go but optional in the schema (list it with a reason if the loader accepts it missing)", key)
		case listed && (optional || required[name]):
			t.Errorf("%s: listed as a deliberate exception but it is not one", key)
		}
		compare(t, root, sub, f.Type, key, extra, raw)
	}
	for name := range props {
		if !seen[name] {
			t.Errorf("%s.%s: the schema has this entry, the Go struct %s has no such key", path, name, typ)
		}
	}
	for name := range required {
		if _, ok := props[name]; !ok {
			t.Errorf("%s: required key %q has no property entry", path, name)
		}
	}
}

func asStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, e := range list {
		out = append(out, e.(string))
	}
	return out
}
