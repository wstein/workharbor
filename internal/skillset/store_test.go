package skillset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadTextRejectsSwaps(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(string) error
	}{
		{"symlink to original inode", func(filename string) error {
			if err := os.Rename(filename, filename+".original"); err != nil {
				return err
			}
			return os.Symlink(filepath.Base(filename)+".original", filename)
		}},
		{"changed permissions", func(filename string) error {
			mode := os.FileMode(0o660)
			return os.Chmod(filename, mode)
		}},
		{"new hardlink", func(filename string) error { return os.Link(filename, filename+".link") }},
		{"different regular inode", func(filename string) error {
			if err := os.Rename(filename, filename+".original"); err != nil {
				return err
			}
			return os.WriteFile(filename, []byte("text\n"), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			filename := filepath.Join(directory, "text.md")
			if err := os.WriteFile(filename, []byte("text\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			before, err := root.Lstat("text.md")
			if err != nil {
				t.Fatal(err)
			}
			if err := test.change(filename); err != nil {
				t.Fatal(err)
			}
			if _, err := readTextChecked(root, "text.md", MaxFileBytes, before); err == nil {
				t.Fatal("swapped resource accepted")
			}
		})
	}
	t.Run("symlinked parent", func(t *testing.T) {
		directory := t.TempDir()
		parent := filepath.Join(directory, "nested")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, "text.md"), []byte("text\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(directory)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		before, err := root.Lstat("nested/text.md")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(parent, parent+".original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("nested.original", parent); err != nil {
			t.Fatal(err)
		}
		if _, err := readTextChecked(root, "nested/text.md", MaxFileBytes, before); err == nil {
			t.Fatal("symlinked parent accepted")
		}
	})
	t.Run("directory becomes fifo", func(t *testing.T) {
		directory := t.TempDir()
		filename := filepath.Join(directory, "nested")
		if err := os.Mkdir(filename, 0o700); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(directory)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		err = walkBounded(root, func(name string, _ os.FileInfo) error {
			if name != "nested" {
				return nil
			}
			if err := os.Remove(filename); err != nil {
				return err
			}
			return syscall.Mkfifo(filename, 0o600)
		})
		if err == nil {
			t.Fatal("swapped directory accepted")
		}
	})
}

// A delayed nonblocking writer releases a regressed blocking reader before the
// test reports failure. Both goroutines finish before the fixture is removed.
func TestReadTextRejectsFIFOWithoutBlocking(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "text.md")
	if err := os.WriteFile(filename, []byte("text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := root.Lstat("text.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filename, 0o600); err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() { _, err := readTextChecked(root, "text.md", MaxFileBytes, before); readDone <- err }()
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("FIFO resource accepted")
		}
		return
	case <-time.After(250 * time.Millisecond):
	}
	releaseDone := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			fd, err := syscall.Open(filename, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
			if err == nil {
				releaseDone <- syscall.Close(fd)
				return
			}
			if err != syscall.ENXIO || time.Now().After(deadline) {
				releaseDone <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := <-releaseDone; err != nil {
		t.Fatalf("release blocked FIFO reader: %v", err)
	}
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("FIFO reader did not finish after release")
	}
	t.Fatal("FIFO resource open blocked until a writer released it")
}

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

func TestManifestRevisionsCoexist(t *testing.T) {
	source, original := fixture(t)
	secondSource, changed := fixture(t)
	candidate, _, err := validate(secondSource, changed)
	if err != nil {
		t.Fatal(err)
	}
	manifest := candidate.Manifest
	manifest.Adapters[0].Model = "other-model"
	manifest.RequiredProjectInputs = append(manifest.RequiredProjectInputs, "contribution_rules")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondSource, ManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	changed.ManifestSHA256, changed.Commit = Digest(data), strings.Repeat("b", 40)
	if changed.InventorySHA256 != original.InventorySHA256 || changed.ManifestSHA256 == original.ManifestSHA256 {
		t.Fatal("fixture does not isolate the manifest revision")
	}
	store := Store{Root: storeRoot(t)}
	first, err := store.Install(source, original)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Install(secondSource, changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Directory == second.Directory || first.Target() != second.Target() {
		t.Fatal("host revisions must differ without changing the inventory mount target")
	}
	for _, revision := range []struct {
		source string
		pin    Pin
		pkg    Package
	}{{source, original, first}, {secondSource, changed, second}} {
		loaded, err := store.Load(revision.pin)
		if err != nil || loaded.Pin != revision.pin || loaded.Directory != revision.pkg.Directory || loaded.Text != revision.pkg.Text {
			t.Fatalf("recorded revision changed: %+v, %v", loaded, err)
		}
		if loaded.Manifest.Adapters[0].Model != revision.pkg.Manifest.Adapters[0].Model || len(loaded.Manifest.RequiredProjectInputs) != len(revision.pkg.Manifest.RequiredProjectInputs) {
			t.Fatal("loaded a different binding or prerequisite manifest")
		}
		before, err := os.Stat(loaded.Directory)
		if err != nil {
			t.Fatal(err)
		}
		reinstalled, err := store.Install(revision.source, revision.pin)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(reinstalled.Directory)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("idempotent reinstall replaced immutable revision: %v", err)
		}
	}
	filename := filepath.Join(second.Directory, ManifestName)
	if err := os.Chmod(filename, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(changed); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	if _, err := store.Install(secondSource, changed); err == nil {
		t.Fatal("reinstall repaired a tampered immutable revision")
	}
	if _, err := store.Load(original); err != nil {
		t.Fatalf("other manifest revision affected original pin: %v", err)
	}
}

func TestLoadRetainsLegacyRevisionWithoutMaskingInvalidNewRevision(t *testing.T) {
	for _, missingManifest := range []bool{false, true} {
		name := "tampered manifest"
		if missingManifest {
			name = "missing manifest"
		}
		t.Run(name, func(t *testing.T) {
			source, pin := fixture(t)
			store := Store{Root: storeRoot(t)}
			installed, err := store.Install(source, pin)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(installed.Directory) != pin.InventorySHA256+"-"+pin.ManifestSHA256 {
				t.Fatal("host key does not bind both digests")
			}
			legacy := filepath.Join(store.Root, pin.InventorySHA256)
			if err := os.Rename(installed.Directory, legacy); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(legacy)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := store.Load(pin)
			if err != nil || loaded.Pin != pin || loaded.Directory != legacy || loaded.Target() != installed.Target() {
				t.Fatalf("original legacy pin unavailable: %+v, %v", loaded, err)
			}
			wrongManifest := pin
			wrongManifest.ManifestSHA256 = strings.Repeat("0", 64)
			if _, err := store.Load(wrongManifest); err == nil {
				t.Fatal("legacy inventory accepted a different manifest pin")
			}
			current, err := store.Install(source, pin)
			if err != nil || current.Directory == legacy {
				t.Fatalf("new installation reused legacy host key: %v", err)
			}
			after, err := os.Stat(legacy)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("new installation changed retained legacy directory: %v", err)
			}
			manifest := filepath.Join(current.Directory, ManifestName)
			if missingManifest {
				if err := os.Remove(manifest); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Chmod(manifest, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.Load(pin); err == nil {
				t.Fatal("invalid new revision fell back to valid legacy content")
			}
			if _, _, err := validate(legacy, pin); err != nil {
				t.Fatalf("retained legacy revision changed: %v", err)
			}
		})
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
