package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scripted is a Runner that answers from a table: no test touches the machine.
type scripted map[string]string

func (s scripted) Output(_ context.Context, argv ...string) ([]byte, error) {
	if out, ok := s[strings.Join(argv, " ")]; ok {
		// "ERR:" scripts a failure, with what the command said on stderr
		if msg, failed := strings.CutPrefix(out, "ERR:"); failed {
			return nil, errors.New(msg)
		}
		return []byte(out), nil
	}
	return nil, errors.New("exit status 1")
}

// answers is a Prompter that gives the next prepared answer.
type answers struct {
	lines, secrets []string
	confirm        bool
	shown          []string
}

func (a *answers) Line(string) (string, error) {
	l := a.lines[0]
	a.lines = a.lines[1:]
	return l, nil
}

func (a *answers) Secret(string) (string, error) {
	s := a.secrets[0]
	a.secrets = a.secrets[1:]
	return s, nil
}
func (a *answers) Confirm(string) (bool, error) { return a.confirm, nil }
func (a *answers) Show(t string)                { a.shown = append(a.shown, t) }

func steps(t *testing.T, d Deps) map[string]Check {
	t.Helper()
	m := map[string]Check{}
	for _, c := range Checks(d) {
		if _, dup := m[c.Name]; dup {
			t.Fatalf("two checks are named %q", c.Name)
		}
		m[c.Name] = c
	}
	return m
}

func hostDeps(r Runner) Deps {
	return Deps{ConfigPath: "/Users/whr/.config/whr/config.json", Home: "/Users/whr", GOOS: "darwin", Runner: r, User: "werner", UID: 501, Prefix: "/nonexistent/opt/whr"}
}

func status(c Check) (Status, string) { return c.Run(context.Background()) }

func TestTheHostChecksReadWhatMacOSPrints(t *testing.T) {
	good := scripted{
		"dscl . -read /Users/whr UniqueID":        "UniqueID: 502",
		"dseditgroup -o checkmember -m whr admin": "no whr is NOT a member of admin",
		"pmset -g": " sleep                0\n disksleep            0\n autorestart          1\n womp                 1\n powernap             0\n",
		"/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate": "Firewall is enabled. (State = 1)",
		"/usr/libexec/ApplicationFirewall/socketfilterfw --getstealthmode": "Stealth mode enabled",
		"fdesetup status":                                            "FileVault is On.",
		"/opt/homebrew/bin/brew --version":                           "Homebrew 5.0",
		"/opt/homebrew/bin/brew list --formula --versions container": "container 1.5.0",
		"/opt/homebrew/bin/brew list --formula --versions git":       "git 2.50",
		"/opt/homebrew/bin/brew list --formula --versions gh":        "gh 2.80",
		"/opt/homebrew/bin/brew list --pinned":                       "container\n",
	}
	st := steps(t, hostDeps(good))
	for _, name := range []string{"whr-user", "power", "firewall", "filevault", "homebrew", "brew-packages", "brew-pin"} {
		if got, detail := status(st[name]); got != OK {
			t.Errorf("%s = %s %q on a set-up Mac", name, got, detail)
		}
	}

	// each way to be wrong is a failure that says what is wrong
	bad := scripted{
		"dscl . -read /Users/whr UniqueID":        "UniqueID: 502",
		"dseditgroup -o checkmember -m whr admin": "yes whr is a member of admin",
		"pmset -g":        " sleep 10\n disksleep 10\n autorestart 0\n powernap 1\n",
		"fdesetup status": "FileVault is Off.",
		"/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate": "Firewall is disabled. (State = 0)",
		"/usr/libexec/ApplicationFirewall/socketfilterfw --getstealthmode": "Stealth mode disabled",
		"/opt/homebrew/bin/brew --version":                                 "Homebrew 5.0",
		"/opt/homebrew/bin/brew list --pinned":                             "git\n",
	}
	st = steps(t, hostDeps(bad))
	want := map[string]string{
		"whr-user": "administrator", "power": "sleep is 10, want 0", "firewall": "stealth mode is off",
		"filevault": "off", "brew-packages": "not installed: container, git, gh", "brew-pin": "not pinned",
	}
	for name, sub := range want {
		got, detail := status(st[name])
		if got != Fail || !strings.Contains(detail, sub) {
			t.Errorf("%s = %s %q, want a failure that says %q", name, got, detail, sub)
		}
	}
	if got, detail := status(steps(t, hostDeps(scripted{}))["whr-user"]); got != Fail || !strings.Contains(detail, "no user") {
		t.Errorf("a missing user = %s %q", got, detail)
	}
}

