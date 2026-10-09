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

func TestAdminRecordContentHasNoSecrets(t *testing.T) {
	b, err := adminRecordOf([]byte(adminCfg), "workharbor")
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
	d.AdminRecordFile = filepath.Join(t.TempDir(), "etc", "whr", AdminRecordName)
	if err := os.WriteFile(d.ConfigPath, []byte(adminCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAdminRecordStepIdempotentAndNeverFails(t *testing.T) {
	d := adminDeps(t)
	c := steps(t, d)["admin-record"]
	if st, _ := status(c); st != Warn {
		t.Fatalf("missing record: %s", st)
	}
	if !strings.Contains(c.Fix.Desc, "/Volumes/Work/ws") {
		t.Errorf("the plan does not show the record: %q", c.Fix.Desc)
	}
	if err := c.Fix.Do(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(adminRecordTemp()) // what sudo install would copy
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(d.AdminRecordFile), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.AdminRecordFile, b, 0o600); err != nil { //nolint:gosec // a test path under t.TempDir
		t.Fatal(err)
	}
	if st, msg := status(c); st != OK {
		t.Fatalf("after install: %s %s", st, msg)
	}
	changed := strings.Replace(adminCfg, "/Volumes/Work/ws", "/Volumes/Other", 1)
	if err := os.WriteFile(d.ConfigPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := status(steps(t, d)["admin-record"]); st != Warn {
		t.Errorf("stale record: %s, want warn", st)
	}
}

func TestAdminRecordCommandsAndMissingConfig(t *testing.T) {
	d := adminDeps(t)
	c := steps(t, d)["admin-record"]
	if len(c.Fix.Cmds) != 2 {
		t.Fatalf("want two commands, got %d", len(c.Fix.Cmds))
	}
	dir, inst := c.Fix.Cmds[0], c.Fix.Cmds[1]
	if !dir.Sudo || strings.Join(dir.Argv[:8], " ") != "install -d -m 0755 -o root -g wheel" || dir.Argv[8] != filepath.Dir(d.AdminRecordFile) {
		t.Errorf("directory command: %v", dir)
	}
	a := inst.Argv
	if !inst.Sudo || len(a) != 9 || strings.Join(a[:7], " ") != "install -m 0644 -o root -g wheel" || a[8] != d.AdminRecordFile {
		t.Errorf("install command: %v", inst)
	}
	if got := (Deps{}).adminRecordFile(); got != "/etc/whr/admin.json" {
		t.Errorf("default path %q", got)
	}
	if err := os.Remove(d.ConfigPath); err != nil {
		t.Fatal(err)
	}
	st, msg := status(steps(t, d)["admin-record"])
	if st != NotVerified || !strings.HasPrefix(msg, needsConfig) {
		t.Errorf("missing config: %s %q", st, msg)
	}
}
