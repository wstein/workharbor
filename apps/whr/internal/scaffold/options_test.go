// SPDX-License-Identifier: EUPL-1.2
package scaffold_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
)

func TestInitPreservesExistingSettings(t *testing.T) {
	s := snapshot()
	old := []byte("{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[]}\n")
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: old}
	s.Entries["projects"] = scaffold.Entry{Kind: "directory"}
	s.Entries["projects/.gitkeep"] = scaffold.Entry{Kind: "file"}
	for _, o := range []scaffold.Options{{DryRun: true}, {DryRun: true, Overwrite: []string{"workharbor.json"}}} {
		p, err := scaffold.Build(s, o)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(p.Files()["workharbor.json"], old) {
			t.Fatal("a permission alone must not rewrite existing bytes")
		}
		if len(p.Preview().Settings) != 0 {
			t.Fatalf("missing settings must not be presented as persisted: %#v", p.Preview().Settings)
		}
	}
	o := scaffold.Options{DryRun: true, HumanShell: str("fish")}
	_, err := scaffold.Build(s, o)
	if int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), "--overwrite workharbor.json") {
		t.Fatalf("explicit default needs ordinary overwrite consent: %v", err)
	}
	o.Overwrite = []string{"workharbor.json"}
	p, err := scaffold.Build(s, o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(p.Files()["workharbor.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m["schema_version"] != float64(2) || m["human_shell"] != "fish" {
		t.Fatalf("current-format replacement missing: %#v", m)
	}
	if !strings.Contains(strings.Join(p.Preview().NextSteps, " "), "T035") {
		t.Fatal("v2 must warn that it is not creation-ready")
	}
}

func TestInitProjectURLRules(t *testing.T) {
	for _, url := range []string{"https://user:secret@example.test/o/a.git", "ssh://git:secret@example.test/o/a.git", "file:///tmp/a.git", "https://example.test/o/a.git?token=x", "https://example.test/o/a%zz.git", "https://example.test/o/../"} {
		o := defaults()
		o.ProjectURL = str(url)
		_, err := scaffold.Build(snapshot(), o)
		if int(exitcode.FromError(err)) != 64 {
			t.Errorf("unsafe URL must be usage error, got %v", err)
		}
		if err != nil && strings.Contains(err.Error(), url) {
			t.Error("URL echoed in error")
		}
	}
	for _, url := range []string{"https://example.test/o/a.git", "ssh://git@example.test/o/a.git"} {
		o := defaults()
		o.ProjectURL = str(url)
		p, err := scaffold.Build(snapshot(), o)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(p.Files()["workharbor.json"], []byte(`"path": "projects/a"`)) {
			t.Fatal("project path not derived")
		}
		found := false
		for _, f := range p.Preview().Validation.Findings {
			found = found || f.Code == "project.declared_not_pinned"
		}
		if !found {
			t.Fatal("missing declaration warning")
		}
	}
}

func TestInitRefusesUnsafeInputs(t *testing.T) {
	for _, o := range []scaffold.Options{{DryRun: true, Name: str("Upper")}, {DryRun: true, HumanShell: str("csh")}, {DryRun: true, StartDir: str("../outside")}, {DryRun: true, Overwrite: []string{".git/config"}}, {DryRun: true, Overwrite: []string{"../a"}}, {DryRun: true, Devcontainer: str("custom.json")}, {DryRun: true, Name: str("")}} {
		_, err := scaffold.Build(snapshot(), o)
		if int(exitcode.FromError(err)) != 64 {
			t.Errorf("invalid input accepted: %v", err)
		}
	}
	o := defaults()
	o.DryRun = false
	o.NonInteractive = true
	_, err := scaffold.Build(snapshot(), o)
	if int(exitcode.FromError(err)) != 70 || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unattended confirmation: %v", err)
	}
	for _, target := range []string{".devcontainer", ".devcontainer/devcontainer.json", "projects"} {
		s := snapshot()
		s.Entries[target] = scaffold.Entry{Kind: "symlink"}
		_, err := scaffold.Build(s, defaults())
		if int(exitcode.FromError(err)) != 68 {
			t.Errorf("unsafe %s: %v", target, err)
		}
	}
	s := snapshot()
	s.Entries["workharbor.json"] = scaffold.Entry{Kind: "file", Data: []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":["unsupported"]}`)}
	_, err = scaffold.Build(s, defaults())
	if int(exitcode.FromError(err)) != 68 {
		t.Fatalf("unsupported existing bundles must remain refused: %v", err)
	}
}

func TestInitRejectsInvalidUTF8Flags(t *testing.T) {
	for _, o := range []scaffold.Options{{DryRun: true, StartDir: str("x\xff")}, {DryRun: true, Overwrite: []string{"x\xff"}}} {
		_, err := scaffold.Build(snapshot(), o)
		if int(exitcode.FromError(err)) != 64 {
			t.Errorf("invalid UTF-8 must be refused before JSON replacement: %v", err)
		}
	}
}
func TestInitInputDiagnosticsDeterministic(t *testing.T) {
	for range 100 {
		_, err := scaffold.Build(snapshot(), scaffold.Options{DryRun: true, Name: str(""), StartDir: str("")})
		if err == nil || !strings.Contains(err.Error(), "--name") {
			t.Fatalf("same bad flags must identify the first setting consistently: %v", err)
		}
	}
}
