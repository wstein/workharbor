package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// devConfig writes a configuration file of the test's own, with extra top-level
// keys, and returns Deps that point at it.
func devConfig(t *testing.T, mode os.FileMode, extra map[string]any) Deps {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{"listen": "127.0.0.1:8787", "unknown_future_key": "kept"}
	for k, v := range extra {
		m[k] = v
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return Deps{ConfigPath: path, Home: dir, GOOS: "darwin", Runner: scripted{}, User: "dev", UID: os.Getuid(), Prefix: filepath.Join(dir, "dev")}
}

func TestDevelopmentModeWarnsOnEveryRunWhileTheKeyIsSet(t *testing.T) {
	prefix := t.TempDir()
	for _, tc := range []struct {
		name  string
		mode  os.FileMode
		extra map[string]any
		want  Status
		text  string
	}{
		{"no key", 0o600, nil, OK, "managed installation"},
		{"empty key is none", 0o600, map[string]any{"development_prefix": ""}, OK, "managed installation"},
		{"key set", 0o600, map[string]any{"development_prefix": prefix}, Warn, "the managed setup"},
		{"relative value", 0o600, map[string]any{"development_prefix": "dev"}, Fail, "absolute"},
		{"managed value", 0o600, map[string]any{"development_prefix": "/opt/whr"}, Fail, "managed prefix"},
		{"group writable file", 0o660, map[string]any{"development_prefix": prefix}, Fail, "group or other"},
		{"wrong type", 0o600, map[string]any{"development_prefix": 5}, Fail, "wrong type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := devConfig(t, tc.mode, tc.extra)
			got, detail := status(steps(t, d)["development-mode"])
			if got != tc.want || !strings.Contains(detail, tc.text) {
				t.Errorf("= %s %q, want %s containing %q", got, detail, tc.want, tc.text)
			}
			if tc.want == Warn && (!strings.Contains(detail, "development_prefix") || !strings.Contains(detail, d.ConfigPath)) {
				t.Errorf("the warning names the key and the file: %q", detail)
			}
		})
	}
}

func TestDevelopmentModeFailsFromAManagedPrefix(t *testing.T) {
	if _, err := os.Stat("/usr/local"); err != nil {
		t.Skip("no /usr/local here")
	}
	d := devConfig(t, 0o600, map[string]any{"development_prefix": t.TempDir()})
	d.Whr = "/usr/local/bin/whr"
	if got, detail := status(steps(t, d)["development-mode"]); got != Fail || !strings.Contains(detail, "managed prefix") {
		t.Errorf("whr in a managed prefix with the key = %s %q", got, detail)
	}
	d.Whr = filepath.Join(d.Home, "bin", "whr")
	if got, _ := status(steps(t, d)["development-mode"]); got != Warn {
		t.Errorf("whr in the development prefix = %s, want warn", got)
	}
}

// An environment variable neither sets the key nor hides it.
func TestDevelopmentModeReadsNoEnvironment(t *testing.T) {
	for _, k := range []string{"WORKHARBOR_DEV", "WHR_DEV", "WHR_DEVELOPMENT_PREFIX"} {
		t.Setenv(k, "1")
	}
	none := devConfig(t, 0o600, nil)
	if got, _ := status(steps(t, none)["development-mode"]); got != OK {
		t.Errorf("without the key and with the variables = %s, want ok", got)
	}
	if got, _ := status(steps(t, none)["development-key"]); got != OK {
		t.Errorf("the step without --dev and with the variables = %s, want ok", got)
	}
	set := devConfig(t, 0o600, map[string]any{"development_prefix": t.TempDir()})
	if got, _ := status(steps(t, set)["development-mode"]); got != Warn {
		t.Errorf("with the key and the variables = %s, want warn", got)
	}
}

func TestDevelopmentKeyStep(t *testing.T) {
	prefix := t.TempDir()
	other := t.TempDir()
	for _, tc := range []struct {
		name         string
		dev, managed bool
		mode         os.FileMode
		extra        map[string]any
		want         Status
		text         string
	}{
		{"managed call, no key", false, false, 0o600, nil, OK, "not read or changed"},
		{"managed call, key kept", false, false, 0o600, map[string]any{"development_prefix": prefix}, OK, "not read or changed"},
		{"--dev, not remembered", true, false, 0o600, nil, Fail, "does not remember"},
		{"--dev, remembered", true, false, 0o600, map[string]any{"development_prefix": prefix}, OK, "remembered"},
		{"--dev, another prefix", true, false, 0o600, map[string]any{"development_prefix": other}, Fail, "not " + prefix},
		{"--dev, key refused", true, false, 0o660, map[string]any{"development_prefix": prefix}, Fail, "group or other"},
		{"--managed, no key", false, true, 0o600, nil, OK, "no development_prefix"},
		{"--managed, key set", false, true, 0o600, map[string]any{"development_prefix": prefix}, Fail, "refuses it"},
		{"--managed, key refused", false, true, 0o660, map[string]any{"development_prefix": prefix}, Fail, "group or other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := devConfig(t, tc.mode, tc.extra)
			d.Dev, d.Managed, d.Prefix = tc.dev, tc.managed, prefix
			got, detail := status(steps(t, d)["development-key"])
			if got != tc.want || !strings.Contains(detail, tc.text) {
				t.Errorf("= %s %q, want %s containing %q", got, detail, tc.want, tc.text)
			}
		})
	}
}

