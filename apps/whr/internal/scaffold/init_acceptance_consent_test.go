// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func consentString(value string) *string { return &value }

func consentFixture(t *testing.T, version int) (string, []byte) {
	t.Helper()
	dir := fixture(t)
	if version == 0 {
		return dir, nil
	}
	// Deliberately retain formatting that a canonical rewrite would change.
	raw := fmt.Appendf(nil, "{ \"schema_version\": %d, \"devcontainer_path\": \".devcontainer/devcontainer.json\", \"projects_path\": \"projects\", \"bundles\": []", version)
	if version == 2 {
		raw = append(raw, []byte(", \"name\": \"existing\", \"human_shell\": \"zsh\", \"console_mode\": \"herdr\", \"shortcuts\": \"off\"")...)
	}
	raw = append(raw, []byte(" }\n\n")...)
	if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, raw
}

func consentSnapshot(t *testing.T, dir string) Snapshot {
	t.Helper()
	s, err := Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func consentAssertUnchanged(t *testing.T, before Snapshot) {
	t.Helper()
	after := consentSnapshot(t, before.Directory)
	if len(before.Entries) != len(after.Entries) {
		t.Fatal("refusal or preview changed the number of init targets")
	}
	for name, old := range before.Entries {
		now, ok := after.Entries[name]
		if !ok || !os.SameFile(old.Info, now.Info) || old.Mode != now.Mode || !old.Info.ModTime().Equal(now.Info.ModTime()) || !bytes.Equal(old.Data, now.Data) {
			t.Fatalf("refusal or preview changed %s", name)
		}
	}
}

func consentManifestAction(t *testing.T, preview Preview) Action {
	t.Helper()
	var found []Action
	for _, action := range preview.Actions {
		if action.Path == "workharbor.json" {
			found = append(found, action)
		}
	}
	if len(found) != 1 {
		t.Fatalf("manifest actions = %+v, want exactly one", found)
	}
	return found[0]
}

func TestInitNeverOverwritesWithoutOverwriteFlag(t *testing.T) {
	for _, paths := range [][]string{nil, {"AGENTS.md"}, {".devcontainer/devcontainer.json"}} {
		t.Run(fmt.Sprint(paths), func(t *testing.T) {
			dir, _ := consentFixture(t, 2)
			before := consentSnapshot(t, dir)
			p, err := Build(before, Options{NonInteractive: true, Yes: true, Name: consentString("replacement"), Overwrite: paths})
			if p != nil || int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), "--overwrite workharbor.json") {
				t.Fatalf("--yes or consent for another path authorized manifest replacement: proposal_present=%t error=%v", p != nil, err)
			}
			consentAssertUnchanged(t, before)
		})
	}
}

func TestInitOverwriteRequiresYesAndPath(t *testing.T) {
	for _, yes := range []bool{false, true} {
		for _, path := range []bool{false, true} {
			t.Run(fmt.Sprintf("yes=%t/path=%t", yes, path), func(t *testing.T) {
				dir, old := consentFixture(t, 2)
				before := consentSnapshot(t, dir)
				o := Options{NonInteractive: true, Yes: yes, Name: consentString("replacement")}
				if path {
					o.Overwrite = []string{"workharbor.json"}
				}
				p, err := Build(before, o)
				if !yes || !path {
					want := "--yes"
					if yes {
						want = "--overwrite workharbor.json"
					}
					if p != nil || int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), want) {
						t.Fatalf("missing consent: proposal_present=%t error=%v, want exit 70 naming %s", p != nil, err, want)
					}
					consentAssertUnchanged(t, before)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				preview := consentManifestAction(t, p.Preview())
				if preview.Action != "overwrite" || preview.Applied || !strings.Contains(preview.Diff, "replacement") || !strings.Contains(preview.Diff, "existing") {
					t.Fatalf("missing unapplied replacement diff: %+v", preview)
				}
				result, err := p.Apply(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				written, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
				if err != nil || bytes.Equal(written, old) || !bytes.Equal(written, p.Files()["workharbor.json"]) {
					t.Fatalf("authorized replacement differs from retained preview: %s, %v", written, err)
				}
				var doc map[string]any
				if err := json.Unmarshal(written, &doc); err != nil || doc["name"] != "replacement" || doc["human_shell"] != "zsh" || doc["console_mode"] != "herdr" || doc["shortcuts"] != "off" {
					t.Fatalf("replacement lost retained settings: %s, %v", written, err)
				}
				if action := consentManifestAction(t, result); action.Action != "overwrite" || !action.Applied || !result.Validation.Valid {
					t.Fatalf("replacement result missing applied/validation evidence: %+v", result)
				}
			})
		}
	}
}

