package apple

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

const secret = "test-token-SECRET-VALUE" //nolint:gosec // a placeholder, not a credential

// A fake container CLI that records its arguments and the env file it was given.
const fakeContainer = `#!/bin/sh
out="$FAKE_OUT"
printf '%s\n' "$@" > "$out.args"
while [ $# -gt 0 ]; do
	if [ "$1" = --env-file ]; then
		cat "$2" > "$out.env"
		ls "$TMPDIR" > "$out.tmp"
	fi
	shift
done
`

func TestExecPassesTheEnvironmentThroughAPipeNeverArgvNeverAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "container"), []byte(fakeContainer), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out := filepath.Join(dir, "out")
	t.Setenv("FAKE_OUT", out)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	running := `[{"configuration":{"id":"whr-1","labels":{"workharbor.owner":"o1","workharbor.role":"environment"}},"status":{"state":"running"}}]`
	a := &Adapter{owner: "o1", run: func(_ context.Context, _ io.Reader, _ ...string) ([]byte, []byte, error) {
		return []byte(running), nil, nil
	}}
	st, err := a.Exec(context.Background(), "whr-1", runtime.ExecRequest{
		Cmd: []string{"true"}, Env: []string{"CLAUDE_CODE_OAUTH_TOKEN=" + secret, "B=x y=z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range st.Chunks() { //nolint:revive // drain
	}
	if code, err := st.Wait(); code != 0 || err != nil {
		t.Fatalf("wait = %d, %v", code, err)
	}
	args, _ := os.ReadFile(out + ".args") //nolint:gosec // a test file
	if strings.Contains(string(args), secret) || strings.Contains(string(args), "-e\n") {
		t.Errorf("a value reached argv:\n%s", args)
	}
	env, _ := os.ReadFile(out + ".env") //nolint:gosec // a test file
	if string(env) != "CLAUDE_CODE_OAUTH_TOKEN="+secret+"\nB=x y=z\n" {
		t.Errorf("env file = %q", env)
	}
	// The CLI was told to read the descriptor of the pipe, and no file held the
	// values at any time: the temp directory was empty while the CLI ran, and after.
	if !strings.Contains(string(args), "--env-file\n/dev/fd/3\n") {
		t.Errorf("the CLI was not told to read the pipe:\n%s", args)
	}
	if during, _ := os.ReadFile(out + ".tmp"); strings.TrimSpace(string(during)) != "" { //nolint:gosec // a test file
		t.Errorf("the temp directory held %q while the CLI ran", during)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("the temp directory is not empty after the exec: %v", left)
	}
}

func TestBadEnvironmentEntriesAreRefusedWithoutTheirValue(t *testing.T) {
	for name, entry := range map[string]string{
		"no equals":     secret,
		"inherit":       "HOME",
		"bad key":       "A B=" + secret,
		"comment key":   "#A=" + secret,
		"newline":       "A=" + secret + "\nB=1",
		"carriage ret.": "A=" + secret + "\r",
		"nul":           "A=" + secret + "\x00",
		"empty key":     "=" + secret,
	} {
		_, err := newEnvPipe([]string{entry})
		if !errors.Is(err, ErrBadEnv) {
			t.Errorf("%s: err = %v, want ErrBadEnv", name, err)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error shows the value: %v", name, err)
		}
	}
}

func TestSweepEnvFilesRemovesOnlyAFormerRunsPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	mk := func(name string, mode os.FileMode, mtime time.Time) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("KEY=value\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := mk("whr-env-111", 0o600, old)
	fresh := mk("whr-env-222", 0o600, time.Now().Add(time.Minute))             // newer than this process: not a former run's
	loose := mk("whr-env-333", 0o644, old)                                     // not private: not one of ours
	other := mk("whr-something-444", 0o600, old)                               // another name
	if err := os.Mkdir(filepath.Join(dir, "whr-env-555"), 0o700); err != nil { // a directory
		t.Fatal(err)
	}
	link := filepath.Join(dir, "whr-env-666")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	removed := SweepEnvFiles(dir, time.Now())
	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("removed %v, want only %s", removed, stale)
	}
	for _, kept := range []string{fresh, loose, other, filepath.Join(dir, "whr-env-555"), link} {
		if _, err := os.Lstat(kept); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
	if got := SweepEnvFiles(filepath.Join(dir, "missing"), time.Now()); len(got) != 0 {
		t.Errorf("a missing directory: %v", got)
	}
}
