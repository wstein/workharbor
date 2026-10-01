package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSecretsMayNotLieInsideAnyRoot(t *testing.T) {
	for root, want := range map[string]string{
		"store": "inside the tool store", "workspaces": "inside a workspace root",
	} {
		r := newRig(t)
		p := filepath.Join(r.dir, root, "key.pem")
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		r.cfg.GitHub.KeyFile = p
		if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), want) {
			t.Errorf("a secret in %s = %v", root, err)
		}
	}
}

func TestACaseVariantOfARootIsTheRoot(t *testing.T) {
	r := newRig(t)
	upper := filepath.Join(r.dir, "STORE")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("a case-sensitive disk: case variants are different directories")
	}
	p := filepath.Join(upper, "key.pem")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.cfg.GitHub.KeyFile = p
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "inside the tool store") {
		t.Errorf("a secret under a case variant of the tool store = %v", err)
	}
}

func TestASecretWithASecondHardLinkIsRefused(t *testing.T) {
	r := newRig(t)
	if err := os.Link(r.cfg.APITokenFile, filepath.Join(r.dir, "workspaces", "copy")); err != nil {
		t.Skip(err)
	}
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "hard links") {
		t.Errorf("a hard-linked secret = %v", err)
	}
	if _, err := ReadSecret(r.cfg.APITokenFile); err == nil {
		t.Error("ReadSecret read a hard-linked secret")
	}
}

func TestReadSecretChecksTheFileItOpened(t *testing.T) {
	r := newRig(t)
	got, err := ReadSecret(r.cfg.APITokenFile)
	if err != nil || string(got) != "value\n" {
		t.Fatalf("ReadSecret = %q, %v", got, err)
	}
	link := filepath.Join(r.dir, "secrets", "link")
	if err := os.Symlink(r.cfg.APITokenFile, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecret(link); err == nil {
		t.Error("a link was followed")
	}
	if err := os.Chmod(r.cfg.APITokenFile, 0o644); err != nil { //nolint:gosec // a test making a secret too open
		t.Fatal(err)
	}
	if _, err := ReadSecret(r.cfg.APITokenFile); err == nil || strings.Contains(err.Error(), "value") {
		t.Errorf("a world-readable secret = %v", err)
	}
	if _, err := ReadSecret("relative/token"); err == nil {
		t.Error("a relative path was read")
	}
}

func TestAgentAPIKeyIsKeyValueLines(t *testing.T) {
	r := newRig(t)
	write := func(s string) {
		if err := os.WriteFile(r.cfg.AgentAPIKeyEnvFile, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("# the API key\nANTHROPIC_API_KEY=test-key-x=y\r\n\nOTHER=1\n")
	env, err := r.cfg.AgentAPIKey()
	if err != nil || !slices.Equal(env, []string{"ANTHROPIC_API_KEY=test-key-x=y", "OTHER=1"}) {
		t.Fatalf("AgentAPIKey = %q, %v", env, err)
	}
	for _, bad := range []string{"test-token-SECRET\n", "export A=test-token-SECRET\n", "# only a comment\n"} {
		write(bad)
		if _, err := r.cfg.AgentAPIKey(); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("AgentAPIKey(%q) = %v", bad, err)
		}
	}
}

// D40: whr never handles a subscription credential, so one in the API-key
// file is refused, by name, without showing the value.
func TestASubscriptionTokenIsRefused(t *testing.T) {
	for _, line := range []string{"CLAUDE_CODE_OAUTH_TOKEN=test-token-SECRET", "SOME_SESSION_TOKEN=test-token-SECRET"} {
		r := newRig(t)
		if err := os.WriteFile(r.cfg.AgentAPIKeyEnvFile, []byte("ANTHROPIC_API_KEY=k\n"+line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := r.parse(t)
		if msg := problems(err); !strings.Contains(msg, "subscription credential") || !strings.Contains(msg, "line 2") || strings.Contains(msg, "SECRET") {
			t.Errorf("%s: problems = %q", line, msg)
		}
	}
}

func TestTheAPIKeyFileIsOptional(t *testing.T) {
	r := newRig(t)
	r.cfg.AgentAPIKeyEnvFile = ""
	c, err := r.parse(t)
	if err != nil {
		t.Fatalf("a configuration without an API key (subscription mode): %v", err)
	}
	if env, err := c.AgentAPIKey(); env != nil || err != nil {
		t.Errorf("AgentAPIKey without a file = %q, %v", env, err)
	}
}
