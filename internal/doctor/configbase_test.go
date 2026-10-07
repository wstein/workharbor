package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/render"
)

// configDeps is a home with fake diskutil whose external disk is a temp folder.
func configDeps(t *testing.T) (Deps, string) {
	t.Helper()
	home := t.TempDir()
	disk := t.TempDir()
	r := fakeDiskutil(t)
	for k, v := range r {
		r[k] = strings.ReplaceAll(v, "/Volumes/Fake SSD", disk)
	}
	r["diskutil info -plist "+disk] = r["diskutil info -plist /Volumes/Fake SSD"]
	d := Deps{GOOS: "darwin", Runner: r, Home: home, ConfigPath: filepath.Join(home, ".config", "whr", "config.json")}
	return d, disk
}

func baseStep(t *testing.T, d Deps) Check {
	t.Helper()
	return steps(t, d)["config-base"]
}

func TestNothingIsWrittenWhenThePersonQuitsOrDeclines(t *testing.T) {
	for name, a := range map[string]*answers{
		"q at the volume": {lines: []string{"wstein/workharbor", "q"}},
		"q at the path":   {lines: []string{"wstein/workharbor", "4", "q"}},
		"no at the write": {lines: []string{"wstein/workharbor", "", ""}},
	} {
		d, _ := configDeps(t)
		err := baseStep(t, d).Fix.Do(context.Background(), a)
		if err == nil || (strings.HasPrefix(name, "q") && !errors.Is(err, render.ErrQuit)) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Dir(d.ConfigPath)); err == nil {
			t.Errorf("%s: left the config directory behind", name)
		}
		if _, err := os.Stat(filepath.Join(d.Home, "workspaces")); err == nil {
			t.Errorf("%s: created the workspaces folder", name)
		}
	}
}

func TestTheChosenVolumeBecomesTheWorkspaceRoot(t *testing.T) {
	d, disk := configDeps(t)
	a := &answers{confirm: true, lines: []string{"wstein/workharbor", "3", ""}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Roots struct{ Workspaces []string }
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.Roots.Workspaces) != 1 || cfg.Roots.Workspaces[0] != filepath.Join(disk, "workspaces") {
		t.Errorf("roots %v, %v", cfg.Roots.Workspaces, err)
	}
	if fi, _ := os.Stat(d.ConfigPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(disk, "workspaces")); err != nil {
		t.Error("the workspaces folder was not made")
	}
	if shown := strings.Join(a.shown, "\n"); !strings.Contains(shown, filepath.Join(disk, "workspaces")) || strings.Contains(shown, "api_token") {
		t.Errorf("the result was not shown as the summary: %q", shown)
	}
	ents, _ := os.ReadDir(filepath.Dir(d.ConfigPath))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".whr-config-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}

func TestAnExistingConfigIsSavedFirstAndKeepsItsKeys(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"listen":"127.0.0.1:9999","extra_unknown":1}`
	if err := os.WriteFile(d.ConfigPath, []byte(old), 0o644); err != nil { //nolint:gosec // a test file
		t.Fatal(err)
	}
	a := &answers{confirm: true, lines: []string{"wstein/workharbor", "", ""}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(d.ConfigPath + ".bak")
	if err != nil || string(bak) != old {
		t.Errorf("backup %q, %v", bak, err)
	}
	if fi, _ := os.Stat(d.ConfigPath + ".bak"); fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode %v", fi.Mode().Perm())
	}
	var m map[string]any
	raw, _ := os.ReadFile(d.ConfigPath)
	if err := json.Unmarshal(raw, &m); err != nil || m["listen"] != "127.0.0.1:9999" || m["extra_unknown"] != 1.0 || m["roots"] == nil {
		t.Errorf("merged config %s, %v", raw, err)
	}
	if fi, _ := os.Stat(d.ConfigPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if !strings.Contains(strings.Join(a.shown, "\n"), "backup") {
		t.Error("the backup path was not named")
	}
}

func TestYesWritesTheDefaultWithoutAConfirmation(t *testing.T) {
	d, _ := configDeps(t)
	d.Yes = true
	a := &answers{lines: []string{"wstein/workharbor", ""}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Home, "workspaces")); err != nil {
		t.Error("the default workspaces folder was not made")
	}
}

func TestAnUnwritableVolumeSaysSoWithoutARawError(t *testing.T) {
	d, disk := configDeps(t)
	if err := os.WriteFile(filepath.Join(disk, "workspaces"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := baseStep(t, d).Fix.Do(context.Background(), &answers{confirm: true, lines: []string{"wstein/workharbor", "3", ""}})
	if err == nil || !strings.Contains(err.Error(), "nothing was written") || strings.Contains(err.Error(), "mkdir ") {
		t.Errorf("%v", err)
	}
	if _, err := os.Stat(d.ConfigPath); err == nil {
		t.Error("a config was written")
	}
}
