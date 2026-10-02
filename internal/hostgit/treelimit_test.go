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
	r := bombRepo(t, g, 4) // 16 files of one byte in 30 trees: 46 entries
	ctx := context.Background()
	if err := r.CheckTree(ctx, "refs/heads/bomb"); err != nil {
		t.Errorf("a tree of 16 files: %v", err)
	}
	if err := r.checkTree(ctx, "refs/heads/bomb", 45, 1<<30); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("46 entries against a limit of 45: %v", err)
	}
	if err := r.checkTree(ctx, "refs/heads/bomb", 46, 1<<30); err != nil {
		t.Errorf("exactly 46 entries against a limit of 46: %v", err)
	}
	if err := r.checkTree(ctx, "refs/heads/bomb", 100, 15); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("16 bytes against a limit of 15: %v", err)
	}
	if err := r.CheckTree(ctx, "refs/heads/nope"); err == nil || errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("a missing branch: %v", err)
	}
}

// bombTree builds `depth` levels that list the level below twice, down to leaf (a
// tree id), and returns the top tree.
func bombTree(t *testing.T, env []string, path, leaf string, depth int) string {
	t.Helper()
	tree := leaf
	for range depth {
		tree = plumb(t, env, path, "040000 tree "+tree+"\ta\n040000 tree "+tree+"\tb\n", "mktree")
	}
	return tree
}

func plumbEnv(t *testing.T) []string {
	t.Helper()
	return append(plainEnv(filepath.Join(t.TempDir(), "home")), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@a", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@a")
}

// 41 objects, no file at all, 2^40 trees: `ls-tree -r` alone prints nothing while git
// walks them, so the trees themselves count and the refusal comes quickly.
func TestAFilelessNestedTreeBombIsRefusedQuickly(t *testing.T) {
	g := newGit(t, WithWorkspaceRoot(t.TempDir()))
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bomb.git")
	r, err := g.InitBare(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	env := plumbEnv(t)
	empty := plumb(t, env, path, "", "mktree")
	top := bombTree(t, env, path, empty, 40)
	commit := plumb(t, env, path, "", "commit-tree", top, "-m", "bomb")
	plumb(t, env, path, "", "update-ref", "refs/heads/bomb", commit)

	start := time.Now()
	if err := r.CheckTree(ctx, commit); !errors.Is(err, ErrTreeTooLarge) {
		t.Fatalf("CheckTree = %v, want ErrTreeTooLarge", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("refusing the fileless bomb took %s", took)
	}
	if err := r.CheckCommits(ctx, commit, ""); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("CheckCommits = %v, want ErrTreeTooLarge", err)
	}
}

// A deadline stops a check that has not finished.
func TestTheTreeCheckHonoursItsDeadline(t *testing.T) {
	g := newGit(t, WithWorkspaceRoot(t.TempDir()))
	r := bombRepo(t, g, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.checkTree(ctx, "refs/heads/bomb", MaxTreeEntries, MaxTreeBytes); err == nil {
		t.Error("a cancelled check passed")
	}
}

// The check is by commit ID and covers the commits in a range, each tree once.
func TestCheckCommitsCoversEveryCommitOfARange(t *testing.T) {
	g := newGit(t, WithWorkspaceRoot(t.TempDir()))
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "r.git")
	r, err := g.InitBare(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	env := plumbEnv(t)
	blob := plumb(t, env, path, "x", "hash-object", "-w", "--stdin")
	small := plumb(t, env, path, "100644 blob "+blob+"\tf\n", "mktree")
	bomb := bombTree(t, env, path, small, 30)
	base := plumb(t, env, path, "", "commit-tree", small, "-m", "base")
	a := plumb(t, env, path, "", "commit-tree", bomb, "-p", base, "-m", "bomb")
	b := plumb(t, env, path, "", "commit-tree", small, "-p", a, "-m", "deleted again")

	if err := r.CheckTree(ctx, b); err != nil {
		t.Errorf("the tip alone is small: %v", err)
	}
	if err := r.CheckCommits(ctx, b, base); !errors.Is(err, ErrTreeTooLarge) {
		t.Errorf("a bomb in an earlier commit: %v, want ErrTreeTooLarge", err)
	}
	if err := r.CheckCommits(ctx, base, ""); err != nil {
		t.Errorf("a small history: %v", err)
	}
	if err := r.CheckCommits(ctx, b, a); err != nil {
		t.Errorf("commits after the bomb are small: %v", err)
	}
}

// Prepare replays every commit, so a bomb that a later commit deletes is refused,
// and the branch stays as it was.
func TestPrepareRefusesABombDeletedByALaterCommit(t *testing.T) {
	p := newPrep(t)
	ctx := context.Background()
	path := p.repo.Path()
	env := plumbEnv(t)
	old := p.rev("refs/heads/" + p.topicBr)
	small := plumb(t, env, path, "", "rev-parse", old+"^{tree}")
	bomb := bombTree(t, env, path, small, 30)
	a := plumb(t, env, path, "", "commit-tree", bomb, "-p", old, "-m", "docs: add")
	b := plumb(t, env, path, "", "commit-tree", small, "-p", a, "-m", "docs: remove")
	plumb(t, env, path, "", "update-ref", "refs/heads/"+p.topicBr, b)

	start := time.Now()
	if _, err := p.repo.Prepare(ctx, p.spec()); !errors.Is(err, ErrTreeTooLarge) {
		t.Fatalf("Prepare = %v, want ErrTreeTooLarge", err)
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("refusing took %s", took)
	}
	if got := p.rev("refs/heads/" + p.topicBr); got != b {
		t.Errorf("the topic moved to %s", got)
	}
}