// Off a Mac nothing is run and nothing is claimed: the steps say not verified.
func TestOffAMacTheStepsAreNotVerified(t *testing.T) {
	d := hostDeps(scripted{})
	d.GOOS = "linux"
	for _, c := range Checks(d) {
		if c.Phase == "" || c.Name == "config-dir" || c.Name == "api-token" || c.Name == "agent-key" || c.Name == "ssh-ca" || c.Name == "config-base" || c.Name == "config-github" || c.Name == "github-app" || c.Name == "tool-store" || c.Name == "prefix" {
			continue
		}
		if got, _ := status(c); got != NotVerified && got != OK {
			t.Errorf("%s = %s off a Mac, want not_verified", c.Name, got)
		}
	}
}

func TestEveryFixIsArgvAndRootOwnedFilesGoThroughInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // the private setup directory lies in the home
	d := hostDeps(scripted{})
	d.Whr = "/opt/whr/bin/whr"
	for _, c := range Checks(d) {
		if c.Fix == nil {
			continue
		}
		for _, cmd := range c.Fix.Cmds {
			if len(cmd.Argv) == 0 {
				t.Errorf("%s: an empty command", c.Name)
				continue
			}
			first := filepath.Base(cmd.Argv[0])
			if first == "sh" || first == "bash" || first == "zsh" || first == "tee" || cmd.Argv[0] == "-c" {
				t.Errorf("%s: %v goes through a shell or a redirect", c.Name, cmd.Argv)
			}
			for _, a := range cmd.Argv {
				if c.Fix.Build != nil && strings.HasPrefix(a, "<") && strings.HasSuffix(a, ">") {
					continue // a placeholder in the preview of a command that is built after the question
				}
				if strings.ContainsAny(a, "|;&><`") || strings.Contains(a, "$(") {
					t.Errorf("%s: %q looks like shell syntax in an argument", c.Name, a)
				}
			}
			if cmd.Argv[0] == "sudo" {
				t.Errorf("%s: sudo is the Sudo flag, not part of the argv", c.Name)
			}
		}
	}
	// the sshd file: written to a private temp file, then installed as root's, mode 0644
	ssh := steps(t, d)["ssh-keys-only"].Fix
	want := []string{"install", "-m", "0644", "-o", "root", "-g", "wheel", sshdTemp(), sshdFile}
	if len(ssh.Cmds) != 1 || !ssh.Cmds[0].Sudo || strings.Join(ssh.Cmds[0].Argv, " ") != strings.Join(want, " ") {
		t.Errorf("ssh fix %+v, want sudo %v", ssh.Cmds, want)
	}
	if err := ssh.Do(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(sshdTemp())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("temp file %v %v, want 0600", fi, err)
	}
	if di, _ := os.Stat(filepath.Dir(sshdTemp())); di.Mode().Perm() != 0o700 {
		t.Errorf("temp dir %v, want 0700", di.Mode().Perm())
	}
	b, _ := os.ReadFile(sshdTemp())
	if string(b) != sshdContent {
		t.Errorf("content %q", b)
	}
	if got := strings.Join(steps(t, d)["power"].Fix.Cmds[0].Argv, " "); got != "pmset -a sleep 0 disksleep 0 autorestart 1 womp 1 powernap 0" {
		t.Errorf("power fix = %q", got)
	}
	// the privileged commands of the host part are all separate argvs with Sudo set
	for _, name := range []string{"whr-user", "power", "firewall", "prefix"} {
		for _, cmd := range steps(t, d)[name].Fix.Cmds {
			if !cmd.Sudo {
				t.Errorf("%s: a privileged command is not marked Sudo: %v", name, cmd.Argv)
			}
		}
	}
	// FileVault is guided: its recovery key must never pass through whr
	if fv := steps(t, d)["filevault"].Fix; len(fv.Cmds) != 0 || fv.Do != nil || !strings.Contains(fv.Guide, "recovery key") {
		t.Errorf("filevault fix %+v", fv)
	}
}

