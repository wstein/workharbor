package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gittest.Git(context.Background(), t.TempDir(), dir, gittest.Identity, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// mainFixture makes a bare origin with one commit on main and a clone of it, and
// returns the clone, a second clone to push from and the first SHA.
func mainFixture(t *testing.T) (clone, pusher, first string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	pusher = filepath.Join(root, "pusher")
	clone = filepath.Join(root, "clone")
	gitRun(t, root, "init", "--bare", "--initial-branch=main", origin)
	gitRun(t, root, "clone", origin, pusher)
	gitRun(t, pusher, "checkout", "-B", "main")
	gitRun(t, pusher, "commit", "--allow-empty", "-m", "one")
	first = gitRun(t, pusher, "rev-parse", "HEAD")
	gitRun(t, pusher, "push", "origin", "main")
	gitRun(t, root, "clone", origin, clone)
	return clone, pusher, first
}

// waitMain runs wait-for-main.sh in dir with the isolated git environment and an
// optional directory of wrappers first on PATH.
func waitMain(t *testing.T, dir, sha string, env ...string) (string, error) {
	t.Helper()
	base := append(gittest.Env(t.TempDir()), "SHA="+sha, "WAIT_INTERVAL_SECONDS=1", "WAIT_TIMEOUT_SECONDS=5")
	script, err := filepath.Abs("wait-for-main.sh")
	if err != nil {
		t.Fatal(err)
	}
	return bash(t, append(base, env...), "cd '"+dir+"' && "+script)
}

func TestWaitForMainAlreadyThere(t *testing.T) {
	t.Parallel()
	clone, _, first := mainFixture(t)
	out, err := waitMain(t, clone, first)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "after 0s") {
		t.Errorf("should pass at once: %s", out)
	}
}

func TestWaitForMainArrivesLater(t *testing.T) {
	t.Parallel()
	clone, pusher, _ := mainFixture(t)
	gitRun(t, pusher, "commit", "--allow-empty", "-m", "two")
	second := gitRun(t, pusher, "rev-parse", "HEAD")
	// The clone's origin/main does not hold the commit; the wrapper makes the
	// third fetch the first one that can see it, as a push that lands mid-wait.
	bin := t.TempDir()
	count := filepath.Join(bin, "count")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := `#!/bin/sh
if [ "$1" = fetch ]; then
	echo x >> "` + count + `"
	if [ "$(wc -l < "` + count + `" | tr -d ' ')" -eq 3 ]; then
		"` + realGit + `" -C "` + pusher + `" push origin main >&2 || exit 1
	fi
fi
exec "` + realGit + `" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755); err != nil { //nolint:gosec // a fake executable
		t.Fatal(err)
	}
	out, err := waitMain(t, clone, second, "PATH="+bin+":"+os.Getenv("PATH"), "WAIT_TIMEOUT_SECONDS=10")
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if strings.Count(out, "waiting") != 2 || !strings.Contains(out, "after 2s") {
		t.Errorf("want two waits then a pass after 2s: %s", out)
	}
}

func TestWaitForMainNeverArrives(t *testing.T) {
	t.Parallel()
	clone, pusher, _ := mainFixture(t)
	gitRun(t, pusher, "commit", "--allow-empty", "-m", "unpushed")
	out, err := waitMain(t, clone, gitRun(t, pusher, "rev-parse", "HEAD"), "WAIT_TIMEOUT_SECONDS=2")
	if err == nil {
		t.Fatalf("should fail:\n%s", out)
	}
	if !strings.Contains(out, "is not on main after waiting 2s") {
		t.Errorf("message lacks the SHA or the waited time: %s", out)
	}
}

func TestWaitForMainInvalidInputMakesNoGitCall(t *testing.T) {
	t.Parallel()
	clone, _, first := mainFixture(t)
	for name, env := range map[string][]string{
		"short":    {"SHA=abc123"},
		"upper":    {"SHA=" + strings.ToUpper(first)},
		"option":   {"SHA=--upload-pack=x"},
		"interval": {"WAIT_INTERVAL_SECONDS=1;id"},
		"timeout":  {"WAIT_TIMEOUT_SECONDS=-1"},
		"zero":     {"WAIT_INTERVAL_SECONDS=0"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bin := t.TempDir()
			log := filepath.Join(bin, "git.log")
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho \"$*\" >> \""+log+"\"\n"), 0o755); err != nil { //nolint:gosec // a fake executable
				t.Fatal(err)
			}
			out, err := waitMain(t, clone, first, append([]string{"PATH=" + bin + ":" + os.Getenv("PATH")}, env...)...)
			if err == nil {
				t.Fatalf("should fail:\n%s", out)
			}
			if _, statErr := os.Stat(log); statErr == nil {
				t.Errorf("git was called")
			}
		})
	}
}