func TestInitExistingManifestPreservedWithoutRequestedEdit(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, permission := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%d/permission=%t", version, permission), func(t *testing.T) {
				dir, original := consentFixture(t, version)
				before := consentSnapshot(t, dir).Entries["workharbor.json"]
				o := Options{NonInteractive: true, Yes: true}
				if permission {
					o.Overwrite = []string{"workharbor.json"}
				}
				p := applyProposal(t, dir, o)
				if !bytes.Equal(p.Files()["workharbor.json"], original) {
					t.Fatal("permission alone changed retained manifest bytes")
				}
				result, err := p.Apply(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				after := consentSnapshot(t, dir).Entries["workharbor.json"]
				if !bytes.Equal(after.Data, original) || !os.SameFile(before.Info, after.Info) || before.Mode != after.Mode || !before.Info.ModTime().Equal(after.Info.ModTime()) {
					t.Fatal("no-edit apply rewrote, migrated or replaced the existing manifest")
				}
				if action := consentManifestAction(t, result); action.Action != "skip_existing" || action.Applied || action.Diff != "" {
					t.Fatalf("retained manifest must be unapplied without a diff: %+v", action)
				}
			})
		}
	}
}

func TestInitManifestEditsNeedOverwrite(t *testing.T) {
	for _, edit := range []struct {
		key   string
		value any
		set   func(*Options)
	}{
		{"name", "replacement", func(o *Options) { o.Name = consentString("replacement") }},
		{"human_shell", "bash", func(o *Options) { o.HumanShell = consentString("bash") }},
		{"console_mode", "plain", func(o *Options) { o.ConsoleMode = consentString("plain") }},
		{"shortcuts", "on", func(o *Options) { o.Shortcuts = consentString("on") }},
		{"start_dir", ".", func(o *Options) { o.StartDir = consentString(".") }},
		{"project", map[string]any{"url": "https://example.test/team/repo.git", "path": "projects/repo"}, func(o *Options) { o.ProjectURL = consentString("https://example.test/team/repo.git") }},
	} {
		t.Run(edit.key, func(t *testing.T) {
			dir, _ := consentFixture(t, 2)
			before := consentSnapshot(t, dir)
			o := Options{DryRun: true}
			edit.set(&o)
			if p, err := Build(before, o); p != nil || int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), "--overwrite workharbor.json") {
				t.Fatalf("dry-run manifest edit bypassed per-path consent: proposal_present=%t error=%v", p != nil, err)
			}
			o.Overwrite = []string{"workharbor.json"}
			p, err := Build(before, o)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(p.Files()["workharbor.json"], &doc); err != nil || !reflect.DeepEqual(doc[edit.key], edit.value) {
				t.Fatalf("requested %s edit absent from preview: %s, %v", edit.key, p.Files()["workharbor.json"], err)
			}
			if action := consentManifestAction(t, p.Preview()); action.Action != "overwrite" || action.Applied || action.Diff == "" {
				t.Fatalf("missing consented edit preview: %+v", action)
			}
			consentAssertUnchanged(t, before)
		})
	}
}

func TestInitNonInteractiveWithoutYes(t *testing.T) {
	for _, version := range []int{0, 2} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%d/dry=%t", version, dry), func(t *testing.T) {
				dir, _ := consentFixture(t, version)
				before := consentSnapshot(t, dir)
				p, err := Build(before, Options{NonInteractive: true, DryRun: dry})
				if !dry {
					if p != nil || int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), "--yes") {
						t.Fatalf("unattended init without --yes: proposal_present=%t error=%v", p != nil, err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					result, err := p.Apply(context.Background())
					if err != nil || !result.DryRun {
						t.Fatalf("unattended dry-run without --yes refused: %+v, %v", result, err)
					}
					for _, action := range result.Actions {
						if action.Applied {
							t.Fatalf("dry-run applied %+v", action)
						}
					}
				}
				consentAssertUnchanged(t, before)
			})
		}
	}
}
