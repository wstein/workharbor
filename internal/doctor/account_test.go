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
	adminKey   = "dseditgroup -o checkmember -m workharbor admin"
	groupKey   = "dscl . -read /Groups/admin GroupMembership"
	isAdmin    = "yes workharbor is a member of admin"
	notAdmin   = "no workharbor is NOT a member of admin"
	oneOtherAd = "GroupMembership: root werner workharbor\n"
)

// accountDeps writes a configuration with the given keys and returns deps over it.
func accountDeps(t *testing.T, r scripted, extra map[string]any) Deps {
	t.Helper()
	// the host's remote logins are off and werner exists, unless a test says otherwise
	for _, k := range []string{"launchctl print system/com.openssh.sshd", "launchctl print system/com.apple.screensharing"} {
		if _, ok := r[k]; !ok {
			r[k] = "ERR:Could not find service in domain for system"
		}
	}
	if _, ok := r["dscl . -read /Users/werner UniqueID"]; !ok {
		r["dscl . -read /Users/werner UniqueID"] = "UniqueID: 501"
	}
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
		"only root and the account": "GroupMembership: root workharbor\\n",
		"only the account":          "GroupMembership: workharbor\\n",
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
	if strings.Join(cmds[0].Full(), " ") != "sudo dseditgroup -o edit -d workharbor -t user admin" || strings.Join(cmds[1].Full(), " ") != "sudo -k" {
		t.Errorf("commands = %v, %v", cmds[0].Full(), cmds[1].Full())
	}
	// the refusal is checked again after the confirmation
	for name, d := range map[string]Deps{
		"lock-out": accountDeps(t, scripted{adminKey: isAdmin, groupKey: "GroupMembership: root workharbor\n"}, nil),
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

func TestPrefixOwnedByTheConfiguredAccountWarns(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	// an administrator configured account owning the prefix warns (alpha, #493)
	d := hostDeps(scripted{adminKey: isAdmin})
	d.Prefix = t.TempDir()
	d.Account = me.Username
	got, detail := status(steps(t, d)["prefix"])
	if got != Warn || !strings.Contains(detail, "belongs to "+me.Username) || !strings.Contains(detail, "revisit at beta") {
		t.Errorf("own prefix = %s %q", got, detail)
	}
	// another administrator owning it is fine
	d.Account = "whr-no-such-account"
	if got, detail := status(steps(t, d)["prefix"]); got != OK {
		t.Errorf("other owner = %s %q", got, detail)
	}
}

func TestBinaryOutsideThePrefixWarns(t *testing.T) {
	d := hostDeps(scripted{adminKey: isAdmin})
	d.Prefix = t.TempDir()
	d.Account = "whr-no-such-account"
	d.Whr = filepath.Join(t.TempDir(), "whr")
	got, detail := status(steps(t, d)["prefix"])
	if got != Warn || !strings.Contains(detail, "whr runs from "+d.Whr) || !strings.Contains(detail, "revisit at beta") {
		t.Errorf("binary outside the prefix = %s %q", got, detail)
	}
	d.Whr = filepath.Join(d.Prefix, "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(d.Whr), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.Whr, []byte("x"), 0o700); err != nil { //nolint:gosec // an executable stand-in
		t.Fatal(err)
	}
	if got, detail := status(steps(t, d)["prefix"]); got != OK {
		t.Errorf("binary inside the prefix = %s %q", got, detail)
	}
}

// The maintainer's flow: whr copied to /usr/local/bin, no /opt/whr at all. The
// doctor warns once and does not fail (#493).
func TestAMissingPrefixIsAWarnWhenWhrRunsFromElsewhere(t *testing.T) {
	d := hostDeps(scripted{adminKey: isAdmin})
	d.Prefix = filepath.Join(t.TempDir(), "opt", "whr")
	d.Account = "whr-no-such-account"
	d.Whr = filepath.Join(t.TempDir(), "whr")
	if got, detail := status(steps(t, d)["prefix"]); got != Warn || !strings.Contains(detail, "revisit at beta") {
		t.Errorf("missing prefix, whr elsewhere = %s %q", got, detail)
	}
	d.Whr = filepath.Join(d.Prefix, "bin", "whr")
	if got, _ := status(steps(t, d)["prefix"]); got != Fail {
		t.Errorf("missing prefix, whr in it = %s", got)
	}
}

// serve finds the guest helpers next to the running binary, so the doctor looks
// there, not under the prefix.
func TestGuestHelpersAreLookedForNextToTheRunningBinary(t *testing.T) {
	d := hostDeps(nil)
	dir := t.TempDir()
	d.Whr = filepath.Join(dir, "bin", "whr")
	d.Prefix = "/opt/whr"
	if err := os.MkdirAll(filepath.Dir(d.Whr), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.Whr, []byte("x"), 0o700); err != nil { //nolint:gosec // an executable stand-in
		t.Fatal(err)
	}
	want := filepath.Join(dir, "libexec", "whr")
	if got := d.libexec(); got != want && !strings.HasSuffix(got, "/libexec/whr") {
		t.Errorf("libexec = %s, want %s", got, want)
	}
	if strings.HasPrefix(d.libexec(), "/opt/whr") {
		t.Errorf("libexec follows the prefix: %s", d.libexec())
	}
}

func TestPrefixFixArgv(t *testing.T) {
	d := hostDeps(scripted{})
	d.Prefix = "/opt/whr"
	d.User, d.Account = "workharbor", "workharbor"
	if got := strings.Join(d.prefixInstallArgv(), " "); got != "install -d -o root -g wheel -m 755 /opt/whr" {
		t.Errorf("same account: %s", got)
	}
	d.User = "boss"
	if got := strings.Join(d.prefixInstallArgv(), " "); got != "install -d -o boss -g admin -m 755 /opt/whr" {
		t.Errorf("other account: %s", got)
	}
}

func TestAccountReviewFindings157(t *testing.T) {
	acct := func(r scripted, extra map[string]any) (Status, string) {
		return status(steps(t, accountDeps(t, r, extra))["account"])
	}
	// an unreadable launchctl answer is not "off": no warn, but not verified
	got, detail := acct(scripted{adminKey: isAdmin, "launchctl print system/com.openssh.sshd": "ERR:operation not permitted"}, nil)
	if got != NotVerified {
		t.Errorf("unreadable launchctl: %s %q", got, detail)
	}
	// a missing launchctl binary and an empty exit-0 answer are unknown, not off
	for _, ans := range []string{`ERR:exec: "launchctl": executable file not found in $PATH`, ""} {
		got, detail = acct(scripted{adminKey: isAdmin, "launchctl print system/com.openssh.sshd": ans}, nil)
		if got != NotVerified {
			t.Errorf("launchctl answer %q: %s %q, want not_verified", ans, got, detail)
		}
	}
	// a remote login that is on still fails whatever else is unreadable
	got, _ = acct(scripted{adminKey: isAdmin, "launchctl print system/com.openssh.sshd": "ERR:boom", "launchctl print system/com.apple.screensharing": "service = x"}, nil)
	if got != Fail {
		t.Errorf("screen sharing on: %s", got)
	}
	// any account value but absent or "dedicated" is shared; drop-admin is never offered
	for _, v := range []any{"Shared", "shared ", true, "dedicatd", 1} {
		extra := map[string]any{"account": v}
		if got, _ := acct(scripted{adminKey: notAdmin}, extra); got != Warn {
			t.Errorf("account %v: %s, want warn (shared)", v, got)
		}
		got, detail := status(steps(t, accountDeps(t, scripted{adminKey: isAdmin, groupKey: oneOtherAd}, extra))["drop-admin"])
		if got != Skipped {
			t.Errorf("drop-admin on account %v: %s %q", v, got, detail)
		}
	}
	if got, _ := acct(scripted{adminKey: notAdmin}, map[string]any{"account": "dedicated"}); got != OK {
		t.Errorf("dedicated: %s", got)
	}
	// group names that are no account do not count as another administrator
	stale := scripted{
		adminKey: isAdmin, groupKey: "GroupMembership: root ghost root2 whr\n",
		"dscl . -read /Users/ghost UniqueID": "ERR:eDSRecordNotFound", "dscl . -read /Users/root2 UniqueID": "UniqueID: 0",
	}
	if got, detail := status(steps(t, accountDeps(t, stale, nil))["drop-admin"]); got != Skipped {
		t.Errorf("stale names: %s %q", got, detail)
	}
	if cmds, err := steps(t, accountDeps(t, stale, nil))["drop-admin"].Fix.Build(t.Context(), nil); err == nil || cmds != nil {
		t.Errorf("stale names built %v", cmds)
	}
}

func TestWhrUserIsNotVerifiedWhenTheAdminStatusIsUnreadable(t *testing.T) {
	d := hostDeps(scripted{"dscl . -read /Users/workharbor UniqueID": "UniqueID: 502"})
	if got, detail := status(steps(t, d)["workharbor-user"]); got != NotVerified {
		t.Errorf("%s %q", got, detail)
	}
}

func TestMediaAnalysisCacheIsReadAsTheConfiguredAccount(t *testing.T) {
	d := hostDeps(scripted{"ps -axo user=,pcpu=,time=,comm=": "x 0.0 0:00 /bin/ls"})
	d.Account, d.User = "bob", "bob"
	if _, detail := status(steps(t, d)["media-analysis"]); strings.Contains(detail, "not read from here") {
		t.Errorf("%q", detail)
	}
	d.User = "bob2"
	if _, detail := status(steps(t, d)["media-analysis"]); !strings.Contains(detail, "as bob") {
		t.Errorf("%q", detail)
	}
}

func TestOtherAdminsAreUnknownWhenDsclFailsForAMember(t *testing.T) {
	d := accountDeps(t, scripted{
		groupKey:                             "GroupMembership: root werner ghost\n",
		"dscl . -read /Users/ghost UniqueID": "ERR:exit status 1: Operation not permitted",
	}, nil)
	if _, err := d.otherAdmins(t.Context()); err == nil || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Errorf("err = %v, want one carrying dscl's text", err)
	}
}

func TestDropAdminIsIrreversible(t *testing.T) {
	c := steps(t, accountDeps(t, scripted{adminKey: isAdmin, groupKey: oneOtherAd}, nil))["drop-admin"]
	if c.Fix == nil || !c.Fix.Irreversible {
		t.Error("drop-admin must be marked Irreversible: Enter is no")
	}
}

// The prefix guide sends a first install to the release archive, not to a
// clone and make (which needs git, make and a prefix the account owns).
func TestPrefixInstallGuideNamesTheArchiveRoute(t *testing.T) {
	guide, try := prefixInstallGuide("/opt/whr")
	if !strings.Contains(guide, "release archive") || !strings.Contains(guide, "checksums.txt") {
		t.Errorf("guide = %q", guide)
	}
	if len(try) != 1 || try[0] != "sudo ./install.sh <tag>" || strings.Contains(guide+try[0], "make install-release") {
		t.Errorf("try = %q", try)
	}
	if _, try := prefixInstallGuide("/Volumes/x y/whr"); len(try) != 1 || try[0] != "sudo ./install.sh <tag> '/Volumes/x y/whr'" {
		t.Errorf("another prefix: %q", try)
	}
}