func readKeys(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDevelopmentKeyFixWritesAndRemovesOnlyTheKey(t *testing.T) {
	ctx := context.Background()
	prefix := t.TempDir()

	d := devConfig(t, 0o600, nil)
	d.Dev, d.Prefix = true, prefix
	st := steps(t, d)["development-key"]
	if !strings.Contains(st.Fix.Desc, "development_prefix") || !strings.Contains(st.Fix.Desc, prefix) {
		t.Errorf("the fix names the key and the prefix before it asks: %q", st.Fix.Desc)
	}
	decline := &answers{confirm: false}
	if err := st.Fix.Do(ctx, decline); err == nil {
		t.Error("a declined write must fail")
	}
	if _, ok := readKeys(t, d.ConfigPath)["development_prefix"]; ok {
		t.Error("a declined write changed the file")
	}
	yes := &answers{confirm: true}
	if err := st.Fix.Do(ctx, yes); err != nil {
		t.Fatal(err)
	}
	m := readKeys(t, d.ConfigPath)
	if m["development_prefix"] != prefix || m["unknown_future_key"] != "kept" || m["listen"] != "127.0.0.1:8787" {
		t.Errorf("after the write: %v", m)
	}
	shown := strings.Join(yes.shown, "\n")
	if !strings.Contains(shown, "+ ") || !strings.Contains(shown, "whr wrote development_prefix") || !strings.Contains(shown, "the managed setup removes it") {
		t.Errorf("the diff and the notice that it was written: %q", shown)
	}
	if got, _ := status(steps(t, d)["development-key"]); got != OK {
		t.Errorf("after the write the step = %s, want ok", got)
	}

	// --managed removes it, keeping the rest
	d.Dev, d.Managed = false, true
	rm := steps(t, d)["development-key"]
	if !strings.Contains(rm.Fix.Desc, "remove") {
		t.Errorf("the fix says remove: %q", rm.Fix.Desc)
	}
	if err := rm.Fix.Do(ctx, &answers{confirm: true}); err != nil {
		t.Fatal(err)
	}
	m = readKeys(t, d.ConfigPath)
	if _, ok := m["development_prefix"]; ok || m["unknown_future_key"] != "kept" {
		t.Errorf("after the removal: %v", m)
	}
	if got, _ := status(steps(t, d)["development-key"]); got != OK {
		t.Errorf("after the removal the step = %s, want ok", got)
	}
}

func TestDevelopmentKeyFixRefusals(t *testing.T) {
	ctx := context.Background()
	// neither --dev nor --managed: the fix writes nothing, whatever calls it
	d := devConfig(t, 0o600, nil)
	d.Prefix = t.TempDir()
	if err := steps(t, d)["development-key"].Fix.Do(ctx, &answers{confirm: true}); err == nil || !strings.Contains(err.Error(), "only by the development setup") {
		t.Errorf("a managed call = %v", err)
	}
	if _, ok := readKeys(t, d.ConfigPath)["development_prefix"]; ok {
		t.Error("a managed call wrote the key")
	}
	// a managed prefix is never written, nor a relative one
	for _, bad := range []string{"/opt/whr", "relative"} {
		d := devConfig(t, 0o600, nil)
		d.Dev, d.Prefix = true, bad
		if err := steps(t, d)["development-key"].Fix.Do(ctx, &answers{confirm: true}); err == nil {
			t.Errorf("prefix %q was written", bad)
		}
		if _, ok := readKeys(t, d.ConfigPath)["development_prefix"]; ok {
			t.Errorf("prefix %q reached the file", bad)
		}
	}
	// no file: the base configuration comes first
	d = devConfig(t, 0o600, nil)
	d.Dev, d.Prefix = true, t.TempDir()
	if err := os.Remove(d.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := steps(t, d)["development-key"].Fix.Do(ctx, &answers{confirm: true}); err == nil || !strings.Contains(err.Error(), "config-base") {
		t.Errorf("no file = %v", err)
	}
}

// encoding/json matches key names without regard to case, so a hand-edited
// "DEVELOPMENT_PREFIX" is the key: --managed removes every spelling, --dev leaves one.
func TestDevelopmentKeyFixHandlesCaseVariants(t *testing.T) {
	ctx := context.Background()
	prefix := t.TempDir()
	d := devConfig(t, 0o600, map[string]any{"DEVELOPMENT_PREFIX": prefix, "Development_Prefix": prefix, "listen": "127.0.0.1:8787"})
	d.Managed = true
	if err := steps(t, d)["development-key"].Fix.Do(ctx, &answers{confirm: true}); err != nil {
		t.Fatal(err)
	}
	m := readKeys(t, d.ConfigPath)
	if len(m) != 2 || m["listen"] != "127.0.0.1:8787" || m["unknown_future_key"] != "kept" {
		t.Errorf("a case variant of the key is left: %v", m)
	}

	d = devConfig(t, 0o600, map[string]any{"DEVELOPMENT_PREFIX": "/elsewhere"})
	d.Dev, d.Prefix = true, prefix
	if err := steps(t, d)["development-key"].Fix.Do(ctx, &answers{confirm: true}); err != nil {
		t.Fatal(err)
	}
	if m := readKeys(t, d.ConfigPath); len(m) != 3 || m["development_prefix"] != prefix {
		t.Errorf("--dev left a second spelling: %v", m)
	}
}
