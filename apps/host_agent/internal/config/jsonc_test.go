// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func jsoncTool(t *testing.T) JSONC {
	t.Helper()
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	dir, e := filepath.Abs("../../tools/devcontainer")
	if e != nil {
		t.Fatal(e)
	}
	return JSONC{NodePath: node, CompanionDir: dir}
}
func TestJSONCCommentsTrailingCommaAndScalarStrings(t *testing.T) {
	parser := jsoncTool(t)
	v, e := parser.Parse(context.Background(), []byte("{/*comment*/\"name\":\"\\ud800\\udc00\",}"), false)
	if e != nil {
		t.Fatal(e)
	}
	m, ok := v.(map[string]any)
	if !ok || m["name"] != "\U00010000" {
		t.Fatalf("value=%#v", v)
	}
}
func TestJSONCRejectsDeepEscapedDuplicatesAndMalformedScalars(t *testing.T) {
	parser := jsoncTool(t)
	for _, raw := range [][]byte{[]byte(`{"a":1,"\u0061":2}`), []byte(`{"x":[{"a":1,"a":2}]}`), []byte(`{"x":"\ud800"}`), []byte(`{"\udc00":1}`), {255}, []byte(`{"x":`), []byte(`{"x":1}garbage`)} {
		_, e := parser.Parse(context.Background(), raw, false)
		code(t, e, "invalid_configuration")
	}
}
func TestStrictMetadataJSONRefusesCommentsAndTrailingComma(t *testing.T) {
	parser := jsoncTool(t)
	for _, raw := range []string{`{/*comment*/"x":1}`, `{"x":1,}`} {
		_, e := parser.Parse(context.Background(), []byte(raw), true)
		code(t, e, "invalid_configuration")
	}
	if _, e := parser.Parse(context.Background(), []byte(`[{"remoteUser":"vscode"}]`), true); e != nil {
		t.Fatal(e)
	}
}

func TestJSONCPreservesExactNumericLexemes(t *testing.T) {
	parser := jsoncTool(t)
	for _, strict := range []bool{false, true} {
		raw := []byte(`{"n":9007199254740993,"nested":[1.0000000000000001,{"n":1e2}],"negative":-0}`)
		value, e := parser.Parse(context.Background(), raw, strict)
		if e != nil {
			t.Fatal(e)
		}
		object := value.(map[string]any)
		nested := object["nested"].([]any)
		if object["n"] != json.Number("9007199254740993") || nested[0] != json.Number("1.0000000000000001") || nested[1].(map[string]any)["n"] != json.Number("1e2") || object["negative"] != json.Number("-0") {
			t.Fatalf("numeric lexemes changed: %#v", value)
		}
	}
}
func TestResolverRejectsNoncanonicalSchemaIntegerLexemes(t *testing.T) {
	for _, number := range []string{"1.0000000000000001", "1e0", "9007199254740993", "18446744073709551616"} {
		t.Run(number, func(t *testing.T) {
			r, d := resolverFixture(t, func(root string) {
				raw := `{"schema_version":` + number + `,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`
				if e := os.WriteFile(filepath.Join(root, "workharbor.json"), []byte(raw), 0600); e != nil {
					t.Fatal(e)
				}
			})
			result, e := r.Resolve(context.Background(), d)
			if result != nil {
				result.Close()
			}
			code(t, e, "invalid_configuration")
		})
	}
}
