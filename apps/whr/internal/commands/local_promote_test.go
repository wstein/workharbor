// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
)

func TestLocalPromoteRegistryDispatch(t *testing.T) {
	found := false
	for _, e := range commands.Table() {
		if e.ID == "workspace.promote" {
			found = true
			if len(e.Short) != 0 {
				t.Fatal("promote must have no bare alias")
			}
		}
	}
	if !found || commands.DefaultHandlers()["workspace.promote"] == nil {
		t.Fatal("promote registry/default handler absent")
	}
	for _, group := range []string{"workspace", "ws"} {
		h := baseHandlers()
		called := false
		h["workspace.promote"] = func(context.Context, commands.Call) (commands.Result, error) {
			called = true
			return commands.Result{Value: stageCLIResult()}, nil
		}
		g := run(t, h, group, "promote", "--local", "--dry-run", "primary", "--json")
		if g.code != 0 || !called {
			t.Fatalf("promote dispatch: %+v", g)
		}
		if decodeEnvelope(t, g.stdout)["command"] != "workspace.promote" {
			t.Fatal(g.stdout)
		}
	}
}