func TestSecretsAreGeneratedOrTypedAndWrittenPrivatelyWithoutOverwriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "whr")
	d := Deps{ConfigPath: filepath.Join(dir, "config.json"), Home: t.TempDir(), GOOS: "darwin", Runner: scripted{}, User: "whr", UID: 502}
	st := steps(t, d)
	ctx := context.Background()

	if err := st["config-dir"].Fix.Do(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := status(steps(t, d)["config-dir"]); got != OK {
		t.Error("the directory is not private after its fix")
	}
	if err := st["api-token"].Fix.Do(ctx, nil); err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(dir, "api.token")
	fi, err := os.Stat(tok)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token %v %v", fi, err)
	}
	first, _ := os.ReadFile(filepath.Clean(tok))
	if len(strings.TrimSpace(string(first))) < 40 {
		t.Errorf("a token of %d characters", len(first))
	}
	if err := st["api-token"].Fix.Do(ctx, nil); err == nil || !strings.Contains(err.Error(), "not overwritten") {
		t.Errorf("a second token = %v, want a refusal", err)
	}
	if again, _ := os.ReadFile(filepath.Clean(tok)); string(again) != string(first) {
		t.Error("the token was overwritten")
	}
	if got, _ := status(steps(t, d)["api-token"]); got != OK {
		t.Error("the token check fails after its fix")
	}

	// the SSH authority is optional: absent is fine, made it is a private key, never overwritten
	if got, detail := status(steps(t, d)["ssh-ca"]); got != OK || !strings.Contains(detail, "off") {
		t.Errorf("no authority = %s %q", got, detail)
	}
	if err := st["ssh-ca"].Fix.Do(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(dir, "ssh-ca")
	if fi, err := os.Stat(ca); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("authority key %v %v", fi, err)
	}
	if got, detail := status(steps(t, d)["ssh-ca"]); got != OK || !strings.Contains(detail, "console.ssh_ca_key_file") {
		t.Errorf("authority = %s %q", got, detail)
	}
	if err := st["ssh-ca"].Fix.Do(ctx, nil); err == nil {
		t.Error("a second authority key overwrote the first")
	}

	// the API key is typed, never echoed, never shown
	key := "sk-ant-api03-" + strings.Repeat("x", 30)
	p := &answers{secrets: []string{key}}
	if err := st["agent-key"].Fix.Do(ctx, p); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, "agent.env")
	if fi, _ := os.Stat(env); fi.Mode().Perm() != 0o600 {
		t.Errorf("agent.env mode %v", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(filepath.Clean(env)); string(b) != "ANTHROPIC_API_KEY="+key+"\n" {
		t.Errorf("agent.env %q", b)
	}
	if strings.Contains(strings.Join(p.shown, ""), key) {
		t.Error("the key was shown")
	}
	if err := st["agent-key"].Fix.Do(ctx, &answers{secrets: []string{key}}); err == nil {
		t.Error("an existing key file was overwritten")
	}
	for _, bad := range []string{"short", "has space in it 1234567890", "a=b1234567890123456789"} {
		_ = os.Remove(env)
		if err := st["agent-key"].Fix.Do(ctx, &answers{secrets: []string{bad}}); err == nil {
			t.Errorf("%q was accepted as a key", bad)
		}
		if _, err := os.Stat(env); err == nil {
			t.Errorf("%q left a file behind", bad)
		}
	}
}

// The configuration is written in two steps, because `whr github app create`
// needs the first part and the App needs that command: no cycle, and the wizard
// edits an existing file as a diff after a y, atomically, keeping unknown keys.
func TestTheConfigurationIsWrittenInTwoStepsWithoutADropOfAnythingUnknown(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "whr")
	d := Deps{ConfigPath: filepath.Join(dir, "config.json"), Home: home, GOOS: "darwin", Runner: scripted{}, User: "whr", UID: 502}
	ctx := context.Background()
	st := steps(t, d)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st["config-dir"].Fix.Do(ctx, nil))
	must(st["api-token"].Fix.Do(ctx, nil))

	if got, _ := status(st["config-base"]); got != Fail {
		t.Fatal("no config file yet")
	}
	if err := st["config-base"].Fix.Do(ctx, &answers{lines: []string{"not a repo", ""}}); err == nil {
		t.Error("a bad repository name was written")
	}
	if _, err := os.Stat(d.ConfigPath); err == nil {
		t.Fatal("a refused answer left a file")
	}
	must(st["config-base"].Fix.Do(ctx, &answers{lines: []string{"wstein/workharbor", ""}}))
	fi, _ := os.Stat(d.ConfigPath)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", fi.Mode().Perm())
	}
	if got, detail := status(steps(t, d)["config-base"]); got != OK {
		t.Errorf("config-base = %s %q after its fix", got, detail)
	}
	if got, _ := status(steps(t, d)["config-github"]); got != Fail {
		t.Error("the base file is not a whole configuration: config-github must still fail")
	}
	if err := st["config-base"].Fix.Do(ctx, &answers{lines: []string{"a/b", ""}}); err == nil {
		t.Error("an existing configuration was overwritten")
	}

	// no App key yet
	if err := st["config-github"].Fix.Do(ctx, &answers{confirm: true}); err == nil || !strings.Contains(err.Error(), "github-app") {
		t.Errorf("config-github without an App = %v", err)
	}
	// a key the wizard does not know in the file, and the App's key beside it
	var m map[string]any
	raw, _ := os.ReadFile(d.ConfigPath)
	must(json.Unmarshal(raw, &m))
	m["a_key_from_the_future"] = map[string]any{"x": 1}
	raw, _ = json.MarshalIndent(m, "", "  ")
	must(os.WriteFile(d.ConfigPath, raw, 0o600))
	must(os.WriteFile(filepath.Join(dir, "github-app-77.pem"), []byte("-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----\n"), 0o600))

	// declined: nothing changes
	decline := &answers{confirm: false}
	if err := st["config-github"].Fix.Do(ctx, decline); err == nil {
		t.Error("a declined change succeeded")
	}
	if after, _ := os.ReadFile(d.ConfigPath); string(after) != string(raw) {
		t.Error("a declined change was written")
	}
	if len(decline.shown) != 1 || !strings.Contains(decline.shown[0], `+     "app_id": 77`) {
		t.Errorf("the diff shown: %v", decline.shown)
	}
	// confirmed: written atomically, keeping the unknown key
	must(st["config-github"].Fix.Do(ctx, &answers{confirm: true}))
	after, _ := os.ReadFile(d.ConfigPath)
	if !strings.Contains(string(after), "a_key_from_the_future") || !strings.Contains(string(after), `"app_id": 77`) {
		t.Errorf("the edited configuration lost a key or lacks the App:\n%s", after)
	}
	if fi, _ := os.Stat(d.ConfigPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v after the edit", fi.Mode().Perm())
	}
	if ents, _ := os.ReadDir(dir); func() bool {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), ".whr-config-") {
				return true
			}
		}
		return false
	}() {
		t.Error("a temporary file was left beside the configuration")
	}
}

