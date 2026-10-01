package apple

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/runtime"
)

const secret = "test-token-SECRET-VALUE" //nolint:gosec // a placeholder, not a credential

// A fake container CLI that records its arguments and the env file it was given.
const fakeContainer = `#!/bin/sh
out="$FAKE_OUT"
printf '%s\n' "$@" > "$out.args"
while [ $# -gt 0 ]; do
	if [ "$1" = --env-file ]; then
		cp "$2" "$out.env"
		ls -l "$2" | cut -c1-10 > "$out.mode"
	fi
	shift
done
`

func TestExecPassesTheEnvironmentThroughAFileNeverArgv(t *testing.T) {
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
	if mode, _ := os.ReadFile(out + ".mode"); strings.TrimSpace(string(mode)) != "-rw-------" { //nolint:gosec // a test file
		t.Errorf("env file mode = %q, want -rw-------", mode)
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "whr-env-*")); len(left) != 0 {
		t.Errorf("the env file was left behind: %v", left)
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
		_, _, err := envFile([]string{entry})
		if !errors.Is(err, ErrBadEnv) {
			t.Errorf("%s: err = %v, want ErrBadEnv", name, err)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error shows the value: %v", name, err)
		}
	}
}
