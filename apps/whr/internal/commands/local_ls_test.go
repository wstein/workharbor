// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
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

func localHandlers(t *testing.T, calls *int) map[string]commands.Handler {
	t.Helper()
	h := baseHandlers()
	h["workspace.ls"] = commands.LocalLsHandler(func(ctx context.Context) ([]localdocker.Workspace, error) {
		*calls++
		return []localdocker.Workspace{{ID: strings.Repeat("a", 64), Name: "sample\x1b[31m", Role: "primary", State: "running", Labels: map[string]string{"workharbor.workspace": "x"}, ProtectedVolumes: []string{}}}, nil
	})
	return h
}
func TestLocalLsCanonicalAndAlias(t *testing.T) {
	calls := 0
	h := localHandlers(t, &calls)
	var previous string
	for _, args := range [][]string{{"workspace", "ls", "--local", "--json"}, {"ws", "ls", "--local", "--json"}, {"ls", "--local", "--json"}, {"--json", "ls", "--local"}} {
		got := run(t, h, args...)
		if got.code != 0 {
			t.Fatalf("%v: %+v", args, got)
		}
		if previous != "" && got.stdout != previous {
			t.Fatal("aliases differ")
		}
		previous = got.stdout
		if err := compileSchema(t).Validate(decodeEnvelope(t, got.stdout)); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
	got := run(t, h, "ls", "--local")
	if got.code != 0 || strings.Contains(got.stdout, "\x1b") || !strings.Contains(got.stdout, "<U+001B>") {
		t.Fatalf("%+v", got)
	}
}
func TestWorkspaceLsWithoutLocalStillRefuses(t *testing.T) {
	calls := 0
	h := localHandlers(t, &calls)
	for _, args := range [][]string{{"ls"}, {"workspace", "ls", "--json"}, {"ls", "--role", "primary"}} {
		got := run(t, h, args...)
		if got.code != 64 || !strings.Contains(got.stdout+got.err, "owner_api") {
			t.Fatalf("%v: %+v", args, got)
		}
	}
	if calls != 0 {
		t.Fatal("remote listing reached local Docker")
	}
}
func TestLocalLsNeverMutatesOrContactsControlPlane(t *testing.T) {
	calls := 0
	h := localHandlers(t, &calls)
	for _, args := range [][]string{{"ls", "--local", "--role", "running"}, {"ls", "--local", "--label", "=bad"}, {"ls", "--local", "--label="}, {"ls", "--local", "extra"}, {"ls", "--local", "--state", "running"}, {"rm", "--local"}, {"agent", "ls", "--local"}} {
		if got := run(t, h, args...); got.code != 64 {
			t.Fatalf("%v: %+v", args, got)
		}
	}
	if calls != 0 {
		t.Fatal("invalid invocation reached discovery")
	}
	got := run(t, h, "ls", "--local", "--role", "temp", "--json")
	if got.code != 0 || calls != 1 {
		t.Fatalf("%+v calls=%d", got, calls)
	}
	if !strings.Contains(got.stdout, `"workspaces":[]`) {
		t.Fatalf("filter ignored: %s", got.stdout)
	}
}
