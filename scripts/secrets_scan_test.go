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
if [ "$FAKE_SCAN_RC" = term ]; then
	printf '#!/bin/sh\nkill -TERM $PPID\nsleep 2\nexit 0\n' >"$GOBIN/gitleaks"
else
	printf '#!/bin/sh\necho "$@" >&2\nexit %s\n' "$FAKE_SCAN_RC" >"$GOBIN/gitleaks"
fi
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
	// a mktemp that makes its directory under $FAKE_TMP when set, because the
	// system one may ignore TMPDIR
	mk := "#!/bin/sh\n[ -n \"$FAKE_TMP\" ] && exec /usr/bin/mktemp -d \"$FAKE_TMP/d.XXXXXX\"\nexec /usr/bin/mktemp \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "mktemp"), []byte(mk), 0o700); err != nil { //nolint:gosec // a test stub
		t.Fatal(err)
	}
	env := append(gittest.Env(t.TempDir()), "PATH="+dir+":"+os.Getenv("PATH"), "FAKE_SCAN_RC="+rc)
	if buildFails {
		env = append(env, "FAKE_BUILD=fail")
	}
	return bash(t, env, "cd .. && "+script)
}

const (
	zero = "0000000000000000000000000000000000000000"
	// a sha no object store holds
	unknown   = "2222222222222222222222222222222222222222"
	preCommit = "make -s secrets-staged"
)

// pushOf is the pre-push hook fed one ref line: HEAD is the local sha (a real
// commit, as git passes), remote is the remote's sha, and env prefixes the hook.
func pushOf(env, remote string) string {
	return "echo \"refs/heads/x $(git rev-parse HEAD) refs/heads/x " + remote + "\" | " + env + " .githooks/pre-push"
}

var prePush = pushOf("", zero)

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

// TestPrePushRanges covers what the hook scans: a remote sha the local object
// store does not hold (a force push over unfetched commits) falls back to every
// commit no remote ref has, a known one scans remote..local, a new branch
// scans what no remote has, and a delete scans nothing.
func TestPrePushRanges(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, script, rc, want string
		ok                     bool
	}{
		{"unknown remote sha, clean", pushOf("", unknown), "0", "--not --remotes", true},
		{"unknown remote sha, finding", pushOf("", unknown), "42", "holds a secret", false},
		{"known remote sha", pushOf("", "$(git rev-parse HEAD~1)"), "0", "HEAD~1", true},
		{"new branch", pushOf("", zero), "0", "--not --remotes", true},
		{"delete", "echo \"(delete) " + zero + " refs/heads/x $(git rev-parse HEAD)\" | .githooks/pre-push", "42", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := scan(t, false, c.rc, c.script)
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok=%v\n%s", err, c.ok, out)
			}
			if c.want == "HEAD~1" { // the known sha is printed, not the name
				c.want = ".."
			}
			if !strings.Contains(out, c.want) && c.want != "" {
				t.Errorf("want %q:\n%s", c.want, out)
			}
		})
	}
}

// TestPrePushBlocksWhatItCannotScan: a ref to a blob or a tree holds no commit
// for gitleaks to read, an unknown sha is unreadable, and a malformed or blank
// line is not a ref at all. Each blocks, says so, and runs no scan (the stub
// would report clean).
func TestPrePushBlocksWhatItCannotScan(t *testing.T) {
	t.Parallel()
	line := func(l string) string { return "printf '%s' \"" + l + "\" | .githooks/pre-push" }
	for _, c := range []struct{ name, script string }{
		{"blob", line("refs/tags/b $(git rev-parse HEAD:go.mod) refs/tags/b " + zero + "\n")},
		{"tree", line("refs/tags/t $(git rev-parse HEAD^{tree}) refs/tags/t " + zero + "\n")},
		{"unknown local sha", line("refs/heads/x " + unknown + " refs/heads/x " + zero + "\n")},
		{"blank line", line("\n")},
		{"one field", line("refs/heads/x\n")},
		{"no remote sha", line("refs/heads/x $(git rev-parse HEAD)\n")},
		{"no final newline, one field", line("refs/heads/x")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := scan(t, false, "0", c.script)
			if err == nil || !strings.Contains(out, "could not run") {
				t.Fatalf("want a block saying could not run, err = %v:\n%s", err, out)
			}
			if strings.Contains(out, "--no-verify") {
				t.Errorf("must never suggest --no-verify:\n%s", out)
			}
		})
	}
	t.Run("no final newline, valid ref still scans", func(t *testing.T) {
		t.Parallel()
		out, err := scan(t, false, "42", line("refs/heads/x $(git rev-parse HEAD) refs/heads/x "+zero))
		if err == nil || !strings.Contains(out, "holds a secret") {
			t.Fatalf("a finding must block, err = %v:\n%s", err, out)
		}
	})
}

