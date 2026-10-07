package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/launchd"
	"github.com/wstein/workharbor/internal/setup"
)

func devSetupRig(t *testing.T) (*setupRig, string) {
	t.Helper()
	r := newSetupRig(t)
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.exe = filepath.Join(home, ".local", "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(r.exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
		t.Fatal(err)
	}
	r.env.Executable = func() (string, error) { return r.exe, nil }
	r.env.UID = os.Getuid() // the checks compare owners with the running account
	r.host.outputs["/usr/bin/dscl . -read /Users/werner NFSHomeDirectory"] = "NFSHomeDirectory: " + home + "\n"
	return r, home
}

func runDevSetup(t *testing.T, r *setupRig, home string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Setup: r.env,
		Getenv: func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		},
	}
	code := Execute(context.Background(), env, args)
	return code, out.String(), errOut.String()
}

func TestDevSetupSelectsTheSourceInstall(t *testing.T) {
	r, home := devSetupRig(t)
	base := []string{"setup", "--user", "werner", "--only", "config-base", "--dry-run"}
	code, _, errOut := runDevSetup(t, r, home, base...)
	if !strings.Contains(errOut, "not an installed binary") || !strings.Contains(errOut, "--dev") {
		t.Fatalf("managed mode must explain development install: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut = runDevSetup(t, r, home, append(base, "--dev")...)
	if code == exitcode.Usage || strings.Contains(errOut, "not an installed binary") || !strings.Contains(errOut, "development installation") {
		t.Fatalf("development install: exit %d, stderr %q", code, errOut)
	}
	if len(r.host.ran) != 0 || len(r.host.opened) != 0 || r.host.asked != 0 {
		t.Fatal("dry run changed the host or asked a question")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "whr", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote configuration: %v", err)
	}
}

func TestDevSetupUsesExplicitPrefixAndResolvedBinary(t *testing.T) {
	for _, kind := range []string{"custom", "symlink", "outside", "working-tree", "root"} {
		t.Run(kind, func(t *testing.T) {
			r, home := devSetupRig(t)
			args := []string{"setup", "--dev", "--user", "werner", "--only", "config-base"}
			want := ""
			switch kind {
			case "custom":
				custom := filepath.Join(home, "custom prefix")
				if err := os.MkdirAll(filepath.Join(custom, "bin"), 0o700); err != nil {
					t.Fatal(err)
				}
				r.exe = filepath.Join(custom, "bin", "whr")
				if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
				args = append(args, "--prefix", custom, "--dry-run")
			case "symlink":
				link := filepath.Join(home, "linked-whr")
				if err := os.Symlink(r.exe, link); err != nil {
					t.Fatal(err)
				}
				r.exe = link
				args = append(args, "--dry-run")
			case "outside":
				other := filepath.Join(home, "other-whr")
				if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
				r.exe = other
				want = "not an installed binary"
			case "working-tree":
				if err := os.Mkdir(filepath.Join(home, ".local", ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				want = "git working tree"
			case "root":
				r.env.User, r.env.UID = "root", 0
				want = "never runs as root"
			}
			code, _, errOut := runDevSetup(t, r, home, args...)
			if want != "" {
				if code != exitcode.Usage || !strings.Contains(errOut, want) {
					t.Fatalf("exit %d, stderr %q, want %q", code, errOut, want)
				}
			} else if code == exitcode.Usage || strings.Contains(errOut, "not an installed binary") {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
		})
	}
}

func TestDevSetupPassesTheSelectedBinaryToServiceInstall(t *testing.T) {
	r, home := devSetupRig(t)
	code, _, errOut := runDevSetup(t, r, home, "setup", "--dev", "--user", "werner", "--only", "service-install", "--dry-run")
	if code == exitcode.Usage {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	want := r.exe + " service install --config " + filepath.Join(home, ".config", "whr", "config.json") + " --whr " + r.exe
	if !strings.Contains(errOut, want) {
		t.Fatalf("service command must pin selected binary %q, stderr %q", want, errOut)
	}
}

func TestDoctorDevReportsTheDevelopmentPrefixAsAWarning(t *testing.T) {
	r, home := devSetupRig(t)
	_, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner")
	if !strings.Contains(out, "warn\tprefix\t") || !strings.Contains(out, filepath.Join(home, ".local")) {
		t.Fatalf("development prefix: stdout %q, stderr %q", out, errOut)
	}
	if !strings.Contains(errOut, "whr setup --dev --only config-base") {
		t.Fatalf("doctor repair omitted --dev: %q", errOut)
	}
	if len(r.host.ran) != 0 || r.host.asked != 0 {
		t.Fatal("doctor changed the host")
	}
}

func TestDoctorDevRejectsUnsafeInstallations(t *testing.T) {
	for _, kind := range []string{"outside", "working-tree", "writable", "root"} {
		t.Run(kind, func(t *testing.T) {
			r, home := devSetupRig(t)
			switch kind {
			case "outside":
				r.exe = filepath.Join(home, "other-whr")
				if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
			case "working-tree":
				if err := os.WriteFile(filepath.Join(home, ".local", ".git"), []byte("gitdir: elsewhere\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(filepath.Dir(r.exe), 0o777); err != nil { //nolint:gosec // deliberately unsafe permissions to test rejection
					t.Fatal(err)
				}
			case "root":
				r.env.User, r.env.UID = "root", 0
			}
			code, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", r.env.User)
			if code == 0 || !strings.Contains(out, "fail\tprefix\t") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

func TestDoctorDevKeepsCustomPrefixInRepairs(t *testing.T) {
	r, home := devSetupRig(t)
	prefix := filepath.Join(home, ".local")
	_, _, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner", "--prefix", prefix)
	if !strings.Contains(errOut, "whr setup --dev --only config-base --prefix '"+prefix+"'") {
		t.Fatal(errOut)
	}
}

type devConfigHost struct{ *setupHost }

func (h devConfigHost) Confirm(string) (bool, error) { h.asked++; return true, nil }
func (h devConfigHost) Line(prompt string) (string, error) {
	h.asked++
	if strings.HasPrefix(prompt, "Repository") {
		return "wstein/workharbor", nil
	}
	return "", nil
}

func TestDevSetupConfigBaseIsPrivateAndIdempotent(t *testing.T) {
	r, home := devSetupRig(t)
	r.env.Host = devConfigHost{r.host}
	args := []string{"setup", "--dev", "--user", "werner", "--only", "config-base"}
	code, _, errOut := runDevSetup(t, r, home, args...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	path := filepath.Join(home, ".config", "whr", "config.json")
	before, err := os.ReadFile(path) //nolint:gosec // path belongs to the isolated test home
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("configuration is not private: %v, %v", fi, err)
	}
	asked := r.host.asked
	code, _, errOut = runDevSetup(t, r, home, args...)
	if code != 0 {
		t.Fatalf("second setup: exit %d: %s", code, errOut)
	}
	after, err := os.ReadFile(path) //nolint:gosec // path belongs to the isolated test home
	if err != nil || !bytes.Equal(before, after) || r.host.asked != asked {
		t.Fatalf("second setup overwrote or prompted: %v", err)
	}
	if len(r.host.ran) != 0 || len(r.host.opened) != 0 {
		t.Fatal("config-base installed a service or ran commands")
	}
	if _, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents")); !os.IsNotExist(err) {
		t.Fatalf("config-base created a service: %v", err)
	}
}

func TestServiceInstallRetainsDevelopmentBinary(t *testing.T) {
	r := newServiceRig(t)
	r.whr = filepath.Join(r.home, ".local", "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(r.whr), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.whr, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
		t.Fatal(err)
	}
	code, _, errOut := r.run(t, "service", "install", "--whr", r.whr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	plist, err := os.ReadFile(filepath.Join(r.home, "Library", "LaunchAgents", launchd.Label+".plist"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(plist, []byte(r.whr)) {
		t.Fatalf("service did not retain the development binary: %s", plist)
	}
}

func TestDoctorDevRepairOfABrokenConfigKeepsDevAndPrefix(t *testing.T) {
	r, home := devSetupRig(t)
	if err := os.MkdirAll(filepath.Join(home, ".config", "whr"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "whr", "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(home, ".local")
	_, _, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner", "--prefix", prefix)
	want := "whr setup --dev --prefix '" + prefix + "'"
	if !strings.Contains(errOut, want) {
		t.Fatalf("a bare `whr setup` repair must keep --dev and the prefix, want %q in %q", want, errOut)
	}
	for _, l := range strings.Split(errOut, "\n") {
		if strings.Contains(l, "→ whr setup") && !strings.Contains(l, "whr setup --dev") && !strings.Contains(l, "whr setup host --dev") {
			t.Fatalf("repair without --dev: %q", l)
		}
	}
}

func TestDoctorDevRefusesBroadOrForeignPrefixes(t *testing.T) {
	for _, kind := range []string{"root", "home", "home-parent", "home-grandparent", "foreign-owner", "writable-above"} {
		t.Run(kind, func(t *testing.T) {
			r, home := devSetupRig(t)
			prefix := filepath.Join(home, ".local")
			switch kind {
			case "root":
				prefix = "/"
			case "home":
				prefix = home
			case "home-parent":
				prefix = filepath.Dir(home) // like `--prefix ~/..`
			case "home-grandparent":
				prefix = filepath.Dir(filepath.Dir(home)) // like `--prefix /Users` for /Users/<u>
			case "foreign-owner":
				r.env.UID++ // the running account is not the owner of the files
			case "writable-above":
				if err := os.Chmod(home, 0o777); err != nil { //nolint:gosec // deliberately unsafe permissions to test rejection
					t.Fatal(err)
				}
			}
			code, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner", "--prefix", prefix)
			if code == 0 || !strings.Contains(out, "fail\tprefix\t") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

func TestDoctorDevStickyDirectoryAbovePrefixIsAcceptedInsideIsRefused(t *testing.T) {
	for _, where := range []string{"above", "inside"} {
		t.Run(where, func(t *testing.T) {
			r, home := devSetupRig(t)
			prefix := filepath.Join(home, ".local")
			dir := home // a sticky directory above the prefix, like /tmp
			if where == "inside" {
				dir = filepath.Join(prefix, "bin") // between the prefix and the binary
			}
			if err := os.Chmod(dir, os.ModeSticky|0o777); err != nil { //nolint:gosec // deliberately unsafe permissions to test the sticky exception
				t.Fatal(err)
			}
			_, out, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "werner", "--prefix", prefix)
			failed := strings.Contains(out, "fail\tprefix\t")
			if failed != (where == "inside") {
				t.Fatalf("%s: prefix failed = %v, stdout %q, stderr %q", where, failed, out, errOut)
			}
		})
	}
}

// The summary's next command keeps the phase and --dev, --user and --prefix, and
// the suggested command is accepted when it is run again (#265).
func TestSetupSummaryNextCommandWorksForThePhaseAndFlagsOfTheRun(t *testing.T) {
	for _, c := range []struct {
		name string
		host bool
		args []string
		want string
	}{
		{"user phase", false, []string{"setup", "--dev", "--user", "werner"}, "next: whr setup --dev --user werner --from "},
		{"host phase", true, []string{"setup", "host", "--dev", "--user", "werner"}, "next: whr setup --only config-base --dev --user werner"}, // config-first is the admin's unreachable step: the remedy, not --from
		{"prefix with a space", false, []string{"setup", "--dev", "--user", "werner", "--prefix", "@HOME@/my prefix"}, "next: whr setup --dev --user werner --prefix '@HOME@/my prefix' --from "},
		{"only", false, []string{"setup", "--dev", "--user", "werner", "--only", "api-token", "--only", "config-dir"}, "next: whr setup --dev --user werner --only "},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, home := devSetupRig(t)
			if c.host {
				r.env.User = "admin" // the administrator runs the host phase, not the standard account
			}
			if strings.Contains(c.name, "prefix") {
				custom := filepath.Join(home, "my prefix")
				if err := os.MkdirAll(filepath.Join(custom, "bin"), 0o700); err != nil {
					t.Fatal(err)
				}
				r.exe = filepath.Join(custom, "bin", "whr")
				if err := os.WriteFile(r.exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // executable stand-in in an isolated test directory
					t.Fatal(err)
				}
			}
			c.args = append([]string(nil), c.args...)
			for i := range c.args {
				c.args[i] = strings.ReplaceAll(c.args[i], "@HOME@", home)
			}
			c.want = strings.ReplaceAll(c.want, "@HOME@", home)
			_, _, errOut := runDevSetup(t, r, home, append(c.args, "--dry-run")...)
			var next string
			for _, l := range strings.Split(errOut, "\n") {
				if strings.HasPrefix(strings.TrimSpace(l), "next: ") {
					next = strings.TrimSpace(l)
				}
			}
			if !strings.HasPrefix(next, c.want) || (c.name == "only" && strings.Contains(next, "--from")) {
				t.Fatalf("want %q, got %q in\n%s", c.want, next, errOut)
			}
			words := shellWords(strings.TrimPrefix(next, "next: whr "))
			if c.host { // the remedy is for the whr account, not the administrator
				r.env.User = "werner"
			}
			_, _, again := runDevSetup(t, r, home, append(words, "--dry-run")...)
			if strings.Contains(again, "no step") || strings.Contains(again, "this part runs as another user") || strings.Contains(again, "not an installed binary") {
				t.Fatalf("the suggested command %q is refused:\n%s", next, again)
			}
			if !strings.Contains(again, "Summary:") {
				t.Fatalf("the suggested command %q did not run:\n%s", next, again)
			}
		})
	}
}

// shellWords splits a printed command the way a shell does for the quoting
// QuoteArgv writes: single quotes, with a quote inside them closed, escaped
// and opened again.
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	in, started := false, false
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '\'':
			in, started = !in, true
		case ch == '\\' && !in && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			started = true
		case ch == ' ' && !in:
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(ch)
			started = true
		}
	}
	if started {
		words = append(words, cur.String())
	}
	return words
}

// A control character in --prefix or --user is refused up front, with no
// summary that would print it into a command a human may paste.
func TestSetupRefusesControlCharactersInPrefixAndUser(t *testing.T) {
	for _, c := range []struct {
		name string
		args func(home string) []string
	}{
		{"prefix newline", func(home string) []string { return []string{"--prefix", home + "/x\nreboot"} }},
		{"prefix escape", func(home string) []string { return []string{"--prefix", home + "/x\x1b[2J"} }},
		{"user newline", func(string) []string { return []string{"--user", "w\nreboot"} }},
		{"user escape", func(string) []string { return []string{"--user", "w\x1b[2J"} }},
		{"user bidi", func(string) []string { return []string{"--user", "w\u202ex"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, home := devSetupRig(t)
			args := append([]string{"setup", "--dev", "--dry-run"}, c.args(home)...)
			code, _, errOut := runDevSetup(t, r, home, args...)
			if code != exitcode.Usage || !strings.Contains(errOut, "must not contain a control") || strings.Contains(errOut, "next:") || strings.ContainsAny(errOut, "\x1b\u202e") {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
		})
	}
}

// Without an absolute HOME a development prefix cannot be judged against the
// home, so `HOME= whr setup --dev --prefix /Users` must not accept the home's parent.
func TestDoctorDevRefusesAPrefixWhenHomeIsEmptyOrRelative(t *testing.T) {
	for _, h := range []string{"", "relative/home"} {
		t.Run("HOME="+h, func(t *testing.T) {
			r, home := devSetupRig(t)
			code, out, errOut := runDevSetup(t, r, h, "doctor", "--dev", "--user", "werner", "--prefix", filepath.Dir(home))
			if code == 0 || !strings.Contains(out, "fail\tprefix\t") || !strings.Contains(out, "absolute HOME") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

// $HOME is the caller's word: the prefix is also compared with the account's
// home from the directory service, and a lookup that fails or gives no absolute
// path fails --dev closed (#278).
func TestDoctorDevComparesThePrefixWithTheDirectoryServiceHome(t *testing.T) {
	const lookup = "/usr/bin/dscl . -read /Users/werner NFSHomeDirectory"
	for _, c := range []struct {
		name    string
		env     string // HOME; "@HOME@" is the real home
		prefix  string // "@PARENT@", "@HOME@", or the default
		dscl    string // "" keeps the rig's answer, "-" removes it
		refused string
	}{
		{"HOME elsewhere, parent of the real home", "/nonexistent/home", "@PARENT@", "", "too broad"},
		{"HOME elsewhere, the real home", "/nonexistent/home", "@HOME@", "", "too broad"},
		{"HOME right, the real home", "@HOME@", "@HOME@", "", "too broad"},
		{"upper-cased parent of the real home", "/nonexistent/home", "@PARENT@", "", "too broad"},
		{"upper-cased real home", "/nonexistent/home", "@HOME@", "", "too broad"},
		{"upper-cased real home, HOME right", "@HOME@", "@HOME@", "", "too broad"},
		{"lookup fails", "@HOME@", "", "-", "directory service"},
		{"lookup empty", "@HOME@", "", "NFSHomeDirectory:\n", "no absolute NFSHomeDirectory"},
		{"lookup relative", "@HOME@", "", "NFSHomeDirectory: relative/home\n", "no absolute NFSHomeDirectory"},
		{"lookup other text", "@HOME@", "", "No such key\n", "no absolute NFSHomeDirectory"},
		{"normal prefix warns", "@HOME@", "", "", ""},
		{"HOME elsewhere, normal prefix warns", "/nonexistent/home", "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, home := devSetupRig(t)
			switch c.dscl {
			case "":
			case "-":
				delete(r.host.outputs, lookup)
			default:
				r.host.outputs[lookup] = c.dscl
			}
			env := strings.ReplaceAll(c.env, "@HOME@", home)
			prefix := filepath.Join(home, ".local")
			switch c.prefix {
			case "@PARENT@":
				prefix = filepath.Dir(home)
			case "@HOME@":
				prefix = home
			}
			if strings.HasPrefix(c.name, "upper-cased") {
				// The same directory under another spelling, on a case-insensitive
				// volume: the identity comparison must still refuse it.
				if _, err := os.Stat(strings.ToUpper(home)); err != nil {
					t.Skip("the volume is case-sensitive")
				}
				prefix = strings.ToUpper(prefix)
				r.exe = strings.ToUpper(r.exe)
			}
			code, out, errOut := runDevSetup(t, r, env, "doctor", "--dev", "--user", "werner", "--prefix", prefix)
			if c.refused == "" {
				if !strings.Contains(out, "warn\tprefix\t") {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
				}
				return
			}
			if code == 0 || !strings.Contains(out, "fail\tprefix\t") || !strings.Contains(out, c.refused) {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

// A control or bidi character in `whr doctor --user` is refused at the flag, as
// in `whr setup`, so no repair line carries it ("(run as <user>)"); a normal
// user prints unchanged, in text and in --json (#280).
func TestDoctorRefusesControlCharactersInUser(t *testing.T) {
	for _, c := range []struct{ name, user string }{
		{"newline", "w\nreboot"},
		{"escape", "w\x1b[2J"},
		{"bidi", "w\u202ex"},
		{"tab", "w\tx"},
	} {
		for _, json := range []bool{false, true} {
			t.Run(c.name, func(t *testing.T) {
				r, home := devSetupRig(t)
				args := []string{"doctor", "--dev", "--user", c.user}
				if json {
					args = append(args, "--json")
				}
				code, out, errOut := runDevSetup(t, r, home, args...)
				if code != exitcode.Usage || !strings.Contains(errOut, "must not contain a control") || out != "" || strings.ContainsAny(out+errOut, "\x1b\u202e") {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
				}
			})
		}
	}
	r, home := devSetupRig(t)
	_, _, errOut := runDevSetup(t, r, home, "doctor", "--dev", "--user", "whr")
	if !strings.Contains(errOut, "(run as whr)") {
		t.Fatalf("a normal user: stderr %q", errOut)
	}
}

// Ctrl-C during the --dev prefix check is an interrupt, not a usage error.
func TestDevSetupInterruptedInThePrefixCheckExitsWithTheInterruptCode(t *testing.T) {
	r, home := devSetupRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.env.Executable = func() (string, error) { cancel(); return r.exe, nil }
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Setup: r.env,
		Getenv: func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		},
	}
	// the prefix check fails (no home from the directory service) as Ctrl-C lands
	delete(r.host.outputs, "/usr/bin/dscl . -read /Users/werner NFSHomeDirectory")
	prefix := filepath.Join(home, ".local")
	code := Execute(ctx, env, []string{"setup", "--dev", "--user", "werner", "--only", "config-base", "--prefix", prefix})
	if code != exitcode.Interrupted {
		t.Errorf("exit %d, want %d\n%s", code, exitcode.Interrupted, errOut.String())
	}
}

// sigSeenHost is a host that has seen a command die of an interrupt while the
// context is still live: Ctrl-C reached the command before whr's own handler.
type sigSeenHost struct{ setup.Host }

func (sigSeenHost) Interrupted() bool { return true }
func (sigSeenHost) ResetInterrupts()  {}

func TestDevSetupPrefixCheckKilledByTheSignalWithALiveContextExitsWithTheInterruptCode(t *testing.T) {
	r, home := devSetupRig(t)
	r.env.Host = sigSeenHost{r.host}
	delete(r.host.outputs, "/usr/bin/dscl . -read /Users/werner NFSHomeDirectory")
	prefix := filepath.Join(home, ".local")
	code, _, errOut := runDevSetup(t, r, home, "setup", "--dev", "--user", "werner", "--only", "config-base", "--prefix", prefix)
	if code != exitcode.Interrupted {
		t.Errorf("exit %d, want %d\n%s", code, exitcode.Interrupted, errOut)
	}
}
