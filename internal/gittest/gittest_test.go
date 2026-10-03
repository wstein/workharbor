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
		extra := []string{"GIT_CONFIG_COUNT=" + count, "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=CREDENTIAL.HELPER", "GIT_CONFIG_VALUE_1="}
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
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_VALUE_0=x"},               // a count without its key
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper"}, // a count without its value
		// any other key can load a file or run a program: only credential.helper="" passes
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=include.path", "GIT_CONFIG_VALUE_0=/tmp/planted"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=includeIf.gitdir:/.path", "GIT_CONFIG_VALUE_0=/tmp/planted"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=INCLUDE.PATH", "GIT_CONFIG_VALUE_0=/tmp/planted"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.sshCommand", "GIT_CONFIG_VALUE_0=x"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0=/x"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=alias.x", "GIT_CONFIG_VALUE_0=!sh"},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.https://h.helper", "GIT_CONFIG_VALUE_0="},
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

// A name that repeats would silently replace the earlier value in git.
func TestARepeatedConfigNamePanics(t *testing.T) {
	for _, extra := range [][]string{
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0="},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0="},
		{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=a", "GIT_CONFIG_VALUE_0=b"},
	} {
		if !panics(extra...) {
			t.Errorf("%v did not panic", extra)
		}
	}
}

// A HOME that is the human's, or contains it, would let a test touch their files.
func TestTheHumansHomeIsRefused(t *testing.T) {
	human, err := os.UserHomeDir()
	if err != nil || human == "" {
		t.Skip("no home directory")
	}
	homes := []string{human, filepath.Dir(human), "/"}
	// a case variant names the same directory on a case-insensitive filesystem
	if hi, err := os.Stat(human); err == nil {
		for _, v := range []string{strings.ToUpper(human), strings.ToLower(human)} {
			if vi, err := os.Stat(v); v != human && err == nil && os.SameFile(hi, vi) {
				homes = append(homes, v)
			}
		}
	}
	for _, home := range homes {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Env(%q) did not panic", home)
				}
			}()
			Env(home, "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"))
		}()
	}
	Env(t.TempDir()) // a temporary directory stays allowed
}

// A relative HOME is refused by the absolute-path rule itself: the call passes
// no extra variable, and the panic message must name the rule, so another
// panic cannot satisfy the row.
func TestARelativeHomeIsRefused(t *testing.T) {
	for _, home := range []string{"relative/home", ".", "../../../../.."} {
		func() {
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "is not an absolute path") {
					t.Errorf("Env(%q) panic = %q, want the absolute-path rule", home, msg)
				}
			}()
			Env(home)
		}()
	}
}

// The case-variant row only runs where the filesystem ignores case.
func TestACaseVariantOfTheHumansHomeIsRefused(t *testing.T) {
	human, err := os.UserHomeDir()
	if err != nil || human == "" {
		t.Skip("no home directory")
	}
	hi, err := os.Stat(human)
	if err != nil {
		t.Skip("home not readable")
	}
	v := strings.ToUpper(human)
	if vi, err := os.Stat(v); v == human || err != nil || !os.SameFile(hi, vi) {
		t.Skip("case-sensitive filesystem")
	}
	defer func() {
		if recover() == nil {
			t.Errorf("Env(%q) did not panic", v)
		}
	}()
	Env(v)
}
