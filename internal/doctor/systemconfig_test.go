package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const adminCfg = `{"listen":"127.0.0.1:8484",` +
	`"api_token_file":"/Users/workharbor/.config/whr/SECRETTOKENPATH",` +
	`"agent_api_key_env_file":"/Users/workharbor/.config/whr/SECRETENV",` +
	`"bot_signing_key_file":"/Users/workharbor/.config/whr/SECRETKEY",` +
	`"github":{"app_id":1,"private_key_file":"/x/SECRETPEM"},` +
	`"roots":{"workspaces":["/Volumes/Work/ws"],"tool_store":"/x/tools"}}`

func TestSystemConfigContentHasNoSecrets(t *testing.T) {
	b, err := systemConfigOf([]byte(adminCfg), "workharbor")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"user": "workharbor"`, `"/Volumes/Work/ws"`, `"dashboard_port": 8484`} {
		if !strings.Contains(s, want) {
			t.Errorf("record lacks %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "SECRET") || strings.Contains(s, "tools") || strings.Contains(s, "token") {
		t.Errorf("record carries a configuration field it must not:\n%s", s)
	}
}

func adminDeps(t *testing.T) Deps {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := hostDeps(scripted{})
	d.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	d.SystemConfigFile = filepath.Join(t.TempDir(), "etc", "whr", SystemConfigName)
	if err := os.WriteFile(d.ConfigPath, []byte(adminCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSystemConfigStepIdempotentAndNeverFails(t *testing.T) {
	d := adminDeps(t)
	c := steps(t, d)["system-config"]
	if st, _ := status(c); st != Warn {
		t.Fatalf("missing record: %s", st)
	}
	if !strings.Contains(c.Fix.Desc, "/Volumes/Work/ws") {
		t.Errorf("the plan does not show the record: %q", c.Fix.Desc)
	}
	if err := c.Fix.Do(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(systemConfigTemp()) // what sudo install would copy
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(d.SystemConfigFile), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.SystemConfigFile, b, 0o600); err != nil { //nolint:gosec // a test path under t.TempDir
		t.Fatal(err)
	}
	if st, msg := status(c); st != OK {
		t.Fatalf("after install: %s %s", st, msg)
	}
	changed := strings.Replace(adminCfg, "/Volumes/Work/ws", "/Volumes/Other", 1)
	if err := os.WriteFile(d.ConfigPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := status(steps(t, d)["system-config"]); st != Warn {
		t.Errorf("stale record: %s, want warn", st)
	}
}

func TestSystemConfigCommandsAndMissingConfig(t *testing.T) {
	d := adminDeps(t)
	c := steps(t, d)["system-config"]
	if len(c.Fix.Cmds) != 2 {
		t.Fatalf("want two commands, got %d", len(c.Fix.Cmds))
	}
	dir, inst := c.Fix.Cmds[0], c.Fix.Cmds[1]
	if !dir.Sudo || strings.Join(dir.Argv[:8], " ") != "install -d -m 0755 -o root -g wheel" || dir.Argv[8] != filepath.Dir(d.SystemConfigFile) {
		t.Errorf("directory command: %v", dir)
	}
	a := inst.Argv
	if !inst.Sudo || len(a) != 9 || strings.Join(a[:7], " ") != "install -m 0644 -o root -g wheel" || a[8] != d.SystemConfigFile {
		t.Errorf("install command: %v", inst)
	}
	if got := (Deps{}).systemConfigFile(); got != "/etc/whr/config.json" {
		t.Errorf("default path %q", got)
	}
	if err := os.Remove(d.ConfigPath); err != nil {
		t.Fatal(err)
	}
	st, msg := status(steps(t, d)["system-config"])
	if st != NotVerified || !strings.HasPrefix(msg, needsConfig) {
		t.Errorf("missing config: %s %q", st, msg)
	}
}

// Issue #510: the host steps find the workspace roots in the user config, then
// in the system config, and warn (not_verified, never fail) when neither reads.
func TestWorkspaceRootsLookupOrder(t *testing.T) {
	write := func(t *testing.T, path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil { //nolint:gosec // a test path under t.TempDir
			t.Fatal(err)
		}
	}
	const sys = `{"version":1,"user":"workharbor","workspaces":["/Volumes/Sys/ws"]}`

	t.Run("user config readable wins", func(t *testing.T) {
		d := adminDeps(t)
		write(t, d.SystemConfigFile, sys, 0o644)
		roots, st, msg := d.workspaceRoots()
		if st != "" || len(roots) != 1 || roots[0] != "/Volumes/Work/ws" {
			t.Fatalf("roots %v, %q, %q", roots, st, msg)
		}
	})
	t.Run("only the system config readable", func(t *testing.T) {
		d := adminDeps(t)
		if os.Geteuid() == 0 {
			t.Skip("root reads every file")
		}
		write(t, d.SystemConfigFile, sys, 0o644)
		if err := os.Chmod(d.ConfigPath, 0); err != nil {
			t.Fatal(err)
		}
		roots, st, msg := d.workspaceRoots()
		if st != "" || len(roots) != 1 || roots[0] != "/Volumes/Sys/ws" {
			t.Fatalf("roots %v, %q, %q", roots, st, msg)
		}
	})
	t.Run("neither readable warns", func(t *testing.T) {
		d := adminDeps(t)
		if os.Geteuid() == 0 {
			t.Skip("root reads every file")
		}
		if err := os.Chmod(d.ConfigPath, 0); err != nil {
			t.Fatal(err)
		}
		_, st, msg := d.workspaceRoots()
		if st != NotVerified || !strings.Contains(msg, d.ConfigPath) || !strings.Contains(msg, d.SystemConfigFile) {
			t.Fatalf("%s: %q", st, msg)
		}
		c := steps(t, d)["workspace-folders"]
		if st, _ := status(c); st == Fail {
			t.Fatal("the step must not fail when no config is readable")
		}
	})
	t.Run("missing user config does not fall back", func(t *testing.T) {
		d := adminDeps(t)
		d.User = d.account() // the whr account's own run
		write(t, d.SystemConfigFile, sys, 0o644)
		if err := os.Remove(d.ConfigPath); err != nil {
			t.Fatal(err)
		}
		if _, st, _ := d.workspaceRoots(); st != NotVerified {
			t.Fatalf("status %s", st)
		}
	})
	t.Run("administrator run, default path without a config", func(t *testing.T) {
		d := adminDeps(t)
		d.User = "admin"
		d.Account = "workharbor"
		write(t, d.SystemConfigFile, sys, 0o644)
		if err := os.Remove(d.ConfigPath); err != nil {
			t.Fatal(err)
		}
		roots, st, msg := d.workspaceRoots()
		if st != "" || len(roots) != 1 || roots[0] != "/Volumes/Sys/ws" {
			t.Fatalf("roots %v, %q, %q", roots, st, msg)
		}
		if u := d.needsRoots(t.Context()); u != nil {
			t.Fatalf("roots steps unreachable: %+v", u)
		}
		for _, name := range []string{"workspace-folders", "workspace-volume", "spotlight"} {
			if u := steps(t, d)[name].Reach(t.Context()); u != nil {
				t.Errorf("%s must be reachable by the roots rule: %+v", name, u)
			}
		}
		if u := steps(t, d)["system-config"].Reach(t.Context()); u == nil {
			t.Fatal("system-config must stay unreachable: it needs the full user config")
		}
	})
}
