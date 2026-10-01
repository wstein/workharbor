package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeveralWorkspaceRootsAreChecked(t *testing.T) {
	r := newRig(t)
	ssd := filepath.Join(r.dir, "ssd")
	if err := os.MkdirAll(ssd, 0o750); err != nil {
		t.Fatal(err)
	}
	r.cfg.Roots.Workspaces = append(r.cfg.Roots.Workspaces, ssd)
	if _, err := r.parse(t); err != nil {
		t.Fatalf("two separate roots: %v", err)
	}
	// A secret in the second root is as bad as one in the first.
	p := filepath.Join(ssd, "key.pem")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.cfg.GitHub.KeyFile = p
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "inside a workspace root") {
		t.Errorf("a secret in the second root = %v", err)
	}
	// Roots may not nest.
	r = newRig(t)
	nested := filepath.Join(r.dir, "workspaces", "inner")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	r.cfg.Roots.Workspaces = append(r.cfg.Roots.Workspaces, nested)
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "overlap") {
		t.Errorf("nested roots = %v", err)
	}
	// At least one root.
	r = newRig(t)
	r.cfg.Roots.Workspaces = nil
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "roots.workspaces: at least one") {
		t.Errorf("no roots = %v", err)
	}
}

func TestARootInsideAGitRepositoryIsRefused(t *testing.T) {
	r := newRig(t)
	if err := os.Mkdir(filepath.Join(r.dir, ".git"), 0o750); err != nil { // the rig's directory is a human's repository
		t.Fatal(err)
	}
	_, err := r.parse(t)
	if err == nil || !strings.Contains(problems(err), "inside the git repository") {
		t.Errorf("a root in a repository = %v", err)
	}
}

func TestCheckWorkspacePath(t *testing.T) {
	r := newRig(t)
	cfg, err := r.parse(t)
	if err != nil {
		t.Fatal(err)
	}
	root := cfg.Roots.Workspaces[0]
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := mk("docs")
	if got, err := cfg.CheckWorkspacePath(good); err != nil || got != good {
		t.Errorf("a folder below the root = %q, %v", got, err)
	}

	notEmpty := mk("busy")
	if err := os.WriteFile(filepath.Join(notEmpty, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(r.dir, "store") // a real directory, not below a workspace root
	repo := mk("human")
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	inRepo := filepath.Join(repo, "ws")
	if err := os.Mkdir(inRepo, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(r.dir, "secrets", "to-store")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct{ name, path, want string }{
		{"the root itself", root, "workspace root itself"},
		{"not below a root", outside, "not below a workspace root"},
		{"a link out of the root", link, "not below a workspace root"},
		{"not empty", notEmpty, "not empty"},
		{"inside a human's repository", inRepo, "inside the git repository"},
		{"relative", "docs", "absolute"},
		{"unclean", root + "/docs/../docs", "clean"},
		{"missing", filepath.Join(root, "nope"), "does not exist"},
		{"a file", file, "not a directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cfg.CheckWorkspacePath(tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

func TestEnvironmentAndStateDirAreChecked(t *testing.T) {
	r := newRig(t)
	r.cfg.Environment = Environment{Image: "fedora", EgressAllow: []string{"api.anthropic.com", "proxy.golang.org"}, CPUs: 4}
	if _, err := r.parse(t); err != nil {
		t.Fatalf("a valid environment: %v", err)
	}
	for _, bad := range []string{"1.2.3.4", "*.example.com", "example.com:443", "a b.com", "localhost", "-x.com", "x..com", "x.com/path", ""} {
		r.cfg.Environment.EgressAllow = []string{bad}
		if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "environment.egress_allow") {
			t.Errorf("egress host %q = %v", bad, err)
		}
	}
	r = newRig(t)
	r.cfg.Environment = Environment{CPUs: -1}
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "must not be negative") {
		t.Errorf("negative cpus = %v", err)
	}
	// The database may not live where an agent writes.
	r = newRig(t)
	r.cfg.StateDir = filepath.Join(r.dir, "workspaces")
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "state_dir") {
		t.Errorf("state_dir in a workspace root = %v", err)
	}
	r = newRig(t)
	state := filepath.Join(r.dir, "state")
	if err := os.Mkdir(state, 0o750); err != nil {
		t.Fatal(err)
	}
	r.cfg.StateDir = state
	if _, err := r.parse(t); err != nil {
		t.Errorf("a separate state_dir: %v", err)
	}
	if got := (Environment{}).Resolved(); got.Image != "" || got.Base != DefaultBase || got.CPUs != 2 || got.MemoryMB != 4096 || got.EgressAllow[0] != "api.anthropic.com" {
		t.Errorf("defaults = %+v", got)
	}
}

func TestTheGitHubAPIURLIsHTTPSOrLoopback(t *testing.T) {
	for url, ok := range map[string]bool{
		"": true, "https://api.github.com": true, "https://ghe.example.com/api/v3": true, "http://127.0.0.1:8081": true, "http://[::1]:9": true,
		"http://api.github.com": false, "http://localhost:8081": false, "ftp://x": false, "https://u:p@x.com": false, "https://x.com?a=b": false, "not a url": false,
	} {
		r := newRig(t)
		r.cfg.GitHub.APIURL = url
		_, err := r.parse(t)
		if (err == nil) != ok {
			t.Errorf("api_url %q: err = %v, want ok=%v", url, err, ok)
		}
	}
}
