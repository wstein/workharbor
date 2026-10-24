// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func resultAcceptanceWarningCount(t *testing.T, preview Preview, code string) int {
	t.Helper()
	if preview.Validation == nil || !preview.Validation.Valid {
		t.Fatalf("warning-only proposal/result lacks valid report: %+v", preview.Validation)
	}
	count := 0
	for _, finding := range preview.Validation.Findings {
		if finding.Code == code {
			count++
			if finding.Severity != workspacecfg.SevWarning {
				t.Fatalf("%s must remain a warning: %+v", code, finding)
			}
		}
	}
	return count
}

func TestInitProjectDeclaredNotPinnedWarning(t *testing.T) {
	dir := fixture(t)
	url := "https://example.test/team/project.git"
	p := applyProposal(t, dir, Options{NonInteractive: true, Yes: true, ProjectURL: &url})
	if count := resultAcceptanceWarningCount(t, p.Preview(), "project.declared_not_pinned"); count != 1 {
		t.Fatalf("preview warning count = %d, want one", count)
	}
	result, err := p.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count := resultAcceptanceWarningCount(t, result, "project.declared_not_pinned"); count != 1 {
		t.Fatalf("result warning count = %d, want one", count)
	}
	if _, err := os.Lstat(filepath.Join(dir, "projects/project")); !os.IsNotExist(err) {
		t.Fatalf("declaration unexpectedly created a project checkout: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
	if err != nil || !bytes.Equal(raw, p.Files()["workharbor.json"]) || !bytes.Contains(raw, []byte(url)) || !bytes.Contains(raw, []byte(`"path": "projects/project"`)) {
		t.Fatalf("written declaration differs from retained proposal: %s, %v", raw, err)
	}
	if result.GitChecks != "not_run" || result.RuntimeEnforcement != "not_in_this_release" {
		t.Fatalf("declaration claimed runtime/Git work: %+v", result)
	}
}

func TestInitVersion2NotCreationReadyWarning(t *testing.T) {
	for _, kind := range []string{"new-v2", "consented-v1-replacement", "retained-v1"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t)
			old := []byte("{ \"schema_version\": 1, \"devcontainer_path\": \".devcontainer/devcontainer.json\", \"projects_path\": \"projects\", \"bundles\": [] }\n\n")
			if kind != "new-v2" {
				if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), old, 0600); err != nil {
					t.Fatal(err)
				}
			}
			opts := Options{NonInteractive: true, Yes: true}
			if kind == "consented-v1-replacement" {
				name := "replacement"
				opts.Name = &name
				opts.Overwrite = []string{"workharbor.json"}
			}
			p := applyProposal(t, dir, opts)
			result, err := p.Apply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, view := range []struct {
				name string
				data Preview
			}{{"preview", p.Preview()}, {"result", result}} {
				count := 0
				for _, step := range view.data.NextSteps {
					if strings.Contains(step, "schema_version 2") && strings.Contains(step, "not creation-ready") && strings.Contains(step, "T035") {
						count++
					}
				}
				want := 1
				if kind == "retained-v1" {
					want = 0
				}
				if count != want {
					t.Fatalf("%s readiness warning count = %d, want %d: %v", view.name, count, want, view.data.NextSteps)
				}
			}
			raw, err := os.ReadFile(filepath.Join(dir, "workharbor.json"))
			if err != nil || !bytes.Equal(raw, p.Files()["workharbor.json"]) {
				t.Fatalf("written bytes differ from preview: %s, %v", raw, err)
			}
			if kind == "retained-v1" {
				if !bytes.Equal(raw, old) {
					t.Fatal("readiness handling rewrote retained v1 bytes")
				}
			} else if checked := workspacecfg.CheckManifest(raw); checked.Manifest == nil || checked.Manifest.SchemaVersion != 2 {
				t.Fatalf("new/replaced manifest is not v2: %s", raw)
			}
		})
	}
}
