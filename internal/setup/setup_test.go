package setup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/launchd"
)

var bg = context.Background()

// fakeHost records everything the wizard does. No test runs a real command.
type fakeHost struct {
	answers []string // Confirm: "y" or "n", in order; empty means n
	lines   []string
	secrets []string

	outputs map[string]string // joined argv -> output
	ran     []string
	opened  []string
	asked   []string
	shown   []string
}

func (f *fakeHost) Output(_ context.Context, argv ...string) ([]byte, error) {
	if out, ok := f.outputs[strings.Join(argv, " ")]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("exit status 1")
}

func (f *fakeHost) Run(_ context.Context, c doctor.Cmd) error {
	f.ran = append(f.ran, strings.Join(c.Full(), " "))
	return nil
}

func (f *fakeHost) Open(_ context.Context, target string) error {
	f.opened = append(f.opened, target)
	return nil
}

func (f *fakeHost) Line(q string) (string, error) {
	f.asked = append(f.asked, q)
	if len(f.lines) == 0 {
		return "", nil
	}
	l := f.lines[0]
	f.lines = f.lines[1:]
	return l, nil
}

func (f *fakeHost) Secret(q string) (string, error) {
	f.asked = append(f.asked, q)
	if len(f.secrets) == 0 {
		return "", errors.New("no secret")
	}
	s := f.secrets[0]
	f.secrets = f.secrets[1:]
	return s, nil
}

func (f *fakeHost) Confirm(q string) (bool, error) {
	f.asked = append(f.asked, q)
	if len(f.answers) == 0 {
		return false, nil
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a == "y", nil
}
func (f *fakeHost) Show(t string) { f.shown = append(f.shown, t) }

// step makes a step whose check passes once its fix has run.
func step(name string, phase doctor.Phase, fixed *bool, fix *doctor.Fix) doctor.Check {
	return doctor.Check{
		Name: name, Phase: phase, Title: name + " title", Fix: fix,
		Run: func(context.Context) (doctor.Status, string) {
			if *fixed {
				return doctor.OK, "now fine"
			}
			return doctor.Fail, "broken"
		},
	}
}

func run(t *testing.T, h *fakeHost, steps []doctor.Check, o Options) (outs []Outcome, out, errOut string) {
	t.Helper()
	var so, se bytes.Buffer
	o.Out, o.Err = &so, &se
	outs, err := Run(bg, steps, h, o)
	if err != nil {
		t.Fatal(err)
	}
	return outs, so.String(), se.String()
}

func TestAStepWhoseCheckPassesDoesNothing(t *testing.T) {
	h := &fakeHost{}
	ok := doctor.Check{
		Name: "power", Phase: doctor.PhaseHost, Run: func(context.Context) (doctor.Status, string) { return doctor.OK, "fine" },
		Fix: &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"pmset", "-a", "sleep", "0"}}}},
	}
	outs, out, _ := run(t, h, []doctor.Check{ok}, Options{Phase: doctor.PhaseHost})
	if len(h.ran) != 0 || len(h.asked) != 0 || len(h.opened) != 0 {
		t.Errorf("a passing step did something: ran %v asked %v", h.ran, h.asked)
	}
	if out != "ok\tpower\tfine\n" || len(outs) != 1 || outs[0].Fixed {
		t.Errorf("out %q outs %+v", out, outs)
	}
}

func TestAFixIsShownThenRunsAfterAYesAndTheCheckRunsAgain(t *testing.T) {
	var fixed bool
	did := []string{}
	fix := &doctor.Fix{
		Desc: "make the directory",
		Do:   func(context.Context, doctor.Prompter) error { did = append(did, "do"); return nil },
		Cmds: []doctor.Cmd{{Argv: []string{"container", "system", "start"}}, {Sudo: true, Argv: []string{"pmset", "-a", "sleep", "0"}}},
	}
	s := step("power", doctor.PhaseHost, &fixed, fix)
	s.Fix.Cmds[0].Argv = []string{"echo", "has space", "it's"}
	h := &fakeHost{answers: []string{"y"}}
	// the fix flips the fixed flag when its last command has run
	wrapped := *fix
	wrapped.Do = func(context.Context, doctor.Prompter) error { did = append(did, "do"); fixed = true; return nil }
	s.Fix = &wrapped
	outs, out, errOut := run(t, h, []doctor.Check{s}, Options{Phase: doctor.PhaseHost})
	// shown before it ran, as argument vectors
	for _, want := range []string{"does: make the directory", "$ echo 'has space' 'it'\\''s'", "$ sudo pmset -a sleep 0", "$ sudo -v"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the fix was not shown: lacks %q\n%s", want, errOut)
		}
	}
	if strings.Join(did, ",") != "do" {
		t.Errorf("the in-process step ran %v", did)
	}
	want := []string{"sudo -v", "echo has space it's", "sudo pmset -a sleep 0"}
	if strings.Join(h.ran, "|") != strings.Join(want, "|") {
		t.Errorf("ran %v, want %v: one sudo -v first, then the commands in order", h.ran, want)
	}
	if !outs[0].Fixed || !strings.Contains(out, "fail\tpower\tbroken") || !strings.Contains(out, "ok\tpower\tnow fine") {
		t.Errorf("outs %+v\n%s", outs, out)
	}
}

