package doctor

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func accountRig(t *testing.T) (Deps, string) {
	t.Helper()
	home := t.TempDir()
	d := hostDeps(scripted{})
	d.User, d.Account, d.AccountHome = "admin", "workharbor", home
	d.ConfigPath = filepath.Join(t.TempDir(), "none.json")
	d.SystemConfigFile = filepath.Join(t.TempDir(), "etc", "config.json")
	return d, home
}

func buildAccountFix(d Deps) error {
	_, err := d.accountConfigFix().Build(context.Background(), nil)
	return err
}

func TestBuildRefusesALinkedConfigFolder(t *testing.T) {
	d, home := accountRig(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	if err := buildAccountFix(d); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("a linked .config: %v", err)
	}
}

func TestBuildRefusesAnUnknownState(t *testing.T) {
	d, home := accountRig(t)
	cfg := filepath.Join(home, ".config")
	if err := os.Mkdir(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfg, 0o600) })
	if st, _ := d.accountConfigState(); st != "unknown" {
		t.Fatalf("EACCES is %q, want unknown", st)
	}
	if err := buildAccountFix(d); err == nil || !strings.Contains(err.Error(), "not writing") {
		t.Fatalf("unknown state: %v", err)
	}
}

func TestMissingFoldersPassTheGuardAndForeignOwnersDoNot(t *testing.T) {
	d, home := accountRig(t)
	if st, _ := d.accountConfigState(); st != "missing" {
		t.Fatalf("no .config is %q, want missing", st)
	}
	if err := d.checkAccountPath(); err != nil {
		t.Errorf("missing folders: %v", err)
	}
	mine, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.Lstat("/private/etc")
	if err != nil {
		t.Skip("no /private/etc")
	}
	if sameOwner(mine, root) && os.Getuid() != 0 {
		t.Error("a root-owned folder counted as the account's")
	}
	if !sameOwner(mine, mine) {
		t.Error("a folder is not owned like itself")
	}
}

func TestBuildRefusesAnExistingFile(t *testing.T) {
	d, home := accountRig(t)
	dir := filepath.Join(home, ".config", "whr")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := buildAccountFix(d); err == nil || !strings.Contains(err.Error(), "not overwritten") {
		t.Fatalf("existing file: %v", err)
	}
}

func TestBuildRefusesAFolderOwnedBySomeoneElse(t *testing.T) {
	d, home := accountRig(t)
	cfg := filepath.Join(home, ".config")
	if err := os.Mkdir(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	old := ownerOf
	t.Cleanup(func() { ownerOf = old })
	ownerOf = func(fi fs.FileInfo) (uint32, bool) {
		if fi.Name() == ".config" {
			return 0, true
		}
		return 501, true
	}
	if err := buildAccountFix(d); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("foreign owner: %v", err)
	}
}
