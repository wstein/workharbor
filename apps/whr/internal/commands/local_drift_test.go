// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"os"
	"path/filepath"
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

func TestBundleDriftRuntimeUnavailableKeepsSchemaReport(t *testing.T) {
	for _, failure := range []string{"endpoint", "container_ls"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "runtime-calls")
			script := fmt.Sprintf("#!/bin/sh\nif test \"$2 $3\" = 'container ls'; then printf 'ls\\n' >> '%s';exit 1;fi\nprintf 'forbidden\\n' >> '%s';exit 99\n", calls, calls)
			executable := filepath.Join(dir, "docker")
			if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ports := doctor.Ports{Getenv: func(key string) string {
				if key == "DOCKER_HOST" {
					return "unix:///fixture/drift.sock"
				}
				return ""
			}, DialUnix: func(context.Context, string) error {
				if failure == "endpoint" {
					return os.ErrNotExist
				}
				return nil
			}}
			handlers := baseHandlers()
			handlers["workspace.drift"] = commands.LocalDriftHandler(func(ctx context.Context, source, selector string) (localdocker.BundleDriftResult, error) {
				return (localdocker.Client{Executable: executable}).Drift(ctx, ports, source, selector)
			})
			got := run(t, handlers, "workspace", "drift", "--local", "--source", "/source", "primary", "--json")
			if got.code != 68 {
				t.Fatalf("runtime failure exit=%d stdout=%s stderr=%s", got.code, got.stdout, got.err)
			}
			envelope := decodeEnvelope(t, got.stdout)
			report, ok := envelope["result"].(map[string]any)
			if !ok || report["status"] != "cannot_determine" || report["reason"] != "runtime_unavailable" || report["container_id"] != nil {
				t.Fatalf("runtime failure lost report: %s", got.stdout)
			}
			if err := compileSchema(t).Validate(envelope); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(calls)
			if failure == "endpoint" {
				if !os.IsNotExist(err) {
					t.Fatalf("unavailable endpoint reached Docker: %q %v", raw, err)
				}
			} else if err != nil || string(raw) != "ls\n" {
				t.Fatalf("listing failure admitted additional calls: %q %v", raw, err)
			}
		})
	}
}