func TestADeclinedFixRunsNothing(t *testing.T) {
	var fixed bool
	s := step("power", doctor.PhaseHost, &fixed, &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"pmset"}}}})
	h := &fakeHost{answers: []string{"n"}}
	outs, _, _ := run(t, h, []doctor.Check{s}, Options{Phase: doctor.PhaseHost})
	if len(h.ran) != 0 || outs[0].Fixed || !outs[0].Asked {
		t.Errorf("ran %v, outs %+v", h.ran, outs)
	}
}

// sudo -v is asked once, only in the host phase, and with no keep-alive.
func TestSudoIsPrimedOnceInTheHostPhaseOnly(t *testing.T) {
	var a, b, c bool
	two := []doctor.Check{
		step("one", doctor.PhaseHost, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"x"}}}}),
		step("two", doctor.PhaseHost, &b, &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"y"}}}}),
	}
	h := &fakeHost{answers: []string{"y", "y"}}
	run(t, h, two, Options{Phase: doctor.PhaseHost})
	if n := strings.Count(strings.Join(h.ran, "\n"), "sudo -v"); n != 1 {
		t.Errorf("sudo -v ran %d times: %v", n, h.ran)
	}
	h2 := &fakeHost{answers: []string{"y"}}
	user := []doctor.Check{step("u", doctor.PhaseUser, &c, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"container", "list"}}}})}
	run(t, h2, user, Options{Phase: doctor.PhaseUser})
	if strings.Contains(strings.Join(h2.ran, "\n"), "sudo") {
		t.Errorf("the user phase used sudo: %v", h2.ran)
	}
}

// --dry-run runs the read-only checks for real and prints every fix without
// running, opening or asking anything.
func TestDryRunChecksForRealAndChangesNothing(t *testing.T) {
	checked := 0
	s := doctor.Check{
		Name: "power", Phase: doctor.PhaseHost, Title: "power",
		Run: func(context.Context) (doctor.Status, string) { checked++; return doctor.Fail, "off" },
		Fix: &doctor.Fix{
			Do:   func(context.Context, doctor.Prompter) error { t.Error("a dry run ran a fix"); return nil },
			Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"pmset", "-a", "sleep", "0"}}}, Open: "x-apple.systempreferences:x",
		},
	}
	h := &fakeHost{}
	_, out, errOut := run(t, h, []doctor.Check{s}, Options{Phase: doctor.PhaseHost, DryRun: true})
	if checked != 1 || len(h.ran) != 0 || len(h.opened) != 0 || len(h.asked) != 0 {
		t.Errorf("checked %d ran %v opened %v asked %v", checked, h.ran, h.opened, h.asked)
	}
	if !strings.Contains(errOut, "$ sudo pmset -a sleep 0") || !strings.Contains(errOut, "dry run") || !strings.Contains(out, "fail\tpower\toff") {
		t.Errorf("out %q err %q", out, errOut)
	}
}

func TestAGuidedStepOpensThePaneAndChecksAgain(t *testing.T) {
	var fixed bool
	s := step("screen-sharing", doctor.PhaseHost, &fixed, &doctor.Fix{Guide: "turn it on", Open: "x-apple.systempreferences:com.apple.Sharing-Settings.extension"})
	h := &fakeHost{answers: []string{"y", "y"}}
	fixed = false
	outs, _, errOut := run(t, h, []doctor.Check{s}, Options{Phase: doctor.PhaseHost})
	if len(h.opened) != 1 || !strings.HasPrefix(h.opened[0], "x-apple.systempreferences:") || len(h.ran) != 0 {
		t.Errorf("opened %v ran %v", h.opened, h.ran)
	}
	if !strings.Contains(errOut, "turn it on") || outs[0].Fixed {
		t.Errorf("%s / %+v", errOut, outs)
	}
}

