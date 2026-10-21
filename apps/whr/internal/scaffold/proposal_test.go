// SPDX-License-Identifier: EUPL-1.2
package scaffold_test

import (
	"bytes"
	"encoding/json"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
	"strings"
	"testing"
)

func str(s string) *string { return &s }
func snapshot() scaffold.Snapshot {
	return scaffold.Snapshot{Directory: "workspace", Entries: map[string]scaffold.Entry{".devcontainer": {Kind: "directory"}, ".devcontainer/devcontainer.json": {Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}}}
}
func defaults() scaffold.Options { return scaffold.Options{Name: str("demo"), DryRun: true} }
func TestInitCreatesExpectedManifest(t *testing.T) {
	p, err := scaffold.Build(snapshot(), defaults())
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(p.Files()["workharbor.json"], &manifest); err != nil {
		t.Fatalf("expected proposed manifest, got %v", err)
	}
	for key, want := range map[string]any{"schema_version": float64(2), "name": "demo", "start_dir": ".", "human_shell": "fish", "console_mode": "herdr", "shortcuts": "on"} {
		if manifest[key] != want {
			t.Errorf("%s=%v, want %v", key, manifest[key], want)
		}
	}
}
func TestInitOutputValidates(t *testing.T) {
	p, err := scaffold.Build(snapshot(), defaults())
	if err != nil {
		t.Fatal(err)
	}
	if p.Preview().Validation == nil || !p.Preview().Validation.Valid {
		t.Fatal("proposal must carry a valid Git-free validation report")
	}
}
func TestInitMissingInputNeedsHuman(t *testing.T) {
	_, err := scaffold.Build(scaffold.Snapshot{Directory: "empty"}, defaults())
	if int(exitcode.FromError(err)) != 70 {
		t.Fatalf("missing devcontainer must need human input, got %v", err)
	}
}
func TestInitTemplateRefused(t *testing.T) {
	opts := defaults()
	opts.Template = str("unsupported")
	_, err := scaffold.Build(snapshot(), opts)
	if int(exitcode.FromError(err)) != 64 {
		t.Fatalf("unsupported starter must be refused, got %v", err)
	}
}

func TestInitRefusesFileRoleCollisions(t *testing.T) {
	for _, tc := range []struct{ dc, projects string }{
		{"projects/.gitkeep", "projects"}, {"workharbor.json", "projects"},
		{".claude/settings.json", "projects"}, {".claude", "projects"},
		{".devcontainer/devcontainer.json", ".claude/settings.json"},
		{"projects", "projects"}, {"custom", "custom/nested"},
		{".devcontainer/devcontainer.json", "workharbor.json/projects"},
	} {
		t.Run(tc.dc+"_"+tc.projects, func(t *testing.T) {
			s := snapshot()
			raw, _ := json.Marshal(map[string]any{"schema_version": 1, "devcontainer_path": tc.dc, "projects_path": tc.projects, "bundles": []string{}})
			s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: raw}
			if tc.dc != "workharbor.json" {
				s.Entries[tc.dc] = scaffold.Entry{Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}
			}
			_, err := scaffold.Build(s, scaffold.Options{DryRun: true, Overwrite: []string{tc.dc}})
			if int(exitcode.FromError(err)) != 68 {
				t.Fatalf("conflicting file/directory roles %s and %s accepted: %v", tc.dc, tc.projects, err)
			}
		})
	}
}
func TestInitPreservesNearLimitManifestWithoutEdit(t *testing.T) {
	s := snapshot()
	raw, _ := json.Marshal(map[string]any{"schema_version": 2, "devcontainer_path": ".devcontainer/devcontainer.json", "projects_path": "projects", "bundles": []string{}, "start_dir": strings.Repeat("a", 65390)})
	if findings := workspacecfg.CheckManifest(raw).Findings; len(findings) != 0 {
		t.Fatalf("invalid fixture: %v", findings)
	}
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: raw}
	p, err := scaffold.Build(s, scaffold.Options{DryRun: true})
	if err != nil {
		t.Fatalf("valid %d-byte no-edit manifest refused: %v", len(raw), err)
	}
	if !bytes.Equal(p.Files()["workharbor.json"], raw) {
		t.Fatal("no-edit proposal changed manifest bytes")
	}
	if !p.Preview().Validation.Valid {
		t.Fatal("retained bytes must validate")
	}
}
