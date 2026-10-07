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
