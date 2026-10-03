package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

// fakeGo is a go that builds no scanner: `go install` writes a
// stub scanner that exits with $FAKE_SCAN_RC (or fails when $FAKE_BUILD=fail),
// and any other subcommand fails. No real gitleaks and no real secret is
// involved, and the environment cannot switch the scan off: the recipes only
// ever call `go install` and the binary it made.
const fakeGo = `#!/bin/sh
if [ "$1" != install ]; then echo "fake go: unexpected $*" >&2; exit 99; fi
[ "$FAKE_BUILD" = fail ] && { echo "fake go: install failed (offline)" >&2; exit 1; }
[ -d "$GOBIN" ] || exit 98
printf '#!/bin/sh\necho "$@" >&2\nexit %s\n' "$FAKE_SCAN_RC" >"$GOBIN/gitleaks"
chmod +x "$GOBIN/gitleaks"
`

// scan runs a shell command from the repository root with the fake go first
// on PATH.
func scan(t *testing.T, buildFails bool, rc, script string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGo), 0o700); err != nil { //nolint:gosec // a test stub
		t.Fatal(err)
	}
	env := append(gittest.Env(t.TempDir()), "PATH="+dir+":"+os.Getenv("PATH"), "FAKE_SCAN_RC="+rc)
	if buildFails {
		env = append(env, "FAKE_BUILD=fail")
	}
	return bash(t, env, "cd .. && "+script)
}

const (
	prePush   = "echo 'refs/heads/x 1111111111111111111111111111111111111111 refs/heads/x 0000000000000000000000000000000000000000' | .githooks/pre-push"
	preCommit = "make -s secrets-staged"
)

// TestSecretScansTellFindingFromScanThatCouldNotRun covers the pre-push
// (secrets-range) and pre-commit (secrets-staged) recipes: exit 42 is a
// finding, 0 is clean, and anything else, a failed build and gitleaks' own
// exit 1 on a fatal error included, is a scan that could not run. All but
// clean block.
func TestSecretScansTellFindingFromScanThatCouldNotRun(t *testing.T) {
	t.Parallel()
	for _, hook := range []struct{ name, script, finding string }{
		{"pre-push", prePush, "holds a secret"},
		{"pre-commit", preCommit, "a secret is staged"},
	} {
		for _, c := range []struct {
			name       string
			buildFails bool
			rc         string
			ok, found  bool
		}{
			{"clean", false, "0", true, false},
			{"finding", false, "42", false, true},
			{"gitleaks fatal error", false, "1", false, false},
			{"other exit", false, "3", false, false},
			{"killed", false, "137", false, false},
			{"install failed", true, "0", false, false},
		} {
			t.Run(hook.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				out, err := scan(t, c.buildFails, c.rc, hook.script)
				if (err == nil) != c.ok {
					t.Fatalf("err = %v, want ok=%v\n%s", err, c.ok, out)
				}
				if got := strings.Contains(out, hook.finding); got != c.found {
					t.Errorf("says finding = %v, want %v:\n%s", got, c.found, out)
				}
				if !c.ok && !c.found && !strings.Contains(out, "could not run") {
					t.Errorf("want \"could not run\":\n%s", out)
				}
				if strings.Contains(out, "--no-verify") {
					t.Errorf("must never suggest --no-verify:\n%s", out)
				}
			})
		}
	}
}