func TestStepNamesAreKebabCaseInTheWizardsOrderWithoutACycle(t *testing.T) {
	kebab := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	all := Checks(hostDeps(scripted{}))
	for _, c := range all {
		if !kebab.MatchString(c.Name) {
			t.Errorf("%q is not kebab-case", c.Name)
		}
		if c.Title == "" && c.Phase != "" {
			t.Errorf("step %s has no title", c.Name)
		}
	}
	var user []string
	for _, c := range Steps(all, PhaseUser) {
		user = append(user, c.Name)
	}
	want := "config-dir api-token agent-key ssh-ca container-kernel container-start standard-user-check config-base github-app config-github tool-store service-install"
	if strings.Join(user, " ") != want {
		t.Errorf("user steps %v\nwant %s", user, want)
	}
	if len(Steps(all, PhaseHost)) < 8 {
		t.Errorf("host steps %d", len(Steps(all, PhaseHost)))
	}
	if len(Shared(all)) == 0 || len(Shared(all))+len(Steps(all, PhaseHost))+len(Steps(all, PhaseUser)) != len(all) {
		t.Error("every check is shared or a step of one phase")
	}
}

// The file root installs is never written where another account could have
// prepared the directory or a link: a directory that is loose or a link is
// refused, and a link in place of the file is replaced, not followed.
func TestWriteTempRefusesADirectoryOthersControl(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := setupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a test making the directory too open
		t.Fatal(err)
	}
	if err := writeTemp(sshdTemp(), "x"); err == nil {
		t.Error("a 0755 directory was accepted")
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a directory, 0700 is private
		t.Fatal(err)
	}
	victim := filepath.Join(home, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, sshdTemp()); err != nil {
		t.Fatal(err)
	}
	if err := writeTemp(sshdTemp(), "new"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" { //nolint:gosec // a file the test made
		t.Errorf("the link was followed: victim now %q", b)
	}
	if fi, err := os.Lstat(sshdTemp()); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Errorf("the temp file is not a fresh 0600 file: %v %v", fi, err)
	}

	other := filepath.Join(home, "elsewhere")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, dir); err != nil {
		t.Fatal(err)
	}
	if err := writeTemp(sshdTemp(), "x"); err == nil {
		t.Error("a linked directory was accepted")
	}
}

