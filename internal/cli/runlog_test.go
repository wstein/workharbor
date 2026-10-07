package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/setup"
)

// Issue #379: --log-file puts the run log where the person says, mode 0600, one
// step line per check, and the path is printed at the end.
func TestDoctorWritesTheRunLogToLogFile(t *testing.T) {
	r := newSetupRig(t)
	p := filepath.Join(t.TempDir(), "sub", "doctor.log")
	_, _, errOut := r.run("doctor", "--user", "operator", "--log-file", p)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("no log file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(p) //nolint:gosec // a test path
	if n := strings.Count(string(b), "step "); n < 5 || !strings.Contains(string(b), "step workharbor-user: unknown") {
		t.Errorf("want one line per check, got %d:\n%s", n, b)
	}
	if !strings.HasSuffix(strings.TrimSpace(errOut), p) {
		t.Errorf("the log path is not the last thing printed:\n%s", errOut)
	}
}

func TestSetupDryRunLogFileAndPath(t *testing.T) {
	r := newSetupRig(t)
	p := filepath.Join(t.TempDir(), "setup.log")
	_, _, errOut := r.run("setup", "--dry-run", "--log-file", p)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("no log file: %v", err)
	}
	if !strings.Contains(errOut, "log  "+p) {
		t.Errorf("path not printed:\n%s", errOut)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; last != "log  "+p {
		t.Errorf("summary does not end with the log path: %q", last)
	}
}

func TestLogFileThatCannotBeOpenedIsAUsageError(t *testing.T) {
	r := newSetupRig(t)
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	code, _, errOut := r.run("doctor", "--user", "operator", "--log-file", filepath.Join(blocker, "x.log"))
	if code != 2 || !strings.Contains(errOut, "cannot open the run log") {
		t.Errorf("code %d: %s", code, errOut)
	}
}

func TestNoDefaultRunLogWithoutAnAbsoluteHome(t *testing.T) {
	r := newSetupRig(t)
	r.env.NoRunLog = false
	wd := t.TempDir()
	t.Chdir(wd)
	r.runBare("doctor", "--user", "operator") // HOME is empty here
	if _, err := os.Stat(filepath.Join(wd, ".local")); err == nil {
		t.Error("a log was written under the current directory")
	}
}

func TestNoDefaultRunLogAsRoot(t *testing.T) {
	r := newSetupRig(t)
	r.env.NoRunLog = false
	r.env.UID = 0
	_, _, errOut := r.run("doctor", "--user", "operator")
	if !strings.Contains(errOut, "no run log as root") || strings.Contains(errOut, "log  /") {
		t.Errorf("want one note and no log:\n%s", errOut)
	}
}

func TestLogFileHelpHasAPlaceholder(t *testing.T) {
	r := newSetupRig(t)
	for _, args := range [][]string{{"doctor", "--help"}, {"setup", "--help"}, {"offboard", "host", "--help"}} {
		out := r.runBare(args...)
		if !strings.Contains(out, "--log-file path") {
			t.Errorf("%v: no --log-file placeholder:\n%s", args, out)
		}
	}
}

// The production wiring: the real Terminal host gets the log, so the commands it
// runs are logged (the rig's fake host cannot show that).
func TestStartRunLogGivesTheLogToTheTerminalHost(t *testing.T) {
	var errBuf strings.Builder
	st := &state{env: &Env{Stderr: &errBuf, Getenv: func(string) string { return "" }}}
	env := SetupEnv{Host: setup.Terminal{Err: &errBuf}}
	p := filepath.Join(t.TempDir(), "t.log")
	lg, err := st.startRunLog(&env, "setup", p, false)
	if err != nil {
		t.Fatal(err)
	}
	term, ok := env.Host.(setup.Terminal)
	if !ok || term.Log != lg {
		t.Fatalf("host %#v: the Terminal has no log", env.Host)
	}
	if _, err := term.Output(context.Background(), "/bin/sh", "-c", "echo hello-log; exit 3"); err == nil {
		t.Fatal("want exit 3")
	}
	_ = lg.Close()
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "exit 3") || !strings.Contains(string(b), "hello-log") { //nolint:gosec // a test path
		t.Errorf("command not logged:\n%s", b)
	}
}

func TestSetupStepLinesReachTheLogFile(t *testing.T) {
	r := newSetupRig(t)
	p := filepath.Join(t.TempDir(), "s.log")
	r.run("setup", "--dry-run", "--log-file", p)
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "step ") { //nolint:gosec // a test path
		t.Errorf("no step lines (Options.RunLog not wired):\n%s", b)
	}
}
