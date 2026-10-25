// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
)

func stageCLIResult() localdocker.LifecycleResult {
	fingerprint := "sha256:" + strings.Repeat("a", 64)
	name := "primary\x1b[31m\nforged"
	id := strings.Repeat("b", 64)
	home := "whr-stage-home-" + strings.Repeat("c", 32)
	return localdocker.LifecycleResult{Status: "preview", Phase: "planned", Fingerprint: &fingerprint, Primary: &localdocker.StageResource{Kind: "container", Name: &name, ID: &id, DaemonID: "daemon"}, Home: &localdocker.StageResource{Kind: "volume", Name: &home, DaemonID: "daemon"}, Effects: []localdocker.StageEffect{}, Recovery: []localdocker.StageRecoveryStep{}, Repositories: []localdocker.StageRepository{}, ResultRefs: []localdocker.StageResultRef{}}
}

func TestLocalStageCommandContractAndAlias(t *testing.T) {
	var entry *commands.Entry
	for _, e := range commands.Table() {
		if e.ID == "workspace.stage" {
			copy := e
			entry = &copy
		}
	}
	want := []string{"--local", "--source", "--smoke", "--dry-run", "--trust", "--yes", "--json"}
	if entry == nil || !reflect.DeepEqual(entry.Flags, want) || len(entry.Short) != 0 || entry.Needs != "none" || entry.Writes != "local" {
		t.Fatalf("stage command entry: %+v", entry)
	}
	if commands.DefaultHandlers()["workspace.stage"] == nil {
		t.Fatal("production stage handler absent")
	}
	for _, group := range []string{"workspace", "ws"} {
		handlers := baseHandlers()
		called := false
		handlers["workspace.stage"] = func(context.Context, commands.Call) (commands.Result, error) {
			called = true
			return commands.Result{Value: stageCLIResult()}, nil
		}
		got := run(t, handlers, group, "stage", "--local", "--source", "/source", "--dry-run", "primary", "--json")
		if got.code != 0 || !called {
			t.Fatalf("dispatch: %+v called=%t", got, called)
		}
		envelope := decodeEnvelope(t, got.stdout)
		if envelope["command"] != "workspace.stage" {
			t.Fatalf("canonical command: %v", envelope)
		}
	}
}
