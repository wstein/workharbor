// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func pathAcceptanceWrite(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func pathAcceptanceStat(t *testing.T, name string) fs.FileInfo {
	t.Helper()
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func pathAcceptanceUnchanged(t *testing.T, name string, before fs.FileInfo, data []byte) {
	t.Helper()
	after := pathAcceptanceStat(t, name)
	raw, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(raw, data) || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("retained file changed: %s, %v", name, err)
	}
}

func pathAcceptanceAction(t *testing.T, preview Preview, name string) Action {
	t.Helper()
	var found []Action
	for _, action := range preview.Actions {
		if action.Path == name {
			found = append(found, action)
		}
	}
	if len(found) != 1 {
		t.Fatalf("actions for %s = %+v, want exactly one", name, found)
	}
	return found[0]
}

func TestInitSymlinkTarget(t *testing.T) {
	for _, name := range []string{"workharbor.json", ".devcontainer/devcontainer.json"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			outside := filepath.Join(t.TempDir(), "foreign-target")
			original := []byte("foreign bytes must survive overwrite consent\n")
			pathAcceptanceWrite(t, outside, original)
			outsideInfo := pathAcceptanceStat(t, outside)
			target := filepath.Join(dir, name)
			if name != "workharbor.json" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			linkInfo := pathAcceptanceStat(t, target)
			opts := Options{NonInteractive: true, Yes: true, Overwrite: []string{name}}
			if _, err := DiscoverWithOptions(context.Background(), dir, opts); int(exitcode.FromError(err)) != 68 {
				t.Fatalf("overwrite consent admitted symlink target: %v", err)
			}
			linkAfter := pathAcceptanceStat(t, target)
			if linkAfter.Mode()&fs.ModeSymlink == 0 || !os.SameFile(linkInfo, linkAfter) {
				t.Fatal("refusal replaced the symlink")
			}
			if destination, err := os.Readlink(target); err != nil || destination != outside {
				t.Fatalf("refusal changed link destination: %q, %v", destination, err)
			}
			pathAcceptanceUnchanged(t, outside, outsideInfo, original)
		})
	}
}

func TestInitExistingDirectoriesReused(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			dir := fixture(t)
			for _, name := range []string{"projects", ".claude"} {
				if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			old := map[string]fs.FileInfo{}
			for _, name := range []string{".devcontainer", "projects", ".claude"} {
				old[name] = pathAcceptanceStat(t, filepath.Join(dir, name))
				pathAcceptanceWrite(t, filepath.Join(dir, name, "keep-owner-file"), []byte(name))
			}
			p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true})
			for name := range old {
				if action := pathAcceptanceAction(t, p.Preview(), name); action.Action != "reuse" || action.Applied {
					t.Fatalf("existing directory not proposed as reuse: %+v", action)
				}
			}
			injected := false
			result, err := p.apply(context.Background(), func(at string) error {
				if rollback && at == "after:.claude/settings.json" {
					injected = true
					return syscall.ENOSPC
				}
				return nil
			})
			if rollback {
				if !injected || int(exitcode.FromError(err)) != 74 || len(result.Remaining) != 0 {
					t.Fatalf("expected complete rollback after injected write failure: %+v, %v", result, err)
				}
				for _, name := range []string{"workharbor.json", "projects/.gitkeep", "AGENTS.md", ".claude/settings.json", ".whr-init-tmp-journal"} {
					if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
						t.Fatalf("rollback retained new path %s: %v", name, err)
					}
				}
			} else if err != nil || !result.Validation.Valid {
				t.Fatalf("reuse apply failed: %+v, %v", result, err)
			}
			for name, before := range old {
				after := pathAcceptanceStat(t, filepath.Join(dir, name))
				if !after.IsDir() || !os.SameFile(before, after) || before.Mode() != after.Mode() {
					t.Fatalf("apply or rollback replaced existing directory %s", name)
				}
				if raw, err := os.ReadFile(filepath.Join(dir, name, "keep-owner-file")); err != nil || string(raw) != name {
					t.Fatalf("apply or rollback changed owner content in %s: %q, %v", name, raw, err)
				}
			}
		})
	}
}