const autologoutKey = "defaults read /Library/Preferences/.GlobalPreferences com.apple.autologout.AutoLogOutDelay"

func TestAutomaticLogOutIsOffWhenTheKeyIsAbsentOrZero(t *testing.T) {
	for name, tc := range map[string]struct {
		out    scripted
		want   Status
		detail string
	}{
		"not set (defaults says so)": {scripted{autologoutKey: "ERR:exit status 1: The domain/default pair of (/Library/Preferences/.GlobalPreferences, com.apple.autologout.AutoLogOutDelay) does not exist"}, OK, "not set"},
		"defaults fails otherwise":   {scripted{autologoutKey: "ERR:signal: killed"}, NotVerified, "not known"},
		"no answer at all":           {scripted{}, NotVerified, "not known"},
		"zero":                       {scripted{autologoutKey: "0\n"}, OK, "off"},
		"ten minutes":                {scripted{autologoutKey: "600\n"}, Fail, "after 600 seconds"},
	} {
		got, detail := status(steps(t, hostDeps(tc.out))["autologout"])
		if got != tc.want || !strings.Contains(detail, tc.detail) {
			t.Errorf("%s: %s %q, want %s with %q", name, got, detail, tc.want, tc.detail)
		}
	}
	d := hostDeps(scripted{})
	d.GOOS = "linux"
	if got, _ := status(steps(t, d)["autologout"]); got != NotVerified {
		t.Errorf("off a Mac: %s", got)
	}
	// the fix is a guide, never a command: whr changes no login setting
	fix := steps(t, hostDeps(scripted{}))["autologout"].Fix
	if fix == nil || len(fix.Cmds) != 0 || fix.Guide == "" || fix.Open == "" {
		t.Errorf("fix = %+v", fix)
	}
}

const (
	goodVolume = "   Volume Name:               ssd\n   FileVault:                 Yes (Unlocked)\n   Owners:                    Enabled\n"
	noOwners   = "   FileVault:                 Yes\n   Owners:                    Disabled\n"
	plain      = "   FileVault:                 No\n   Owners:                    Enabled\n"
)

// writeRoots writes a configuration whose workspace roots are the given
// directories, which must exist: a root is resolved before its disk is asked.
func writeRoots(t *testing.T, roots ...string) Deps {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"roots": map[string]any{"workspaces": roots}})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	d := hostDeps(nil)
	d.ConfigPath = path
	return d
}

// root makes a directory and returns its resolved path.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// onDisk scripts `df -P` saying a directory is on a mount point.
func onDisk(dir, mount string) (string, string) {
	return "df -P " + dir, "Filesystem 512-blocks Used Available Capacity Mounted on\n/dev/disk5s1 100 1 99 1% " + mount + "\n"
}

