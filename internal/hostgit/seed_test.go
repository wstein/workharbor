package hostgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testOrigin = "https://github.com/wstein/workharbor.git"

func TestSeedAgentCloneIsIndependentOfItsSource(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	f := newForge(t, "", 3)
	marker := filepath.Join(t.TempDir(), "pwned") // per test: nothing shared between runs
	// The source carries planted config: nothing of it may reach the clone.
	mustGit(t, f.env, f.dir, "config", "core.fsmonitor", "touch "+marker)
	if err := os.WriteFile(filepath.Join(f.dir, ".git", "hooks", "post-checkout"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil { //nolint:gosec // a planted hook in a test
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "repo")
	if err := g.SeedAgentClone(context.Background(), dest, f.dir, "main", testOrigin); err != nil {
		t.Fatal(err)
	}
	if n := f.count(dest, "HEAD"); n != 3 {
		t.Errorf("the clone has %d commits, want 3", n)
	}
	if got := strings.TrimSpace(mustGit(t, f.env, dest, "remote", "get-url", "origin")); got != testOrigin {
		t.Errorf("origin = %q: a host path must never be in the clone", got)
	}
	if got := strings.TrimSpace(mustGit(t, f.env, dest, "symbolic-ref", "--short", "HEAD")); got != "main" {
		t.Errorf("HEAD = %q", got)
	}
	for _, p := range []string{
		filepath.Join(dest, ".git", "objects", "info", "alternates"),
		filepath.Join(dest, ".git", "hooks", "post-checkout"),
	} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s exists in the clone", p)
		}
	}
	if cfg, _ := os.ReadFile(filepath.Join(dest, ".git", "config")); //nolint:gosec // inside the clone just made
	strings.Contains(string(cfg), "fsmonitor") || strings.Contains(string(cfg), f.dir) {
		t.Errorf("the source's config or path is in the clone's config:\n%s", cfg)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("something planted in the source ran during the clone")
	}
	// Hard links would tie the clone's objects to the source's.
	srcObj := mustGit(t, f.env, f.dir, "rev-parse", "HEAD:file.txt")
	st, err := os.Stat(filepath.Join(dest, ".git", "objects", srcObj[:2], srcObj[2:]))
	if err == nil && linkCount(st) > 1 {
		t.Error("the clone's objects are hard links to the source")
	}
}

func TestSeedAgentCloneRefusesBadInput(t *testing.T) {
	t.Parallel()
	g := newGit(t)
	f := newForge(t, "", 1)
	base := t.TempDir()
	tests := []struct {
		name                         string
		dest, source, branch, origin string
		want                         error
	}{
		{"ssh source", filepath.Join(base, "a"), "git@github.com:x/y.git", "main", testOrigin, ErrBadSource},
		{"flag as source", filepath.Join(base, "b"), "--upload-pack=x", "main", testOrigin, ErrBadSource},
		{"bad branch", filepath.Join(base, "c"), f.dir, "-x", testOrigin, ErrBadBranch},
		{"host path as origin", filepath.Join(base, "d"), f.dir, "main", f.dir, ErrBadSource},
		{"relative dest", "repo", f.dir, "main", testOrigin, ErrBadPath},
		{"unclean dest", base + "/x/../e", f.dir, "main", testOrigin, ErrBadPath},
		{"missing parent", filepath.Join(base, "nope", "f"), f.dir, "main", testOrigin, ErrBadPath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := g.SeedAgentClone(context.Background(), tc.dest, tc.source, tc.branch, tc.origin); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	existing := filepath.Join(base, "exists")
	if err := os.Mkdir(existing, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := g.SeedAgentClone(context.Background(), existing, f.dir, "main", testOrigin); !errors.Is(err, ErrBadPath) {
		t.Errorf("an existing destination: %v", err)
	}
	if err := g.SeedAgentClone(context.Background(), filepath.Join(base, "g"), f.dir, "no-such-branch", testOrigin); err == nil {
		t.Error("an unknown branch was accepted")
	}
	if _, err := os.Stat(filepath.Join(base, "g")); err == nil {
		t.Error("a failed clone left its destination behind")
	}
}
