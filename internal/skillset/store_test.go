package skillset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, Pin) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := []File{{Path: ".agents/role.md", SHA256: Digest([]byte("role\n"))}, {Path: "SKILL.md", SHA256: Digest([]byte("Read .agents/role.md from the mounted package root.\n"))}}
	for _, resource := range []struct{ path, text string }{{".agents/role.md", "role\n"}, {"SKILL.md", "Read .agents/role.md from the mounted package root.\n"}} {
		if err := os.WriteFile(filepath.Join(directory, resource.path), []byte(resource.text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{ContractVersion: 1, Identity: "alternative", Entrypoint: "SKILL.md", RequiredProjectInputs: []string{"project_policy"}, Adapters: []Binding{{Name: "test", Version: "1.0.0", Model: "test-model", Effort: "low"}}, Files: files}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := InventoryDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	return directory, Pin{Identity: "alternative", Source: "https://example.test/skills", Commit: strings.Repeat("a", 40), ManifestSHA256: Digest(data), InventorySHA256: digest, ContractVersion: 1}
}

func storeRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInstallAndRevalidate(t *testing.T) {
	source, pin := fixture(t)
	store := Store{Root: storeRoot(t)}
	pkg, err := store.Install(source, pin)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Target() != "/skills/"+pin.InventorySHA256 || pkg.Text == "" {
		t.Fatalf("package: %+v", pkg)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(pin); err != nil {
		t.Fatalf("source changes affected immutable copy: %v", err)
	}
	filename := filepath.Join(pkg.Directory, "SKILL.md")
	if err := os.Chmod(filename, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(pin); err == nil {
		t.Fatal("tampered installed content accepted")
	}
}

func TestRejectUnsafePackages(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(string) error
	}{
		{"unexpected dotfile", func(root string) error { return os.WriteFile(filepath.Join(root, ".hidden"), []byte("extra"), 0o600) }},
		{"missing", func(root string) error { return os.Remove(filepath.Join(root, "SKILL.md")) }},
		{"executable", func(root string) error {
			mode := os.FileMode(0o700)
			return os.Chmod(filepath.Join(root, "SKILL.md"), mode)
		}},
		{"writable", func(root string) error {
			mode := os.FileMode(0o666)
			return os.Chmod(filepath.Join(root, "SKILL.md"), mode)
		}},
		{"symlink", func(root string) error {
			if err := os.Remove(filepath.Join(root, "SKILL.md")); err != nil {
				return err
			}
			return os.Symlink(".agents/role.md", filepath.Join(root, "SKILL.md"))
		}},
		{"hardlink", func(root string) error {
			return os.Link(filepath.Join(root, "SKILL.md"), filepath.Join(root, "other.md"))
		}},
		{"directory link", func(root string) error { return os.Symlink(".agents", filepath.Join(root, "alias")) }},
		{"oversize", func(root string) error {
			return os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(strings.Repeat("a", MaxFileBytes+1)), 0o600)
		}},
		{"changed manifest", func(root string) error { return os.WriteFile(filepath.Join(root, ManifestName), []byte("{}"), 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, pin := fixture(t)
			if err := test.change(source); err != nil {
				t.Fatal(err)
			}
			if _, err := (Store{Root: storeRoot(t)}).Install(source, pin); err == nil {
				t.Fatal("unsafe package accepted")
			}
		})
	}
}

func TestStoreRejectsOverlapAndAliases(t *testing.T) {
	root := storeRoot(t)
	for _, forbidden := range []string{root, filepath.Dir(root), filepath.Join(root, "workspace")} {
		if err := (Store{Root: root, Forbidden: []string{forbidden}}).CheckRoot(); err == nil {
			t.Fatalf("accepted overlap %s", forbidden)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := (Store{Root: alias}).CheckRoot(); err == nil {
		t.Fatal("accepted store alias")
	}
}

func TestManifestRefusesUnknownAndDuplicateKeys(t *testing.T) {
	for _, data := range []string{`{"contract_version":1,"installer":"run"}`, `{"identity":"a","identity":"b"}`} {
		var manifest Manifest
		if err := decode([]byte(data), &manifest); err == nil {
			t.Fatal("accepted ambiguous or executable declaration")
		}
	}
}
