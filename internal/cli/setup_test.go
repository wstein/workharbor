package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/launchd"
)

// setupHost is a Host that runs nothing: every command is recorded.
type setupHost struct {
	outputs map[string]string
	ran     []string
	opened  []string
	asked   int
}

func (h *setupHost) Output(_ context.Context, argv ...string) ([]byte, error) {
	if out, ok := h.outputs[strings.Join(argv, " ")]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("exit status 1")
}

func (h *setupHost) Run(_ context.Context, c doctor.Cmd) error {
	h.ran = append(h.ran, strings.Join(c.Full(), " "))
	return nil
}

func (h *setupHost) Open(_ context.Context, t string) error {
	h.opened = append(h.opened, t)
	return nil
}
func (h *setupHost) Line(string) (string, error)   { h.asked++; return "", nil }
func (h *setupHost) Secret(string) (string, error) { h.asked++; return "", nil }
func (h *setupHost) Confirm(string) (bool, error)  { h.asked++; return false, nil }
func (h *setupHost) Show(string)                   {}

type aquaOnly struct{ name string }

func (a aquaOnly) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if args[0] == "managername" {
		return []byte(a.name), nil
	}
	return nil, errors.New("exit status 113")
}

type setupRig struct {
	t    *testing.T
	host *setupHost
	env  SetupEnv
	exe  string
}

func newSetupRig(t *testing.T) *setupRig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "opt", "whr", "bin", "whr")
	if err := os.MkdirAll(filepath.Dir(exe), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
		t.Fatal(err)
	}
	r := &setupRig{t: t, host: &setupHost{outputs: map[string]string{}}, exe: exe}
	r.env = SetupEnv{
		Host: r.host, User: "werner", UID: 501, GOOS: "darwin", IsTerminal: func() bool { return true },
		Executable: func() (string, error) { return exe, nil },
		Manager:    &launchd.Manager{R: aquaOnly{"Aqua"}, UID: 501, GOOS: "darwin"},
	}
	return r
}

// runBare runs a command line exactly as given.
func (r *setupRig) runBare(args ...string) string {
	r.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return "" }, Setup: r.env}
	Execute(context.Background(), env, args)
	return out.String() + errOut.String()
}

func (r *setupRig) run(args ...string) (int, string, string) {
	r.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Getenv: func(k string) string {
		if k == "HOME" {
			return "/Users/whr"
		}
		return ""
	}, Setup: r.env}
	code := Execute(context.Background(), env, append(args, "--config", "/Users/whr/.config/whr/config.json", "--prefix", filepath.Dir(filepath.Dir(r.exe))))
	return code, out.String(), errOut.String()
}

func TestSetupNeedsATerminalUnlessItIsADryRun(t *testing.T) {
	r := newSetupRig(t)
	r.env.IsTerminal = func() bool { return false }
	code, _, errOut := r.run("setup", "host")
	if code != exitcode.Usage || !strings.Contains(errOut, "needs a terminal") || !strings.Contains(errOut, "--dry-run") {
		t.Errorf("no terminal: exit %d, stderr %q", code, errOut)
	}
	if len(r.host.ran) != 0 || r.host.asked != 0 {
		t.Error("something ran without a terminal")
	}
	if code, _, _ := r.run("setup", "host", "--dry-run"); code == exitcode.Usage {
		t.Errorf("a dry run without a terminal was refused")
	}
}

func TestTheHostPartRefusesRootAndTheWhrUser(t *testing.T) {
	r := newSetupRig(t)
	r.env.User, r.env.UID = "root", 0
	if code, _, errOut := r.run("setup", "host"); code != exitcode.Usage || !strings.Contains(errOut, "never runs as root") {
		t.Errorf("root: exit %d, stderr %q", code, errOut)
	}
	r.env.User, r.env.UID = "whr", 502
	if code, _, errOut := r.run("setup", "host"); code != exitcode.Usage || !strings.Contains(errOut, "workharbor's own account") {
		t.Errorf("the whr user: exit %d, stderr %q", code, errOut)
	}
	if len(r.host.ran) != 0 {
		t.Errorf("a refused run ran %v", r.host.ran)
	}
}

