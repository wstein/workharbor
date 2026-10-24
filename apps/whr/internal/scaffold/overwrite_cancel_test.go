// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestInitOverwriteCancellationEveryStep(t *testing.T) {
	setup := func(t *testing.T) (string, *Proposal) {
		t.Helper()
		dir := fixture(t)
		old := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[]}\n")
		if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), old, 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, "projects"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "projects/owner.txt"), []byte("preserve project data\n"), 0600); err != nil {
			t.Fatal(err)
		}
		name := "changed"
		p := applyProposal(t, dir, Options{Name: &name, NonInteractive: true, Yes: true, Overwrite: []string{"workharbor.json"}})
		if bytes.Equal(p.Files()["workharbor.json"], old) {
			t.Fatal("fixture must propose different manifest bytes")
		}
		return dir, p
	}
	_, probe := setup(t)
	var events []string
	seen := map[string]bool{}
	if _, err := probe.apply(context.Background(), func(event string) error {
		if seen[event] {
			t.Fatalf("duplicate success-path event: %s", event)
		}
		seen[event] = true
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 28 {
		t.Fatalf("observed %d success-path events, want current 28-event matrix: %v", len(events), events)
	}
	for _, event := range []string{"journal:write", "write-temp:.whr-init-tmp-00000001", "sync-temp:.whr-init-tmp-00000001", "rename:workharbor.json", "after:workharbor.json", "before:validate"} {
		if !seen[event] {
			t.Fatalf("missing required overwrite event: %s", event)
		}
	}
	type entry struct {
		info fs.FileInfo
		data []byte
	}
	snapshot := func(t *testing.T, dir string) map[string]entry {
		t.Helper()
		out := map[string]entry{}
		err := filepath.WalkDir(dir, func(name string, item fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, name)
			if err != nil {
				return err
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			var data []byte
			if !info.IsDir() {
				data, err = os.ReadFile(name)
				if err != nil {
					return err
				}
			}
			out[filepath.ToSlash(rel)] = entry{info, data}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			dir, proposal := setup(t)
			before := snapshot(t, dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hit := false
			result, err := proposal.apply(ctx, func(at string) error {
				if at == event && !hit {
					hit = true
					cancel()
				}
				// Cancellation must be observed through the context, including
				// when this is a fault callback rather than a checkpoint.
				return nil
			})
			if !hit || exitcode.FromError(err) != exitcode.Interrupted {
				t.Fatalf("event reached=%t, cancellation must exit 72: %v", hit, err)
			}
			if len(result.Remaining) != 0 {
				t.Fatalf("successful rollback reported remaining paths: %v", result.Remaining)
			}
			after := snapshot(t, dir)
			if len(after) != len(before) {
				t.Fatalf("rollback changed inventory: before=%v after=%v", before, after)
			}
			for name, old := range before {
				current, found := after[name]
				if !found || old.info.Mode() != current.info.Mode() || !bytes.Equal(old.data, current.data) {
					t.Fatalf("rollback did not restore bytes/mode of %s", name)
				}
				if old.info.IsDir() || name != "workharbor.json" {
					if !os.SameFile(old.info, current.info) {
						t.Fatalf("rollback replaced pre-existing %s", name)
					}
					if !old.info.IsDir() && old.info.ModTime() != current.info.ModTime() {
						t.Fatalf("rollback changed pre-existing file time: %s", name)
					}
				}
			}
		})
	}
}
