package devcontainer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

// gitRepo is a real repository the tests commit to; its Run is what hostgit's
// Repo.Run does for the reader, without the hardening.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	g := &gitRepo{t: t, dir: t.TempDir()}
	g.git("init", "-q", "-b", "main")
	return g
}

func (g *gitRepo) git(args ...string) {
	g.t.Helper()
	cmd := gittest.Git(context.Background(), "", g.dir, nil, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (g *gitRepo) RunCapped(ctx context.Context, limit int64, args ...string) ([]byte, error) {
	cmd := gittest.Git(ctx, "", g.dir, nil, args...)
	out, err := cmd.Output()
	if int64(len(out)) > limit {
		return nil, errors.New("output over the cap")
	}
	return out, err
}

func (g *gitRepo) write(name, content string, mode os.FileMode) {
	g.t.Helper()
	p := filepath.Join(g.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil { //nolint:gosec // a test fixture that may be executable
		g.t.Fatal(err)
	}
}

func (g *gitRepo) commit(msg string) { g.git("add", "-A"); g.git("commit", "-q", "-m", msg) }

// The topic's devcontainer.json differs from the default branch's: only the
// default branch's is read, so an agent cannot configure its own environment
// (D38).
func TestResolveIgnoresATopicsDevcontainer(t *testing.T) {
	g := newGitRepo(t)
	g.write(".devcontainer/devcontainer.json", `{"image":"docker.io/library/golang:1.27.1"}`, 0o644)
	g.commit("default")
	g.git("switch", "-q", "-c", "topic")
	g.write(".devcontainer/devcontainer.json", `{"image":"docker.io/evil/image:1"}`, 0o644)
	g.commit("the agent's change")
	env, err := Resolve(context.Background(), g, "main", testOpts)
	if err != nil || env.Image != "docker.io/library/golang:1.27.1" {
		t.Fatalf("env = %+v err = %v", env, err)
	}
	// The same file with a working tree edit that was never committed.
	g.git("switch", "-q", "main")
	g.write(".devcontainer/devcontainer.json", `{"image":"docker.io/evil/uncommitted:1"}`, 0o644)
	env, err = Resolve(context.Background(), g, "main", testOpts)
	if err != nil || env.Image != "docker.io/library/golang:1.27.1" {
		t.Errorf("a working tree edit leaked into the environment: %+v %v", env, err)
	}
}

func TestExportWritesTheTreeNotTheWorkingTree(t *testing.T) {
	g := newGitRepo(t)
	g.write("app/Dockerfile", "FROM scratch\nCOPY run.sh /run.sh\n", 0o644)
	g.write("app/run.sh", "#!/bin/sh\n", 0o755)
	g.write("app/sub/data.txt", "committed", 0o644)
	g.write("other/secret.txt", "outside the context", 0o644)
	if err := os.Symlink("/etc/passwd", filepath.Join(g.dir, "app", "link")); err != nil {
		t.Fatal(err)
	}
	g.commit("files")
	g.write("app/sub/data.txt", "edited, not committed", 0o644)
	g.write("app/untracked.txt", "never committed", 0o644)

	dest := filepath.Join(t.TempDir(), "ctx")
	res, err := Export(context.Background(), g, "main", "app", dest, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 3 || len(res.Skipped) != 1 || !strings.HasPrefix(res.Skipped[0], "app/link") {
		t.Errorf("Exported = %+v", res)
	}
	got, _ := os.ReadFile(filepath.Join(dest, "sub", "data.txt")) //nolint:gosec // a path in the test's own temporary directory
	if string(got) != "committed" {
		t.Errorf("data.txt = %q: the context must come from the commit", got)
	}
	for _, gone := range []string{"untracked.txt", "link", "../other"} {
		if _, err := os.Lstat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s must not be in the context", gone)
		}
	}
	if fi, err := os.Stat(filepath.Join(dest, "run.sh")); err != nil || fi.Mode()&0o100 == 0 {
		t.Errorf("run.sh must stay executable: %v %v", fi, err)
	}
}

func TestExportWholeTreeAndLimits(t *testing.T) {
	g := newGitRepo(t)
	g.write("a.txt", "12345", 0o644)
	g.write("d/b.txt", "67890", 0o644)
	g.commit("files")
	res, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{})
	if err != nil || res.Files != 2 || res.Bytes != 10 {
		t.Errorf("whole tree: %+v %v", res, err)
	}
	if _, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{Files: 1, Bytes: 100}); !errors.Is(err, ErrRefused) {
		t.Errorf("too many files: err = %v", err)
	}
	if _, err := Export(context.Background(), g, "main", ".", t.TempDir(), Limits{Files: 10, Bytes: 7}); !errors.Is(err, ErrRefused) {
		t.Errorf("too many bytes: err = %v", err)
	}
	for _, dir := range []string{"..", "../x", "/etc"} {
		if _, err := Export(context.Background(), g, "main", dir, t.TempDir(), Limits{}); !errors.Is(err, ErrRefused) {
			t.Errorf("dir %q: err = %v, want ErrRefused", dir, err)
		}
	}
}
