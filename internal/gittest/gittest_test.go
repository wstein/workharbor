package gittest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheEnvironmentIsMinimalAndNeverTheHumans(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "x")
	env := Env("/tmp/home-x", "A=1")
	joined := strings.Join(env, "\n")
	for _, want := range []string{"HOME=/tmp/home-x", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "SSH_AUTH_SOCK=\n", "A=1"} {
		if !strings.Contains(joined+"\n", want) {
			t.Errorf("the environment lacks %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"/tmp/agent.sock", "GIT_CONFIG_COUNT", "AWS_SECRET"} {
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
