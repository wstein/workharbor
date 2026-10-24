// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"strings"
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

func TestBundleDriftJSONMatchesSchema(t *testing.T) {
	id := strings.Repeat("a", 64)
	schema := compileSchema(t)
	for _, status := range []string{"in_sync", "drift", "cannot_determine"} {
		h := baseHandlers()
		h["workspace.drift"] = commands.LocalDriftHandler(func(context.Context, string, string) (localdocker.BundleDriftResult, error) {
			r := localdocker.BundleDriftResult{ContainerID: &id, Status: status, Reason: "reference_match", Bundles: []localdocker.BundleDrift{}}
			if status != "in_sync" {
				r.Reason = "incomplete_evidence"
				e := exitcode.Errorf(exitcode.PathConflict, "bundle references: %s", status)
				e.Result = r
				return r, e
			}
			return r, nil
		})
		got := run(t, h, "workspace", "drift", "--local", "--source", "/source", "primary", "--json")
		want := 0
		if status != "in_sync" {
			want = 68
		}
		if got.code != want {
			t.Fatalf("%+v", got)
		}
		env := decodeEnvelope(t, got.stdout)
		r, ok := env["result"].(map[string]any)
		if !ok || r["status"] != status {
			t.Fatalf("nonzero report lost: %s", got.stdout)
		}
		if err := schema.Validate(env); err != nil {
			t.Fatal(err)
		}
		r["secret"] = "unexpected"
		if err := schema.Validate(env); err == nil {
			t.Fatal("schema accepts unexpected drift report field")
		}
	}
}
func TestBundleDriftRefusesInvalidInvocationBeforeIO(t *testing.T) {
	calls := 0
	h := baseHandlers()
	h["workspace.drift"] = commands.LocalDriftHandler(func(context.Context, string, string) (localdocker.BundleDriftResult, error) {
		calls++
		return localdocker.BundleDriftResult{}, nil
	})
	for _, args := range [][]string{{"workspace", "drift"}, {"workspace", "drift", "--source", "/source", "primary"}, {"workspace", "drift", "--local", "primary"}, {"workspace", "drift", "--local", "--source", "/source"}, {"workspace", "drift", "--local", "--source", "/source", "one", "two"}, {"workspace", "drift", "--local", "--source", "/source", "--yes", "primary"}} {
		got := run(t, h, args...)
		if got.code != 64 {
			t.Fatalf("%v %+v", args, got)
		}
	}
	if calls != 0 {
		t.Fatal("invalid invocation reached local I/O")
	}
}
func TestBundleDriftHumanEscapesUntrustedValues(t *testing.T) {
	bad := "bad\x1b\u202e\n"
	r := localdocker.BundleDriftResult{Status: "cannot_determine", Reason: "incomplete_evidence", Bundles: []localdocker.BundleDrift{{Bundle: bad, PinnedReference: &bad, MountedIdentity: &bad, Status: "cannot_determine", Reason: "mount_unavailable"}}}
	if human := r.Human(); strings.Contains(human, "\x1b") || strings.Contains(human, "\u202e") || !strings.Contains(human, "<U+001B>") {
		t.Fatal(human)
	}
	raw, err := json.Marshal(r)
	if err != nil || strings.Contains(string(raw), "\x1b") {
		t.Fatalf("%s %v", raw, err)
	}
}
