package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/skillset"
)

func lanePackageFixture(t *testing.T) skillset.Config {
	t.Helper()
	cfg, _ := laneInstalledPackageFixture(t)
	return cfg
}

func laneInstalledPackageFixture(t *testing.T) (skillset.Config, string) {
	t.Helper()
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := []byte("External instructions.\n")
	files := []skillset.File{{Path: "SKILL.md", SHA256: skillset.Digest(text)}}
	manifest := skillset.Manifest{ContractVersion: 1, Identity: "alternative", Entrypoint: "SKILL.md", RequiredProjectInputs: []string{"project_policy"}, Adapters: []skillset.Binding{{Name: "codex", Version: "fixture-version", Model: "fixture-model", Effort: "low"}}, Files: files}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"SKILL.md": text, skillset.ManifestName: data} {
		if err := os.WriteFile(filepath.Join(source, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := skillset.InventoryDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	pin := skillset.Pin{Identity: "alternative", Source: "https://example.test/pack", Commit: strings.Repeat("a", 40), ManifestSHA256: skillset.Digest(data), InventorySHA256: inventory, ContractVersion: 1}
	installed, err := (skillset.Store{Root: root}).Install(source, pin)
	if err != nil {
		t.Fatal(err)
	}
	return skillset.Config{Selection: "package", Store: root, Package: &pin}, installed.Directory
}

func TestSelectedLanePackageDoesNotClaimNativeSupport(t *testing.T) {
	selection := lanePackageFixture(t)
	status, detail := selectedLanePackage(selection, nil)
	if status != NotVerified || !strings.Contains(detail, "canonical role bindings") || !strings.Contains(detail, "model=fixture-model effort=low") {
		t.Fatalf("%s: %s", status, detail)
	}
	status, _ = selectedLanePackage(skillset.Config{Selection: "none"}, nil)
	if status != NotVerified {
		t.Fatalf("none status = %s", status)
	}
}

func TestSelectedLanePackageRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, *skillset.Config, string)
	}{
		{"missing pin", func(_ *testing.T, cfg *skillset.Config, _ string) { cfg.Package = nil }},
		{"missing default", func(_ *testing.T, cfg *skillset.Config, _ string) { cfg.Selection = "default"; cfg.Package = nil }},
		{"manifest hash", func(_ *testing.T, cfg *skillset.Config, _ string) {
			cfg.Package.ManifestSHA256 = strings.Repeat("b", 64)
		}},
		{"inventory hash", func(_ *testing.T, cfg *skillset.Config, _ string) {
			cfg.Package.InventorySHA256 = strings.Repeat("b", 64)
		}},
		{"path root", func(_ *testing.T, cfg *skillset.Config, _ string) { cfg.Store = filepath.Join(cfg.Store, "..") }},
		{"case alias", func(t *testing.T, _ *skillset.Config, directory string) {
			if err := os.Chmod(filepath.Join(directory, "SKILL.md"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "skill.md"), []byte("alias"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing skills", func(t *testing.T, _ *skillset.Config, directory string) {
			if err := os.Remove(filepath.Join(directory, "SKILL.md")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, directory := laneInstalledPackageFixture(t)
			test.change(t, &cfg, directory)
			status, detail := selectedLanePackage(cfg, nil)
			if status != Fail || !Failed([]Result{{Status: status, Detail: detail}}) {
				t.Fatalf("invalid package did not fail doctor: %s: %s", status, detail)
			}
		})
	}
}

func TestLaneAgentsChecksSelectedPackageWithoutClaudeDescriptors(t *testing.T) {
	rig := newRig(t)
	selection, directory := laneInstalledPackageFixture(t)
	rig.cfg.SkillSet = selection
	rig.write(t)
	check := laneAgentsCheck(Deps{ConfigPath: rig.cfgPath})
	if status, detail := check(context.Background()); status != NotVerified || !strings.Contains(detail, "selected package alternative") {
		t.Fatalf("%s: %s", status, detail)
	}
	if err := os.Remove(filepath.Join(directory, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	results := Run(context.Background(), []Check{{Name: "lane-agents", Run: check}}, nil)
	if !Failed(results) {
		t.Fatalf("invalid selected package did not fail command report: %+v", results)
	}
}
