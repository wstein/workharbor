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
	r["diskutil info -plist "+disk] = strings.Replace(r["diskutil info -plist /Volumes/Fake SSD"], "exfat", "apfs", 1)
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

func TestAnExistingFileIsNotAskedAboutOrChangedWhereItAlreadyAnswers(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(t.TempDir(), "mine")
	old := `{"account":"shared","repositories":[{"name":"own/repo"}],"roots":{"workspaces":["` + mine + `"],"tool_store":"` + mine + `-tools"},"extra_unknown":1}`
	if err := os.WriteFile(d.ConfigPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &answers{confirm: true}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{mine, mine + "-tools", filepath.Join(d.Home, "workspaces"), filepath.Join(d.Home, "tools")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was created though the file names its own roots", p)
		}
	}
	shown := strings.Join(a.shown, "\n")
	for _, want := range []string{"own/repo", mine, "shared"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the summary lacks %q: %s", want, shown)
		}
	}
	var m map[string]any
	raw, _ := os.ReadFile(d.ConfigPath)
	if err := json.Unmarshal(raw, &m); err != nil || m["account"] != "shared" || m["extra_unknown"] != 1.0 || m["listen"] == nil || m["api_token_file"] == nil {
		t.Errorf("merged %s, %v", raw, err)
	}
}

