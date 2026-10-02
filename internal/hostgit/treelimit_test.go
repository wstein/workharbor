package hostgit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// plumb runs a git command with input in a repository, as a hostile guest's objects
// would be made.
func plumb(t *testing.T, env []string, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-c", "credential.helper="}, args...)...) //nolint:gosec // test helper; the arguments are built by the test
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bombRepo makes a bare repository with a branch whose tree is a nested-tree bomb:
// every level lists the level below it twice, so a handful of tiny objects name
// 2^depth files.
func bombRepo(t *testing.T, g *Git, depth int) *Repo {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bomb.git")
	r, err := g.InitBare(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	env := append(plainEnv(filepath.Join(t.TempDir(), "home")), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@a", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@a")
	blob := plumb(t, env, path, "x", "hash-object", "-w", "--stdin")
	tree := plumb(t, env, path, "100644 blob "+blob+"\tf\n", "mktree")
	for range depth {
		tree = plumb(t, env, path, "040000 tree "+tree+"\ta\n040000 tree "+tree+"\tb\n", "mktree")
	}
	commit := plumb(t, env, path, "", "commit-tree", tree, "-m", "bomb")
	plumb(t, env, path, "", "update-ref", "refs/heads/bomb", commit)
	return r
}

// A few kilobytes of objects that name a billion files are refused before anything is
// written, quickly, by the prepare and the editor copy alike.
func TestANestedTreeBombIsRefusedBeforeCheckout(t *testing.T) {
	g := newGit(t, WithWorkspaceRoot(t.TempDir()))
	r := bombRepo(t, g, 30) // 2^30 files from 31 tree objects
	start := time.Now()
	err := r.CheckTree(context.Background(), "refs/heads/bomb")
	if !errors.Is(err, ErrTreeTooLarge) {
		t.Fatalf("CheckTree = %v, want ErrTreeTooLarge", err)
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("refusing the bomb took %s: the listing was not stopped", took)
	}
	dest := filepath.Join(t.TempDir(), "copy")
	if _, err := r.EditorCopy(context.Background(), dest, "bomb"); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("EditorCopy = %v, want ErrTreeTooLarge", err)
	}
	if _, err := os.Lstat(dest); err == nil {
		t.Error("the editor copy was started for a bomb")
	}
}

// An ordinary tree passes, and each limit stops a tree that exceeds it.
func TestTheTreeLimitsApplyToEntriesAndBytes(t *testing.T) {
	g := newGit(t, WithWorkspaceRoot(t.TempDir()))
	r := bombRepo(t, g, 4) // 16 files of one byte
	ctx := context.Background()
	if err := r.CheckTree(ctx, "refs/heads/bomb"); err != nil {
		t.Errorf("a tree of 16 files: %v", err)
	}
	if err := r.checkTree(ctx, "refs/heads/bomb", 15, 1<<30); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("16 files against a limit of 15: %v", err)
	}
	if err := r.checkTree(ctx, "refs/heads/bomb", 16, 1<<30); err != nil {
		t.Errorf("exactly 16 files against a limit of 16: %v", err)
	}
	if err := r.checkTree(ctx, "refs/heads/bomb", 100, 15); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("16 bytes against a limit of 15: %v", err)
	}
	if err := r.CheckTree(ctx, "refs/heads/nope"); err == nil || errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("a missing branch: %v", err)
	}
}
