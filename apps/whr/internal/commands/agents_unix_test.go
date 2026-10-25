//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package commands_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInitPublicAgentsUnsafeTargets(t *testing.T) {
	for _, kind := range []string{"symlink", "FIFO"} {
		for _, edit := range []bool{false, true} {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/edit=%v/dry-run=%v", kind, edit, dryRun), func(t *testing.T) {
					dir := initFixture(t)
					target := filepath.Join(dir, "AGENTS.md")
					outside := filepath.Join(t.TempDir(), "owner-policy")
					original := []byte("owner-only outside policy sentinel\n")
					if err := os.WriteFile(outside, original, 0600); err != nil {
						t.Fatal(err)
					}
					stamp := time.Unix(1700000000, 0)
					if err := os.Chtimes(outside, stamp, stamp); err != nil {
						t.Fatal(err)
					}
					if kind == "symlink" {
						if err := os.Symlink(outside, target); err != nil {
							t.Fatal(err)
						}
					} else if err := unix.Mkfifo(target, 0600); err != nil {
						t.Fatal(err)
					}
					before, err := os.Lstat(target)
					if err != nil {
						t.Fatal(err)
					}
					outsideBefore, err := os.Stat(outside)
					if err != nil {
						t.Fatal(err)
					}
					dirBefore, err := os.Stat(dir)
					if err != nil {
						t.Fatal(err)
					}
					args := []string{"init", dir, "--non-interactive", "--yes", "--json"}
					if edit {
						args = append(args, "--overwrite=AGENTS.md")
					}
					if dryRun {
						args = append(args, "--dry-run")
					}
					got := run(t, nil, args...)
					if got.code != 68 {
						t.Fatalf("unsafe target must return 68: %d / %s", got.code, got.err)
					}
					doc, err := jsonUnmarshal(got.stdout)
					if err != nil {
						t.Fatal(err)
					}
					if err := compileSchema(t).Validate(doc); err != nil {
						t.Fatalf("invalid unsafe-target refusal: %v", err)
					}
					envelope := decodeEnvelope(t, got.stdout)
					body := envelope["error"].(map[string]any)
					const reason = "init target is unsafe, unreadable or changed while it was inspected"
					if got.code != 68 || envelope["ok"] != false || envelope["exit_code"] != float64(68) || body["code"] != "conflict" || body["message"] != reason || !strings.Contains(got.err, reason) {
						t.Fatalf("wrong unsafe-target refusal: %v / %s", envelope, got.err)
					}
					result := envelope["result"].(map[string]any)
					if result["dry_run"] != dryRun {
						t.Fatalf("wrong unsafe-target dry_run: %v", result["dry_run"])
					}
					for _, key := range []string{"actions", "agents_block_warnings"} {
						if values, ok := result[key].([]any); !ok || len(values) != 0 {
							t.Fatalf("unsafe-target %s must be empty: %v", key, result[key])
						}
					}
					after, err := os.Lstat(target) // Never open or read the FIFO.
					if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
						t.Fatalf("unsafe target changed: %v", err)
					}
					if kind == "symlink" {
						if link, err := os.Readlink(target); err != nil || link != outside {
							t.Fatalf("link target changed: %q / %v", link, err)
						}
					}
					outsideAfter, err := os.Stat(outside)
					if err != nil || !os.SameFile(outsideBefore, outsideAfter) || outsideBefore.Mode() != outsideAfter.Mode() || !outsideBefore.ModTime().Equal(outsideAfter.ModTime()) {
						t.Fatalf("outside file metadata changed: %v", err)
					}
					if data, err := os.ReadFile(outside); err != nil || !bytes.Equal(data, original) {
						t.Fatalf("outside file bytes changed: %v", err)
					}
					if strings.Contains(got.stdout+got.err, strings.TrimSuffix(string(original), "\n")) {
						t.Fatal("outside file content was exposed")
					}
					dirAfter, err := os.Stat(dir)
					if err != nil || !os.SameFile(dirBefore, dirAfter) || !dirBefore.ModTime().Equal(dirAfter.ModTime()) {
						t.Fatalf("refusal changed directory: %v", err)
					}
					if _, err := os.Stat(filepath.Join(dir, "workharbor.json")); !os.IsNotExist(err) {
						t.Fatalf("refusal wrote manifest: %v", err)
					}
				})
			}
		}
	}
}
