package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type showOnly struct {
	Prompter
	shown []string
}

func (s *showOnly) Show(t string) { s.shown = append(s.shown, t) }

func TestReplacingTheConfigSavesAPrivateBackupAndSaysWhere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	marker := "marker-7f3a91c2"
	if err := os.WriteFile(path, []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &showOnly{}
	if err := replaceWithBackup(p, path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Clean(path + ".bak"))
	if err != nil || string(b) != marker {
		t.Fatalf("backup = %q, %v", b, err)
	}
	if fi, _ := os.Stat(path + ".bak"); fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode %v", fi.Mode())
	}
	if len(p.shown) != 1 || !strings.Contains(p.shown[0], path+".bak") ||
		strings.Contains(p.shown[0], marker) {
		t.Errorf("shown %q", p.shown)
	}
	// a first write has nothing to save
	fresh := filepath.Join(dir, "fresh.json")
	p = &showOnly{}
	if err := replaceWithBackup(p, fresh, []byte("x")); err != nil || len(p.shown) != 0 {
		t.Errorf("fresh: %v %q", err, p.shown)
	}
}

func TestAnOldBackupIsReplacedAtMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("one"), 0o600)
	_ = os.WriteFile(path+".bak", []byte("older"), 0o644) //nolint:gosec // the test needs a loose mode
	if err := replaceWithBackup(&showOnly{}, path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path + ".bak")
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode: %v %v", fi, err)
	}
}

func TestASymlinkAtTheBackupPathIsReplacedNotFollowed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	target := filepath.Join(dir, "target")
	_ = os.WriteFile(path, []byte("one"), 0o600)
	_ = os.WriteFile(target, []byte("untouched"), 0o600)
	if err := os.Symlink(target, path+".bak"); err != nil {
		t.Skip(err)
	}
	if err := replaceWithBackup(&showOnly{}, path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Clean(target)); string(b) != "untouched" {
		t.Errorf("the symlink target changed: %q", b)
	}
	if fi, _ := os.Lstat(path + ".bak"); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("the symlink is still there")
	}
}

func TestADirectoryAtTheBackupPathStopsBeforeTheConfigChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	_ = os.WriteFile(path, []byte("one"), 0o600)
	_ = os.Mkdir(path+".bak", 0o700)
	err := replaceWithBackup(&showOnly{}, path, []byte("two"))
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Clean(path)); string(b) != "one" {
		t.Errorf("the config changed: %q", b)
	}
}
