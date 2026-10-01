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
		"store": "inside the tool store", "cache": "inside the cache root", "workspaces": "inside the workspace root",
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

func TestAgentLoginIsKeyValueLines(t *testing.T) {
	r := newRig(t)
	write := func(s string) {
		if err := os.WriteFile(r.cfg.AgentLoginEnvFile, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("# the subscription login\nCLAUDE_CODE_OAUTH_TOKEN=test-token-x=y\r\n\nOTHER=1\n")
	env, err := r.cfg.AgentLogin()
	if err != nil || !slices.Equal(env, []string{"CLAUDE_CODE_OAUTH_TOKEN=test-token-x=y", "OTHER=1"}) {
		t.Fatalf("AgentLogin = %q, %v", env, err)
	}
	for _, bad := range []string{"test-token-SECRET\n", "export A=test-token-SECRET\n", "# only a comment\n"} {
		write(bad)
		if _, err := r.cfg.AgentLogin(); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("AgentLogin(%q) = %v", bad, err)
		}
	}
}