func TestTheUserPartNeedsWhrInItsDesktopSession(t *testing.T) {
	r := newSetupRig(t)
	if code, _, errOut := r.run("setup"); code != exitcode.Usage || !strings.Contains(errOut, "it is for whr, and this is werner") {
		t.Errorf("another user: exit %d, stderr %q", code, errOut)
	}
	r.env.User, r.env.UID = "whr", 502
	r.env.Manager = &launchd.Manager{R: aquaOnly{"Background"}, UID: 502, GOOS: "darwin"}
	if code, _, errOut := r.run("setup"); code != exitcode.Usage || !strings.Contains(errOut, "desktop session") {
		t.Errorf("over SSH: exit %d, stderr %q", code, errOut)
	}
	r.env.User, r.env.UID = "root", 0
	if code, _, errOut := r.run("setup"); code != exitcode.Usage || !strings.Contains(errOut, "never runs as root") {
		t.Errorf("root: exit %d, stderr %q", code, errOut)
	}
}

// A whr that is not the installed one is refused; a dry run says so and goes on.
func TestOnlyTheInstalledBinaryRunsTheWizard(t *testing.T) {
	r := newSetupRig(t)
	other := filepath.Join(t.TempDir(), "whr")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
		t.Fatal(err)
	}
	r.env.Executable = func() (string, error) { return other, nil }
	if code, _, errOut := r.run("setup", "host"); code != exitcode.Usage || !strings.Contains(errOut, "not an installed binary") {
		t.Errorf("a binary outside the prefix: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut := r.run("setup", "host", "--dry-run")
	if code == exitcode.Usage || !strings.Contains(errOut, "note (dry run)") {
		t.Errorf("a dry run: exit %d, stderr %q", code, errOut)
	}
}

func TestADryRunPrintsEveryFixAndChangesNothing(t *testing.T) {
	r := newSetupRig(t)
	r.host.outputs["pmset -g"] = " sleep 10\n autorestart 0\n"
	r.host.outputs["fdesetup status"] = "FileVault is Off."
	r.host.outputs["/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate"] = "Firewall is disabled. (State = 0)"
	code, out, errOut := r.run("setup", "host", "--dry-run")
	if len(r.host.ran) != 0 || len(r.host.opened) != 0 || r.host.asked != 0 {
		t.Fatalf("a dry run ran %v opened %v asked %d", r.host.ran, r.host.opened, r.host.asked)
	}
	for _, want := range []string{
		"$ sudo pmset -a sleep 0 disksleep 0 autorestart 1 womp 1",
		"$ sudo /usr/libexec/ApplicationFirewall/socketfilterfw --setglobalstate on",
		"$ sudo sysadminctl -addUser whr -fullName workharbor -password -",
		"$ sudo install -m 0644 -o root -g wheel",
		"sudo fdesetup enable",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the dry run does not show %q\n%s", want, errOut)
		}
	}
	for _, want := range []string{"fail\tpower\t", "fail\tfirewall\t", "fail\tfilevault\t"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if code != exitcode.Error {
		t.Errorf("exit %d, want 1 while steps are not done", code)
	}
	// optional steps (Screen Sharing, Tailscale) wait to be named
	if strings.Contains(out, "screen-sharing") || strings.Contains(out, "tailscale") {
		t.Error("optional steps ran unasked")
	}
	if code, out, _ := r.run("setup", "host", "--dry-run", "--only", "tailscale"); !strings.Contains(out, "not_verified\ttailscale") {
		t.Errorf("--only tailscale: exit %d, stdout %q", code, out)
	}
}

func TestAnUnknownStepIsAUsageErrorThatListsTheKnownOnes(t *testing.T) {
	r := newSetupRig(t)
	code, _, errOut := r.run("setup", "host", "--dry-run", "--only", "nope")
	if code != exitcode.Usage || !strings.Contains(errOut, `no step "nope"`) || !strings.Contains(errOut, "power") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := r.run("setup", "host", "--dry-run", "--from", "config-base"); code != exitcode.Usage {
		t.Errorf("a step of the other part: exit %d, stderr %q", code, errOut)
	}
}

func TestTheStepsCompleteInTheShell(t *testing.T) {
	r := newSetupRig(t)
	out := r.runBare("__complete", "setup", "host", "--only", "")
	for _, want := range []string{"power", "firewall", "filevault", "brew-packages"} {
		if !strings.Contains(out, want) {
			t.Errorf("--only does not complete %q:\n%s", want, out)
		}
	}
	out = r.runBare("__complete", "setup", "--from", "")
	if !strings.Contains(out, "config-base") || strings.Contains(out, "firewall") {
		t.Errorf("--from of the user part:\n%s", out)
	}
}