func TestMediaAnalysisFailsOnlyOnMeasuredCPU(t *testing.T) {
	d := hostDeps(nil)
	cache := "du -sk /Users/whr/Library/Caches/com.apple.mediaanalysisd"
	check := func(r scripted) (Status, string) {
		d.Runner = r
		return status(steps(t, d)["media-analysis"])
	}
	const ps = "ps -axo pcpu=,time=,comm="
	if st, detail := check(scripted{ps: " 12.0 1:02.03 /usr/sbin/cfprefsd\n", cache: "2048\tx"}); st != OK || !strings.Contains(detail, "not running") || !strings.Contains(detail, "2 MiB") {
		t.Errorf("no process: %s %q", st, detail)
	}
	if st, detail := check(scripted{ps: " 3.0 0:10.00 /System/Library/PrivateFrameworks/MediaAnalysis.framework/Versions/A/mediaanalysisd\n"}); st != OK || !strings.Contains(detail, "3%") {
		t.Errorf("idle: %s %q", st, detail)
	}
	if st, detail := check(scripted{ps: " 222.0 21:35:00 /System/Library/PrivateFrameworks/MediaAnalysis.framework/Versions/A/mediaanalysisd\n", cache: "9437184\tx"}); st != Fail || !strings.Contains(detail, "222%") || !strings.Contains(detail, "21:35:00") || !strings.Contains(detail, "9216 MiB") {
		t.Errorf("busy: %s %q", st, detail)
	}
	if st, _ := check(scripted{}); st != NotVerified {
		t.Errorf("ps that does not answer: %s", st)
	}
	if st, _ := check(scripted{ps: " x 1:00 mediaanalysisd\n"}); st != NotVerified {
		t.Errorf("unreadable ps: %s", st)
	}
	fix := steps(t, d)["media-analysis"].Fix
	if len(fix.Cmds) != 0 || fix.Build != nil || fix.Do != nil || fix.Open == "" || strings.Contains(fix.Guide, "killall ") {
		t.Errorf("the fix must be guided only: %+v", fix)
	}
}

func TestSpotlightIsOffOnWorkspaceVolumes(t *testing.T) {
	a := root(t)
	ka, va := onDisk(a, "/Volumes/ssd")
	d := writeRoots(t, a)
	check := func(r scripted) (Status, string) {
		d.Runner = r
		return status(steps(t, d)["spotlight"])
	}
	if st, _ := check(scripted{ka: va, "mdutil -s /Volumes/ssd": "/Volumes/ssd:\n\tIndexing disabled.\n"}); st != OK {
		t.Errorf("indexing off: %s", st)
	}
	on := scripted{ka: va, "mdutil -s /Volumes/ssd": "/Volumes/ssd:\n\tIndexing enabled.\n"}
	if st, detail := check(on); st != Fail || !strings.Contains(detail, "/Volumes/ssd") {
		t.Errorf("indexing on: %s %q", st, detail)
	}
	if st, _ := check(scripted{ka: va}); st != NotVerified {
		t.Errorf("mdutil that does not answer: %s", st)
	}
	ki, vi := onDisk(a, "/System/Volumes/Data")
	if st, detail := check(scripted{ki: vi}); st != NotVerified || !strings.Contains(detail, "internal disk") {
		t.Errorf("internal root: %s %q", st, detail)
	}
	d.Runner = on
	cmds, err := steps(t, d)["spotlight"].Fix.Build(context.Background(), nil)
	if err != nil || len(cmds) != 1 || !cmds[0].Sudo || strings.Join(cmds[0].Argv, " ") != "mdutil -i off /Volumes/ssd" {
		t.Errorf("fix commands = %+v, %v", cmds, err)
	}
}

