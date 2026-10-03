package doctor

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

const (
	adminKey   = "dseditgroup -o checkmember -m whr admin"
	groupKey   = "dscl . -read /Groups/admin GroupMembership"
	isAdmin    = "yes whr is a member of admin"
	notAdmin   = "no whr is NOT a member of admin"
	oneOtherAd = "GroupMembership: root werner whr\n"
)

// accountDeps writes a configuration with the given keys and returns deps over it.
func accountDeps(t *testing.T, r scripted, extra map[string]any) Deps {
	t.Helper()
	m := map[string]any{
		"listen": "127.0.0.1:8787", "api_token_file": "/x/token",
		"repositories": []map[string]any{{"name": "a/b"}},
		"roots":        map[string]any{"workspaces": []string{"/x/ws"}},
		"github":       map[string]any{"app_id": 1, "key_file": "/x/key.pem"},
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	d := hostDeps(r)
	d.ConfigPath = path
	return d
}

func TestTheAccountCheckGradesTheAccountByAdminSharedAndRemoteAccess(t *testing.T) {
	remote := map[string]any{"public_url": "https://whr.example.ts.net"}
	for name, tc := range map[string]struct {
		out   scripted
		extra map[string]any
		want  Status
		sub   string
	}{
		"dedicated standard":            {scripted{adminKey: notAdmin}, nil, OK, "dedicated standard"},
		"dedicated standard, remote":    {scripted{adminKey: notAdmin}, remote, OK, "dedicated standard"},
		"administrator, no remote":      {scripted{adminKey: isAdmin}, nil, Warn, "drop-admin"},
		"administrator, remote":         {scripted{adminKey: isAdmin}, remote, Fail, "administrator"},
		"shared standard, no remote":    {scripted{adminKey: notAdmin}, map[string]any{"account": "shared"}, Warn, "your own account"},
		"shared admin, no remote":       {scripted{adminKey: isAdmin}, map[string]any{"account": "shared"}, Warn, "an administrator and your own"},
		"shared, remote":                {scripted{adminKey: notAdmin}, map[string]any{"account": "shared", "public_url": "https://whr.example.ts.net"}, Fail, "other devices"},
		"administrator, console ssh":    {scripted{adminKey: isAdmin}, map[string]any{"console": map[string]any{"ssh_ca_key_file": "/x/ca"}}, Fail, "other devices"},
		"unreadable dseditgroup answer": {scripted{adminKey: "ERR:exit status 1"}, nil, NotVerified, "unverified"},
	} {
		got, detail := status(steps(t, accountDeps(t, tc.out, tc.extra))["account"])
		if got != tc.want || !strings.Contains(detail, tc.sub) {
			t.Errorf("%s: %s %q, want %s with %q", name, got, detail, tc.want, tc.sub)
		}
	}
	// the host's own remote logins count as remote access, and the message says what is given up
	for _, lbl := range []string{"system/com.openssh.sshd", "system/com.apple.screensharing"} {
		got, detail := status(steps(t, accountDeps(t, scripted{adminKey: notAdmin, "launchctl print " + lbl: "service = x"}, map[string]any{"account": "shared"}))["account"])
		if got != Fail || !strings.Contains(detail, " is on") || !strings.Contains(detail, "read the API token") || !strings.Contains(detail, "replace the whr binary") {
			t.Errorf("%s: %s %q", lbl, got, detail)
		}
	}
	// a shared account has no drop-admin to suggest
	if _, detail := status(steps(t, accountDeps(t, scripted{adminKey: isAdmin}, map[string]any{"account": "shared"}))["account"]); strings.Contains(detail, "drop-admin") {
		t.Errorf("a shared account was pointed at drop-admin: %q", detail)
	}
	// without a usable configuration an administrator is not graded
	d := hostDeps(scripted{adminKey: isAdmin})
	if got, _ := status(steps(t, d)["account"]); got != NotVerified {
		t.Errorf("no configuration: %s", got)
	}
	d.GOOS = "linux"
	if got, _ := status(steps(t, d)["account"]); got != NotVerified {
		t.Errorf("off a Mac: %s", got)
	}
}

func TestWarnDoesNotFailTheExitCode(t *testing.T) {
	if Failed([]Result{{Status: Warn}, {Status: OK}}) {
		t.Error("a warning failed the run")
	}
	if !Failed([]Result{{Status: Warn}, {Status: Fail}}) {
		t.Error("a failure was lost beside a warning")
	}
}

func TestDropAdminRefusesWhenNoOtherAdministratorExists(t *testing.T) {
	check := func(r scripted, extra map[string]any) (Status, string) {
		return status(steps(t, accountDeps(t, r, extra))["drop-admin"])
	}
	if got, _ := check(scripted{adminKey: notAdmin}, nil); got != OK {
		t.Errorf("a standard user: %s", got)
	}
	got, detail := check(scripted{adminKey: isAdmin, groupKey: oneOtherAd}, nil)
	if got != Warn || !strings.Contains(detail, "werner") {
		t.Errorf("another administrator exists: %s %q", got, detail)
	}
	for name, out := range map[string]string{
		"only root and the account": "GroupMembership: root whr\\n",
		"only the account":          "GroupMembership: whr\\n",
		"unreadable":                "ERR:exit status 1",
		"unknown format":            "something else",
	} {
		out = strings.ReplaceAll(out, "\\n", "\n")
		got, detail := check(scripted{adminKey: isAdmin, groupKey: out}, nil)
		if got != Skipped || !strings.Contains(detail, "refused") {
			t.Errorf("%s: %s %q", name, got, detail)
		}
	}
	// never offered on a shared account, whoever else is an administrator
	got, detail = check(scripted{adminKey: isAdmin, groupKey: oneOtherAd}, map[string]any{"account": "shared"})
	if got != Skipped || !strings.Contains(detail, "not offered") {
		t.Errorf("shared: %s %q", got, detail)
	}
}

func TestDropAdminRunsOneDseditgroupAndSudoK(t *testing.T) {
	d := accountDeps(t, scripted{adminKey: isAdmin, groupKey: oneOtherAd}, nil)
	c := steps(t, d)["drop-admin"]
	if c.Optional || !c.FixOnWarn {
		t.Errorf("drop-admin is the last, offered step: %+v", c)
	}
	cmds, err := c.Fix.Build(t.Context(), nil)
	if err != nil || len(cmds) != 2 || !cmds[0].Sudo || !cmds[1].Sudo {
		t.Fatalf("commands = %+v, %v", cmds, err)
	}
	if strings.Join(cmds[0].Full(), " ") != "sudo dseditgroup -o edit -d whr -t user admin" || strings.Join(cmds[1].Full(), " ") != "sudo -k" {
		t.Errorf("commands = %v, %v", cmds[0].Full(), cmds[1].Full())
	}
	// the refusal is checked again after the confirmation
	for name, d := range map[string]Deps{
		"lock-out": accountDeps(t, scripted{adminKey: isAdmin, groupKey: "GroupMembership: root whr\n"}, nil),
		"shared":   accountDeps(t, scripted{adminKey: isAdmin, groupKey: oneOtherAd}, map[string]any{"account": "shared"}),
	} {
		if cmds, err := steps(t, d)["drop-admin"].Fix.Build(t.Context(), nil); err == nil || cmds != nil {
			t.Errorf("%s: built %v", name, cmds)
		}
	}
	if !strings.Contains(c.Fix.Guide, "Log out") {
		t.Error("the step does not tell the human to log out or restart")
	}
	// it is the last of the user steps, and a warn names the fix
	us := Steps(Checks(d), PhaseUser)
	if us[len(us)-1].Name != "drop-admin" {
		t.Errorf("last user step = %s", us[len(us)-1].Name)
	}
	rs := Run(t.Context(), []Check{c}, nil)
	if rs[0].Status != Warn || rs[0].Fix != "whr setup --only drop-admin" {
		t.Errorf("result = %+v", rs[0])
	}
}

func TestPrefixIsNeverOwnedByTheConfiguredAccount(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	// an administrator configured account owning the prefix fails
	d := hostDeps(scripted{adminKey: isAdmin})
	d.Prefix = t.TempDir()
	d.Account = me.Username
	got, detail := status(steps(t, d)["prefix"])
	if got != Fail || !strings.Contains(detail, "replace its own supervisor") {
		t.Errorf("own prefix = %s %q", got, detail)
	}
	// another administrator owning it is fine
	d.Account = "whr-no-such-account"
	if got, detail := status(steps(t, d)["prefix"]); got != OK {
		t.Errorf("other owner = %s %q", got, detail)
	}
}

func TestPrefixFixArgv(t *testing.T) {
	d := hostDeps(scripted{})
	d.Prefix = "/opt/whr"
	d.User, d.Account = "whr", "whr"
	if got := strings.Join(d.prefixInstallArgv(), " "); got != "install -d -o root -g wheel -m 755 /opt/whr" {
		t.Errorf("same account: %s", got)
	}
	d.User = "boss"
	if got := strings.Join(d.prefixInstallArgv(), " "); got != "install -d -o boss -g admin -m 755 /opt/whr" {
		t.Errorf("other account: %s", got)
	}
}
