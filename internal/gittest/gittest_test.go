package gittest

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTheEnvironmentIsMinimalAndNeverTheHumans(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("GIT_CONFIG_COUNT", "7")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "x")
	env := Env("/tmp/home-x", "A=1")
	joined := strings.Join(env, "\n")
	for _, want := range []string{"HOME=/tmp/home-x", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_KEY_0=user.useConfigOnly\nGIT_CONFIG_VALUE_0=true", "SSH_AUTH_SOCK=\n", "A=1"} {
		if !strings.Contains(joined+"\n", want) {
			t.Errorf("the environment lacks %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"/tmp/agent.sock", "GIT_CONFIG_COUNT=7", "AWS_SECRET"} {
		if strings.Contains(joined, bad) {
			t.Errorf("the human's environment leaked: %q", bad)
		}
	}
}

// git sees no system or global configuration and no credential helper.
func TestGitSeesNoCredentialHelper(t *testing.T) {
	out, err := Git(context.Background(), t.TempDir(), t.TempDir(), nil, "config", "--show-origin", "--get-all", "credential.helper").CombinedOutput()
	// an explicit --system read finds nothing either
	if out, err := Git(context.Background(), t.TempDir(), t.TempDir(), nil, "config", "--system", "--list").CombinedOutput(); err == nil && strings.Contains(string(out), "osxkeychain") {
		t.Errorf("git config --system reached the system file: %s", out)
	}
	// only the empty one from the command line, which resets any helper
	if err == nil && (strings.Contains(string(out), "osxkeychain") || strings.Contains(string(out), "file:")) {
		t.Errorf("git found a credential helper: %s", out)
	}
	if cmd := Git(context.Background(), "", t.TempDir(), nil, "status"); !filepath.IsAbs(strings.TrimPrefix(cmd.Env[1], "HOME=")) {
		t.Errorf("no private home: %v", cmd.Env)
	}
}

// A caller's own GIT_CONFIG_COUNT pairs must not replace the useConfigOnly guard.
func TestCallerConfigPairsKeepTheGuard(t *testing.T) {
	env := Env(t.TempDir(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=")
	for key, want := range map[string]string{"user.useConfigOnly": "true", "credential.helper": ""} {
		cmd := Git(context.Background(), t.TempDir(), t.TempDir(), nil, "config", "--get", key)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("git config --get %s = %q, %v; want %q", key, out, err, want)
		}
	}
}

// panics reports whether Env panics for these extra variables.
func panics(extra ...string) (p bool) {
	defer func() { p = recover() != nil }()
	Env("/tmp/home-x", extra...)
	return false
}

// A malformed count must never drop the guard: Env refuses it, where git would
// accept some of them ("" gives no entries, " 1" reads as 1).
func TestAMalformedConfigCountPanics(t *testing.T) {
	for _, count := range []string{"", " 1", "1 ", "x", "-1", "+1", "0x1"} {
		if !panics("GIT_CONFIG_COUNT=" + count) {
			t.Errorf("GIT_CONFIG_COUNT=%q did not panic", count)
		}
	}
	for _, count := range []string{"0", "1", "2"} {
		extra := []string{"GIT_CONFIG_COUNT=" + count, "GIT_CONFIG_KEY_0=a.b", "GIT_CONFIG_VALUE_0=1", "GIT_CONFIG_KEY_1=c.d", "GIT_CONFIG_VALUE_1=2"}
		if panics(extra...) {
			t.Errorf("GIT_CONFIG_COUNT=%s panicked", count)
			continue
		}
		joined := "\n" + strings.Join(Env("/tmp/h", extra...), "\n") + "\n"
		n, _ := strconv.Atoi(count)
		for _, want := range []string{"\nGIT_CONFIG_COUNT=" + strconv.Itoa(n+1) + "\n", "\nGIT_CONFIG_KEY_0=user.useConfigOnly\nGIT_CONFIG_VALUE_0=true\n"} {
			if !strings.Contains(joined, want) {
				t.Errorf("count %s: lacks %q in\n%s", count, want, joined)
			}
		}
	}
}

// A caller cannot weaken the isolation through extra: each of these panics.
func TestCallersCannotWeakenTheIsolation(t *testing.T) {
	for _, extra := range [][]string{
		{"HOME=/Users/someone"},
		{"XDG_CONFIG_HOME=/x"},
		{"GIT_CONFIG_SYSTEM=/etc/gitconfig"},
		{"GIT_CONFIG_NOSYSTEM=0"},
		{"GIT_CONFIG_GLOBAL=/Users/someone/.gitconfig"},
		{"GIT_CONFIG_PARAMETERS='user.useConfigOnly'='false'"},
		{"GIT_ASKPASS=/bin/ask"},
		{"SSH_AUTH_SOCK=/tmp/agent"},
		{"GIT_TERMINAL_PROMPT=1"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.useConfigOnly", "GIT_CONFIG_VALUE_0=false"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=osxkeychain"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_VALUE_0=x"}, // a count without its key
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=a.b"}, // a count without its value
		{"NOEQUALS"},
	} {
		if !panics(extra...) {
			t.Errorf("%v did not panic", extra)
		}
	}
	// the legitimate uses stay
	if panics("GIT_CONFIG_GLOBAL=/tmp/home-x/.gitconfig", "GIT_AUTHOR_NAME=t", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=") {
		t.Error("a global file inside home, an identity and an empty helper must be allowed")
	}
}
