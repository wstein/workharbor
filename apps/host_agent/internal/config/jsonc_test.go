// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"context"
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