func TestAWorkspaceVolumeMustBeEncryptedAndHonourOwnership(t *testing.T) {
	check := func(out scripted, roots ...string) (Status, string) {
		d := writeRoots(t, roots...)
		d.Runner = out
		return status(steps(t, d)["workspace-volume"])
	}
	a, b := root(t), root(t)
	ka, va := onDisk(a, "/Volumes/ssd")
	kb, vb := onDisk(b, "/Volumes/ssd")
	if st, detail := check(scripted{ka: va, kb: vb, "diskutil info /Volumes/ssd": goodVolume}, a, b); st != OK {
		t.Errorf("a good volume: %s %q", st, detail)
	}
	ki, vi := onDisk(a, "/System/Volumes/Data")
	if st, detail := check(scripted{ki: vi}, a); st != OK || !strings.Contains(detail, "internal disk") {
		t.Errorf("roots on the internal disk: %s %q", st, detail)
	}
	for name, tc := range map[string]struct{ out, want string }{
		"no ownership":  {noOwners, "ignores file ownership"},
		"not encrypted": {plain, "is not encrypted"},
	} {
		if st, detail := check(scripted{ka: va, "diskutil info /Volumes/ssd": tc.out}, a); st != Fail || !strings.Contains(detail, tc.want) {
			t.Errorf("%s: %s %q", name, st, detail)
		}
	}
	if st, _ := check(scripted{ka: va}, a); st != NotVerified {
		t.Errorf("diskutil that does not answer: %s", st)
	}
	// a mount point that holds spaces is read whole
	ks, vs := onDisk(a, "/Volumes/My SSD")
	if st, detail := check(scripted{ks: vs, "diskutil info /Volumes/My SSD": plain}, a); st != Fail || !strings.Contains(detail, "/Volumes/My SSD is not encrypted") {
		t.Errorf("a mount point with a space: %s %q", st, detail)
	}
	// df that does not answer, or a root that is not there: not a pass
	if st, _ := check(scripted{}, a); st != NotVerified {
		t.Errorf("df that does not answer: %s", st)
	}
	if st, _ := check(scripted{}, filepath.Join(a, "missing")); st != NotVerified {
		t.Errorf("a root that does not exist: %s", st)
	}
	// a link to an external disk is that disk, not the internal one
	link := filepath.Join(root(t), "ws")
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	if st, detail := check(scripted{ka: va, "diskutil info /Volumes/ssd": plain}, link); st != Fail || !strings.Contains(detail, "is not encrypted") {
		t.Errorf("a root linked to an unencrypted external disk: %s %q", st, detail)
	}
	// without a configuration the roots are not known: not verified, never a pass
	d := hostDeps(scripted{})
	d.ConfigPath = filepath.Join(t.TempDir(), "none.json")
	if st, _ := status(steps(t, d)["workspace-volume"]); st != NotVerified {
		t.Errorf("no configuration: %s", st)
	}

	// the fix turns ownership on for the volumes that need it, as sudo argv, and
	// leaves encryption to the human
	c := root(t)
	kc, vc := onDisk(c, "/Volumes/good")
	d = writeRoots(t, a, c)
	d.Runner = scripted{ka: va, kc: vc, "diskutil info /Volumes/ssd": noOwners, "diskutil info /Volumes/good": goodVolume}
	fix := steps(t, d)["workspace-volume"].Fix
	cmds, err := fix.Build(context.Background(), nil)
	if err != nil || len(cmds) != 1 || !cmds[0].Sudo || strings.Join(cmds[0].Argv, " ") != "diskutil enableOwnership /Volumes/ssd" {
		t.Errorf("fix commands = %+v, %v", cmds, err)
	}
	if !strings.Contains(fix.Guide, "Encryption is not") {
		t.Errorf("the guide does not leave encryption to the human: %q", fix.Guide)
	}
}

// recording is a scripted runner that remembers every command it was asked.
type recording struct {
	scripted
	calls []string
}

func (r *recording) Output(ctx context.Context, argv ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(argv, " "))
	return r.scripted.Output(ctx, argv...)
}

// Outside the Aqua session (SSH) the container checks are not_verified and run
// no container command; in Aqua they behave as before (#156).
func TestContainerChecksNeedTheDesktopSession(t *testing.T) {
	names := []string{"container-kernel", "container-start", "standard-user-check"}
	for _, session := range []string{"Background", "System"} {
		r := &recording{scripted: scripted{"launchctl managername": session}}
		st := steps(t, hostDeps(r))
		for _, name := range names {
			got, detail := status(st[name])
			if got != NotVerified || !strings.Contains(detail, "whr's desktop session") || !strings.Contains(detail, "whr status") {
				t.Errorf("%s over %s = %s %q", name, session, got, detail)
			}
		}
		for _, c := range r.calls {
			if strings.HasPrefix(c, "container") {
				t.Errorf("ran %q outside Aqua", c)
			}
		}
	}
	r := &recording{scripted: scripted{
		"launchctl managername":   "Aqua",
		"container system status": "apiserver is running",
		"container list --all":    "",
		"launchctl print gui/501": "com.apple.container.apiserver",
	}}
	st := steps(t, hostDeps(r))
	for _, name := range names {
		if got, detail := status(st[name]); got != OK {
			t.Errorf("%s in Aqua = %s %q", name, got, detail)
		}
	}
}