func TestInitIdenticalFileSkipExisting(t *testing.T) {
	for _, permission := range []bool{false, true} {
		t.Run(fmt.Sprintf("permission=%t", permission), func(t *testing.T) {
			dir := fixture(t)
			if _, err := applyProposal(t, dir, Options{NonInteractive: true, Yes: true}).Apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "workharbor.json")
			raw, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			before := pathAcceptanceStat(t, target)
			shell := "fish"
			opts := Options{NonInteractive: true, Yes: true, HumanShell: &shell}
			if permission {
				opts.Overwrite = []string{"workharbor.json"}
			}
			p := applyProposal(t, dir, opts)
			if !bytes.Equal(p.Files()["workharbor.json"], raw) {
				t.Fatal("explicit identical setting changed proposed bytes")
			}
			if action := pathAcceptanceAction(t, p.Preview(), "workharbor.json"); action.Action != "skip_existing" || action.Applied || action.Diff != "" {
				t.Fatalf("identical edit must skip without a diff: %+v", action)
			}
			result, err := p.Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range result.Actions {
				if action.Applied {
					t.Fatalf("identical rerun applied an action: %+v", action)
				}
			}
			if action := pathAcceptanceAction(t, result, "workharbor.json"); action.Action != "skip_existing" || action.Diff != "" {
				t.Fatalf("identical result changed action: %+v", action)
			}
			pathAcceptanceUnchanged(t, target, before, raw)
		})
	}
}

func TestInitBundlesLeftUnchanged(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(fmt.Sprintf("requested-edit=%t", edit), func(t *testing.T) {
			dir := fixture(t)
			raw := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[\"owner-bundle\"]}\n")
			target := filepath.Join(dir, "workharbor.json")
			pathAcceptanceWrite(t, target, raw)
			before := pathAcceptanceStat(t, target)
			opts := Options{NonInteractive: true, Yes: true}
			if edit {
				name := "replacement"
				opts.Name = &name
				opts.Overwrite = []string{"workharbor.json"}
			}
			s, err := DiscoverWithOptions(context.Background(), dir, opts)
			if err == nil {
				_, err = Build(s, opts)
			}
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("unsupported existing bundle must refuse, never erase it: %v", err)
			}
			pathAcceptanceUnchanged(t, target, before, raw)
			for _, name := range []string{"projects", "AGENTS.md", ".claude", ".whr-init-tmp-journal"} {
				if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("bundle refusal created %s: %v", name, err)
				}
			}
		})
	}
}

func TestInitOverwriteTempFilePrefixDetected(t *testing.T) {
	for _, tc := range []struct {
		name         string
		afterPreview bool
	}{
		{".whr-init-tmp-owner-recovery", false},
		{".whr-init-tmp-owner-recovery", true},
		{".devcontainer/.whr-init-tmp-owner-recovery", false},
	} {
		name, afterPreview := tc.name, tc.afterPreview
		t.Run(fmt.Sprintf("%s/after-preview=%t", name, afterPreview), func(t *testing.T) {
			dir := fixture(t)
			raw := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[]}\n")
			target := filepath.Join(dir, "workharbor.json")
			pathAcceptanceWrite(t, target, raw)
			before := pathAcceptanceStat(t, target)
			newName := "replacement"
			opts := Options{NonInteractive: true, Yes: true, Name: &newName, Overwrite: []string{"workharbor.json"}}
			var p *Proposal
			if afterPreview {
				p = applyProposal(t, dir, opts)
			}
			orphan := filepath.Join(dir, name)
			orphanBytes := []byte("owned recovery bytes\n")
			pathAcceptanceWrite(t, orphan, orphanBytes)
			orphanInfo := pathAcceptanceStat(t, orphan)
			var err error
			if afterPreview {
				_, err = p.Apply(context.Background())
			} else {
				_, err = DiscoverWithOptions(context.Background(), dir, opts)
			}
			if int(exitcode.FromError(err)) != 68 || !strings.Contains(err.Error(), ".whr-init-tmp-") {
				t.Fatalf("orphan did not block consented replacement: %v", err)
			}
			pathAcceptanceUnchanged(t, target, before, raw)
			pathAcceptanceUnchanged(t, orphan, orphanInfo, orphanBytes)
		})
	}
}
