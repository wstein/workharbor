// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

func TestLocalLsJSONMatchesSchema(t *testing.T) {
	w := localdocker.Workspace{ID: strings.Repeat("a", 64), Name: "sample", Role: "primary", Image: "ubuntu", State: "running", Status: "Up 1 hour", WorkspaceFolder: "/host/project", Labels: map[string]string{"workharbor.workspace": "x"}, ProtectedVolumes: []string{}}
	makeEnvelope := func(workspaces []localdocker.Workspace) any {
		raw, err := json.Marshal(map[string]any{"schema_version": 1, "command": "workspace.ls", "ok": true, "exit_code": 0, "result": map[string]any{"workspaces": workspaces}})
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	schema := compileSchema(t)
	if err := schema.Validate(makeEnvelope([]localdocker.Workspace{w})); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(makeEnvelope([]localdocker.Workspace{})); err != nil {
		t.Fatal(err)
	}
	w.ID = "short"
	if err := schema.Validate(makeEnvelope([]localdocker.Workspace{w})); err == nil {
		t.Fatal("schema accepts abbreviated JSON IDs")
	}
}