// TestHooksIgnoreAnExportedMakeFunction: bash imports BASH_FUNC_make%% even
// when it runs as sh, and the function would stand in for make. Both hooks call
// `command make`.
func TestHooksIgnoreAnExportedMakeFunction(t *testing.T) {
	t.Parallel()
	const fn = "'BASH_FUNC_make%%=() { exit 0; }'"
	for _, h := range []struct{ name, script, finding string }{
		{"pre-push", pushOf("env "+fn, zero), "holds a secret"},
		{"pre-commit", "env " + fn + " .githooks/pre-commit", "a secret is staged"},
	} {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			out, err := scan(t, false, "42", h.script)
			if err == nil || !strings.Contains(out, h.finding) {
				t.Fatalf("a finding must block, err = %v:\n%s", err, out)
			}
		})
	}
}

// TestSecretsRangeBlocksAnUnreadableRange: when git cannot resolve the range,
// gitleaks would report a clean scan, so the recipe blocks first.
func TestSecretsRangeBlocksAnUnreadableRange(t *testing.T) {
	t.Parallel()
	out, err := scan(t, false, "0", "make -s secrets-range RANGE="+unknown+"..HEAD")
	if err == nil || !strings.Contains(out, "could not run") {
		t.Fatalf("want a block saying could not run, err = %v:\n%s", err, out)
	}
}

// TestHooksIgnoreMakeFlagsInTheEnvironment: MAKEFLAGS and friends can turn a
// recipe off (GITLEAKS_FOUND=0, -n, -i), so both hooks unset them.
func TestHooksIgnoreMakeFlagsInTheEnvironment(t *testing.T) {
	t.Parallel()
	for _, flags := range []string{"MAKEFLAGS=GITLEAKS_FOUND=0", "MAKEFLAGS=n", "MAKEFLAGS=i", "GNUMAKEFLAGS=-n", "MFLAGS=-n"} {
		for _, h := range []struct{ name, script, finding string }{
			{"pre-push", pushOf("env "+flags, zero), "holds a secret"},
			{"pre-commit", "env " + flags + " .githooks/pre-commit", "a secret is staged"},
		} {
			t.Run(h.name+"/"+flags, func(t *testing.T) {
				t.Parallel()
				out, err := scan(t, false, "42", h.script)
				if err == nil || !strings.Contains(out, h.finding) {
					t.Fatalf("a finding must block, err = %v:\n%s", err, out)
				}
			})
		}
	}
}

// TestHooksIgnoreMakefilesInTheEnvironment: make reads every file MAKEFILES
// lists before the Makefile, so one can zero the finding or replace the shell
// and make the scan a silent no-op. Both hooks unset it.
func TestHooksIgnoreMakefilesInTheEnvironment(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"override":   "override GITLEAKS_FOUND := 0\n",
		"true shell": "SHELL := /usr/bin/true\n",
	} {
		for _, h := range []struct {
			name    string
			script  func(env string) string
			finding string
		}{
			{"pre-push", func(env string) string { return pushOf(env, zero) }, "holds a secret"},
			{"pre-commit", func(env string) string { return env + " .githooks/pre-commit" }, "a secret is staged"},
		} {
			t.Run(h.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				mk := filepath.Join(t.TempDir(), "extra.mk")
				if err := os.WriteFile(mk, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				out, err := scan(t, false, "42", h.script("env MAKEFILES="+mk))
				if err == nil || !strings.Contains(out, h.finding) {
					t.Fatalf("a finding must block, err = %v:\n%s", err, out)
				}
			})
		}
	}
}

// TestSecretScansRemoveTheirTempDirOnTerm: a TERM or HUP to the recipe still
// removes the directory that holds the scanner.
func TestSecretScansRemoveTheirTempDirOnTerm(t *testing.T) {
	t.Parallel()
	for name, script := range map[string]string{"secrets-range": "make -s secrets-range RANGE=HEAD~1..HEAD", "secrets-staged": preCommit} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			_, _ = scan(t, false, "term", "FAKE_TMP="+tmp+"; export FAKE_TMP; "+script)
			left, err := os.ReadDir(tmp)
			if err != nil || len(left) != 0 {
				t.Errorf("temp dir left behind: %v (%v)", left, err)
			}
		})
	}
}