func TestSelectingStepsByOnlyFromAndOptional(t *testing.T) {
	mk := func(name string, opt bool) doctor.Check {
		return doctor.Check{Name: name, Phase: doctor.PhaseUser, Optional: opt, Run: func(context.Context) (doctor.Status, string) { return doctor.OK, "" }}
	}
	steps := []doctor.Check{mk("a", false), mk("b", true), mk("c", false), {Name: "h", Phase: doctor.PhaseHost}}
	names := func(o Options) string {
		got, err := Select(steps, o)
		if err != nil {
			return err.Error()
		}
		var n []string
		for _, s := range got {
			n = append(n, s.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(Options{Phase: doctor.PhaseUser}); got != "a,c" {
		t.Errorf("default: %s (optional steps wait to be named)", got)
	}
	if got := names(Options{Phase: doctor.PhaseUser, Only: []string{"b"}}); got != "b" {
		t.Errorf("--only b: %s", got)
	}
	if got := names(Options{Phase: doctor.PhaseUser, From: "c"}); got != "c" {
		t.Errorf("--from c: %s", got)
	}
	if got := names(Options{Phase: doctor.PhaseUser, Only: []string{"h"}}); !strings.Contains(got, `no step "h" in this phase`) || !strings.Contains(got, "a, b, c") {
		t.Errorf("a step of the other phase: %s", got)
	}
	if got := names(Options{Phase: doctor.PhaseUser, From: "zzz"}); !strings.Contains(got, `no step "zzz"`) {
		t.Errorf("an unknown step: %s", got)
	}
}

func TestTheGuards(t *testing.T) {
	if err := GuardHost("root", 0, "whr"); !errors.Is(err, ErrRoot) {
		t.Errorf("root = %v", err)
	}
	if err := GuardHost("whr", 502, "whr"); !errors.Is(err, ErrWrongUser) {
		t.Errorf("the whr user on the host part = %v", err)
	}
	if err := GuardHost("werner", 501, "whr"); err != nil {
		t.Errorf("the administrator = %v", err)
	}

	aqua := &fakeLaunchctl{name: "Aqua"}
	m := launchd.Manager{R: aqua, UID: 502, GOOS: "darwin"}
	if err := GuardUser(bg, m, "whr", 502, "whr"); err != nil {
		t.Errorf("whr in Aqua = %v", err)
	}
	if err := GuardUser(bg, m, "werner", 501, "whr"); !errors.Is(err, ErrWrongUser) {
		t.Errorf("another user = %v", err)
	}
	if err := GuardUser(bg, launchd.Manager{R: aqua, UID: 0, GOOS: "darwin"}, "root", 0, "whr"); !errors.Is(err, ErrRoot) {
		t.Errorf("root = %v", err)
	}
	ssh := launchd.Manager{R: &fakeLaunchctl{name: "Background"}, UID: 502, GOOS: "darwin"}
	err := GuardUser(bg, ssh, "whr", 502, "whr")
	if !errors.Is(err, launchd.ErrNoGUILogIn) || !strings.Contains(err.Error(), "desktop session") {
		t.Errorf("over SSH = %v, want the desktop-session instruction", err)
	}
}

type fakeLaunchctl struct{ name string }

func (f *fakeLaunchctl) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if args[0] == "managername" {
		return []byte(f.name), nil
	}
	return nil, errors.New("unexpected")
}

func TestOnlyAnInstalledBinaryUnderAnAdminPrefixIsAccepted(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "opt", "whr")
	bin := filepath.Join(prefix, "bin", "whr")
	tree := filepath.Join(dir, "tree")
	built := filepath.Join(tree, "bin", "whr")
	for _, p := range []string{bin, built} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tree, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := CheckInstalled(bin, prefix); err != nil {
		t.Errorf("an installed binary = %v", err)
	}
	if err := CheckInstalled(built, prefix); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("a binary in a working tree = %v", err)
	}
	other := filepath.Join(dir, "elsewhere", "whr")
	if err := os.MkdirAll(filepath.Dir(other), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // an executable test file
		t.Fatal(err)
	}
	if err := CheckInstalled(other, prefix); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("a binary outside the prefix = %v", err)
	}
}
