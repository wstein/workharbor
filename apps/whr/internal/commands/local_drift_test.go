// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

func TestBundleDriftCommandRegistered(t *testing.T) {
	calls := 0
	h := baseHandlers()
	h["workspace.drift"] = func(context.Context, commands.Call) (commands.Result, error) {
		calls++
		return commands.Result{Value: map[string]any{"status": "in_sync"}}, nil
	}
	got := run(t, h, "workspace", "drift", "--local", "--source", "/source", "primary", "--json")
	if got.code != 0 || calls != 1 {
		t.Fatalf("drift did not reach its registered handler: %+v calls=%d", got, calls)
	}
}
