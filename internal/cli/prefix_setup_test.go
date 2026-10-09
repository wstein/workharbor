package cli

import (
	"bytes"
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
)

func userPrefixRig(t *testing.T) (*setupRig, string) {
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

func runWithHome(t *testing.T, r *setupRig, home string, args ...string) (int, string, string) {
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

type configHost struct{ *setupHost }

func (h configHost) Confirm(string) (bool, error) { h.asked++; return true, nil }
func (h configHost) Line(prompt string) (string, error) {
	h.asked++
	if strings.HasPrefix(prompt, "Repository") {
		return "wstein/workharbor", nil
	}
	return "", nil
}

func TestSetupConfigBaseIsPrivateAndIdempotent(t *testing.T) {
	r, home := userPrefixRig(t)
	r.env.Host = configHost{r.host}
	args := []string{"setup", "--user", "werner", "--only", "config-base"}
	code, _, errOut := runWithHome(t, r, home, args...)
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
	code, _, errOut = runWithHome(t, r, home, args...)
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

// The summary's next command keeps the phase and --user and --prefix, and
// the suggested command is accepted when it is run again (#265).
func TestSetupSummaryNextCommandWorksForThePhaseAndFlagsOfTheRun(t *testing.T) {
	for _, c := range []struct {
		name string
		host bool
		args []string
		want string
	}{
		{"user phase", false, []string{"setup", "--user", "werner"}, "next: whr setup --user werner --from "},
		{"host phase", true, []string{"setup", "host", "--user", "werner"}, "next: whr setup --only config-base --user werner"}, // config-first is the admin's unreachable step: the remedy, not --from
		{"prefix with a space", false, []string{"setup", "--user", "werner", "--prefix", "@HOME@/my prefix"}, "next: whr setup --user werner --prefix '@HOME@/my prefix' --from "},
		{"only", false, []string{"setup", "--user", "werner", "--only", "api-token", "--only", "config-dir"}, "next: whr setup --user werner --only "},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, home := userPrefixRig(t)
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
			_, _, errOut := runWithHome(t, r, home, append(c.args, "--dry-run")...)
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
			_, _, again := runWithHome(t, r, home, append(words, "--dry-run")...)
			if strings.Contains(again, "no step") || strings.Contains(again, "this part runs as another user") {
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
			r, home := userPrefixRig(t)
			args := append([]string{"setup", "--dry-run"}, c.args(home)...)
			code, _, errOut := runWithHome(t, r, home, args...)
			if code != exitcode.Usage || !strings.Contains(errOut, "must not contain a control") || strings.Contains(errOut, "next:") || strings.ContainsAny(errOut, "\x1b\u202e") {
				t.Fatalf("exit %d, stderr %q", code, errOut)
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
				r, home := userPrefixRig(t)
				args := []string{"doctor", "--user", c.user}
				if json {
					args = append(args, "--json")
				}
				code, out, errOut := runWithHome(t, r, home, args...)
				if code != exitcode.Usage || !strings.Contains(errOut, "must not contain a control") || out != "" || strings.ContainsAny(out+errOut, "\x1b\u202e") {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
				}
			})
		}
	}
	r, home := userPrefixRig(t)
	_, _, errOut := runWithHome(t, r, home, "doctor", "--user", "whr")
	if !strings.Contains(errOut, "(run as whr)") {
		t.Fatalf("a normal user: stderr %q", errOut)
	}
}

func TestDevIsNoLongerAFlag(t *testing.T) {
	r, home := userPrefixRig(t)
	for _, args := range [][]string{{"setup", "--dev"}, {"setup", "--managed"}, {"doctor", "--dev"}, {"service", "install", "--dev"}} {
		if code, _, errOut := runWithHome(t, r, home, args...); code != exitcode.Usage || !strings.Contains(errOut, "unknown flag") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
	}
}

// A prefix the account owns just works: setup does not refuse it and the
// doctor warns (alpha policy, issue #493).
func TestAUserOwnedPrefixWorksWithAWarning(t *testing.T) {
	r, home := userPrefixRig(t)
	prefix := filepath.Join(home, ".local")
	code, _, errOut := runWithHome(t, r, home, "setup", "--user", "werner", "--only", "config-base", "--prefix", prefix, "--dry-run")
	if code == exitcode.Usage {
		t.Fatalf("setup refused a user-owned prefix: exit %d, stderr %q", code, errOut)
	}
	// the owner check looks the account up in the real user database, so the
	// test names the account that runs it
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	r.host.outputs["/usr/bin/dscl . -read /Users/"+me.Username+" NFSHomeDirectory"] = "NFSHomeDirectory: " + home + "\n"
	_, out, errOut := runWithHome(t, r, home, "doctor", "--user", me.Username, "--prefix", prefix)
	if !strings.Contains(out, "warn\tprefix\t") || strings.Contains(out, "fail\tprefix\t") && !strings.Contains(out, "does not exist") {
		t.Fatalf("stdout %q, stderr %q", out, errOut)
	}
}