func TestRootsWithoutWorkspacesGetWorkspacesAndKeepTheRest(t *testing.T) {
	d, disk := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigPath, []byte(`{"roots":{"tool_store":"/keep/tools"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &answers{confirm: true, lines: []string{"wstein/workharbor", "3", ""}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Roots struct {
			Workspaces []string `json:"workspaces"`
			ToolStore  string   `json:"tool_store"`
		}
	}
	raw, _ := os.ReadFile(d.ConfigPath)
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Roots.ToolStore != "/keep/tools" || len(cfg.Roots.Workspaces) != 1 || cfg.Roots.Workspaces[0] != filepath.Join(disk, "workspaces") {
		t.Errorf("roots %+v, %v", cfg.Roots, err)
	}
	if fi, err := os.Stat(filepath.Join(disk, "workspaces")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("workspaces folder: %v %v", fi, err)
	}
}

func TestAConfigThatAlreadyHasEverythingIsNotOverwritten(t *testing.T) {
	d, _ := configDeps(t)
	first := &answers{confirm: true, lines: []string{"wstein/workharbor", "", ""}}
	if err := baseStep(t, d).Fix.Do(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	err := baseStep(t, d).Fix.Do(context.Background(), &answers{confirm: true})
	if err == nil || !strings.Contains(err.Error(), "not overwritten") {
		t.Errorf("%v", err)
	}
}

func TestABadRepositoryNameIsRefusedBeforeAnyOtherQuestion(t *testing.T) {
	d, _ := configDeps(t)
	a := &answers{lines: []string{"not a repo"}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err == nil || len(a.lines) != 0 {
		t.Errorf("%v, %d lines left", err, len(a.lines))
	}
}

func TestTheSummaryEscapesWhatItShows(t *testing.T) {
	s := configSummary(map[string]any{"roots": map[string]any{"workspaces": []string{"/a\u009b2J"}}})
	if strings.Contains(s, "\u009b") || !strings.Contains(s, `\u009b2J`) {
		t.Errorf("%q", s)
	}
}

func TestAConfigHoldingOnlyNullIsRefusedWithoutAPanicOrAWrite(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigPath, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigMap(d.ConfigPath); err == nil || !strings.Contains(err.Error(), "is not a JSON object") {
		t.Errorf("readConfigMap: %v", err)
	}
	a := &answers{confirm: true, lines: []string{"wstein/workharbor", "", ""}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err == nil || !strings.Contains(err.Error(), "is not a JSON object") {
		t.Errorf("config-base: %v", err)
	}
	if raw, _ := os.ReadFile(d.ConfigPath); string(raw) != "null\n" {
		t.Errorf("the file changed: %q", raw)
	}
	if _, err := os.Stat(d.ConfigPath + ".bak"); err == nil {
		t.Error("a backup was written")
	}
}

func TestLargeAndExponentNumbersSurviveARewriteUnchanged(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(t.TempDir(), "mine")
	old := `{"big":9007199254740993,"huge":12345678901234567890,"exp":1e2,"account":"shared","repositories":[{"name":"own/repo"}],"roots":{"workspaces":["` + mine + `"],"tool_store":"` + mine + `-tools"}}`
	if err := os.WriteFile(d.ConfigPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := baseStep(t, d).Fix.Do(context.Background(), &answers{confirm: true}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(d.ConfigPath)
	for _, want := range []string{`"big": 9007199254740993`, `"huge": 12345678901234567890`, `"exp": 1e2`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("lost %s in %s", want, raw)
		}
	}
	if _, err := readConfigMap(d.ConfigPath); err != nil {
		t.Error(err)
	}
	if err := os.WriteFile(d.ConfigPath, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigMap(d.ConfigPath); err == nil {
		t.Error("trailing data was accepted")
	}
}

func TestTheSummaryShowsSeveralWorkspacesAndEndsWithTheLastPathIntact(t *testing.T) {
	s := configSummary(map[string]any{"roots": map[string]any{
		"workspaces": []any{"/a/one", "/b/two[1]"},
		"tool_store": "/t/store",
	}})
	if !strings.Contains(s, "workspaces:  /a/one, /b/two[1]\n") || !strings.Contains(s, "tool store:  /t/store\n") {
		t.Errorf("%q", s)
	}
	if s := configSummary(map[string]any{"roots": map[string]any{"workspaces": []string{"/x", "/y"}}}); !strings.Contains(s, "workspaces:  /x, /y\n") {
		t.Errorf("%q", s)
	}
}

func TestTheRepositoryLineEscapesAHostileNameFromAnExistingFile(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(t.TempDir(), "mine")
	old := `{"repositories":[{"name":"a/b\u001b[2J\u009b2J"}],"roots":{"workspaces":["` + mine + `"]}}`
	if err := os.WriteFile(d.ConfigPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &answers{confirm: true, lines: []string{"shared"}}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	shown := strings.Join(a.shown, "\n")
	if strings.ContainsAny(shown, "\u001b\u009b") || !strings.Contains(shown, `repository:  a/b\x1b[2J\u009b2J`) {
		t.Errorf("%q", shown)
	}
}

func TestTheToolStoreLineShowsTheStoreTheFileGets(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(t.TempDir(), "mine")
	if err := os.WriteFile(d.ConfigPath, []byte(`{"account":"shared","repositories":[{"name":"own/repo"}],"roots":{"workspaces":["`+mine+`"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &answers{confirm: true}
	if err := baseStep(t, d).Fix.Do(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	// only tool_store was missing: that alone counts as added, not "not overwritten"
	if shown := strings.Join(a.shown, "\n"); !strings.Contains(shown, "tool store:  "+filepath.Join(d.Home, "tools")) {
		t.Errorf("%q", shown)
	}
	var cfg struct {
		Roots struct {
			ToolStore string `json:"tool_store"`
		} `json:"roots"`
	}
	raw, _ := os.ReadFile(d.ConfigPath)
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Roots.ToolStore != filepath.Join(d.Home, "tools") {
		t.Errorf("%s %v", raw, err)
	}
}

func TestANonObjectRootsIsRefusedWithoutAWrite(t *testing.T) {
	d, _ := configDeps(t)
	if err := os.MkdirAll(filepath.Dir(d.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	const old = `{"roots":["/x"]}`
	if err := os.WriteFile(d.ConfigPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	err := baseStep(t, d).Fix.Do(context.Background(), &answers{confirm: true, lines: []string{"wstein/workharbor"}})
	if err == nil || !strings.Contains(err.Error(), "roots entry that is not an object") {
		t.Errorf("%v", err)
	}
	if raw, _ := os.ReadFile(d.ConfigPath); string(raw) != old {
		t.Errorf("the file changed: %q", raw)
	}
}
