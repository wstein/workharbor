package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/launchd"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/runlog"
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
	for _, want := range []string{"make the directory", "$ echo 'has space' 'it'\\''s'", "$ sudo pmset -a sleep 0", "$ sudo -v"} {
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
	if err := GuardHost("root", 0, "whr", true); !errors.Is(err, ErrRoot) {
		t.Errorf("root = %v", err)
	}
	if err := GuardHost("whr", 502, "whr", false); !errors.Is(err, ErrWrongUser) {
		t.Errorf("a standard whr user on the host part = %v", err)
	}
	if err := GuardHost("whr", 502, "whr", true); err != nil {
		t.Errorf("an administrator whr user on the host part = %v", err)
	}
	if err := GuardHost("root", 0, "root", true); !errors.Is(err, ErrRoot) {
		t.Errorf("root as the whr user = %v", err)
	}
	if err := GuardHost("werner", 501, "whr", true); err != nil {
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

// A fix that needs a service runs only after a step brought it up, and the kernel
// step then runs against the started system (#265).
func TestAFixRunsOnlyAfterTheServiceItNeedsIsUp(t *testing.T) {
	var started, kernel bool
	start := step("container-start", doctor.PhaseUser, &started, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"start"}}}})
	start.Provides = "svc"
	k := step("container-kernel", doctor.PhaseUser, &kernel, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"kernel"}}}})
	k.Needs = "svc"

	// the start fix works: the kernel fix runs afterwards
	h := &fakeHost{answers: []string{"y", "y"}}
	startOK := start
	startOK.Fix = &doctor.Fix{Do: func(context.Context, doctor.Prompter) error { started = true; return nil }}
	kOK := k
	kOK.Fix = &doctor.Fix{Do: func(context.Context, doctor.Prompter) error { kernel = true; return nil }}
	outs, _, _ := run(t, h, []doctor.Check{startOK, kOK}, Options{Phase: doctor.PhaseUser})
	if !outs[0].Fixed || !outs[1].Fixed {
		t.Errorf("both should be fixed: %+v", outs)
	}

	// the start fix does not bring the service up: the kernel fix is not run
	started, kernel = false, false
	h = &fakeHost{answers: []string{"y", "y"}}
	outs, _, errOut := run(t, h, []doctor.Check{start, k}, Options{Phase: doctor.PhaseUser})
	for _, c := range h.ran {
		if c == "kernel" {
			t.Errorf("the kernel fix ran without the system: %v", h.ran)
		}
	}
	if !outs[1].Asked || !strings.Contains(errOut, "not run: it needs svc") {
		t.Errorf("outs %+v err %q", outs, errOut)
	}
}

func TestSummaryNamesWhatIsDoneWhatIsLeftAndTheNextCommand(t *testing.T) {
	var b bytes.Buffer
	Summary(&b, []Outcome{
		{Step: "config-dir", Status: doctor.OK},
		{Step: "container-start", Status: doctor.OK},
		{Step: "container-kernel", Status: doctor.Fail},
		{Step: "config-base", Status: doctor.NotVerified},
	}, Options{Verbose: true})
	got := b.String()
	for _, want := range []string{"Summary: 2 ok, 1 need action, 1 not verified\n", "done: config-dir, container-start", "left: container-kernel (fail), config-base (not_verified)", "next: whr setup --from container-kernel", "no Linux kernel"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	b.Reset()
	Summary(&b, []Outcome{{Step: "config-dir", Status: doctor.OK}}, Options{Verbose: true})
	if strings.Contains(b.String(), "next:") || !strings.Contains(b.String(), "left: none") {
		t.Errorf("a finished run: %s", b.String())
	}
}

// The next command keeps the phase and the flags of the run, so it works where
// the bare `whr setup --from <step>` is refused (#265).
// Without --verbose the summary is one count line and the next step: no step
// names with raw status tokens.
func TestSummaryHidesInternalTokensWithoutVerbose(t *testing.T) {
	var b bytes.Buffer
	Summary(&b, []Outcome{{Step: "config-base", Status: doctor.NotVerified}}, Options{})
	for _, bad := range []string{"not_verified", "left:", "done:"} {
		if strings.Contains(b.String(), bad) {
			t.Errorf("%q in\n%s", bad, b.String())
		}
	}
}

func TestSummaryNextCommandKeepsThePhaseAndFlags(t *testing.T) {
	left := []Outcome{
		{Step: "config-dir", Status: doctor.OK},
		{Step: "container-kernel", Status: doctor.Fail},
		{Step: "api-token", Status: doctor.Fail},
	}
	for _, c := range []struct {
		name string
		o    Options
		want string
	}{
		{"user phase", Options{}, "next: whr setup --from container-kernel\n"},
		{"host phase", Options{Resume: []string{"whr", "setup", "host"}}, "next: whr setup host --from container-kernel\n"},
		{
			"dev user",
			Options{Resume: []string{"whr", "setup", "--dev", "--user", "werner", "--prefix", "/Users/me/my prefix"}},
			"next: whr setup --dev --user werner --prefix '/Users/me/my prefix' --from container-kernel\n",
		},
		{"host dev", Options{Resume: []string{"whr", "setup", "host", "--dev", "--user", "werner"}}, "next: whr setup host --dev --user werner --from container-kernel\n"},
		// --from would also run steps nobody selected: name the steps left instead
		{
			"only",
			Options{Only: []string{"container-kernel", "api-token"}, Resume: []string{"whr", "setup", "--dev", "--user", "werner"}},
			"next: whr setup --dev --user werner --only container-kernel --only api-token\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			Summary(&b, left, c.o)
			if !strings.Contains(b.String(), c.want) {
				t.Errorf("want %q in\n%s", c.want, b.String())
			}
			if c.name == "only" && strings.Contains(b.String(), "--from") {
				t.Errorf("--only run suggests --from:\n%s", b.String())
			}
		})
	}
}

// providedElsewhere: `--only container-kernel` runs against a system that already
// runs and is refused against a stopped one, and a start step that is already
// done in this run counts as providing the service (#265).
func TestNeedsAreMetByAServiceThatRunsOrWasFoundRunning(t *testing.T) {
	mk := func(running bool) (start, kernel doctor.Check) {
		up, k := running, false
		start = step("container-start", doctor.PhaseUser, &up, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"start"}}}})
		start.Provides = "svc"
		kernel = step("container-kernel", doctor.PhaseUser, &k, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"kernel"}}}})
		kernel.Needs = "svc"
		return start, kernel
	}
	ranKernel := func(h *fakeHost) bool {
		for _, c := range h.ran {
			if c == "kernel" {
				return true
			}
		}
		return false
	}
	t.Run("only with the system running", func(t *testing.T) {
		start, kernel := mk(true)
		h := &fakeHost{answers: []string{"y"}}
		run(t, h, []doctor.Check{start, kernel}, Options{Phase: doctor.PhaseUser, Only: []string{"container-kernel"}})
		if !ranKernel(h) {
			t.Errorf("the kernel fix did not run against a running system: %v", h.ran)
		}
	})
	t.Run("only with the system stopped", func(t *testing.T) {
		start, kernel := mk(false)
		h := &fakeHost{answers: []string{"y"}}
		outs, _, errOut := run(t, h, []doctor.Check{start, kernel}, Options{Phase: doctor.PhaseUser, Only: []string{"container-kernel"}})
		if ranKernel(h) || !outs[0].Asked || !strings.Contains(errOut, "not run: it needs svc") {
			t.Errorf("the kernel fix ran against a stopped system: ran %v, outs %+v, err %q", h.ran, outs, errOut)
		}
	})
	t.Run("the start step is already ok in the same run", func(t *testing.T) {
		start, kernel := mk(true)
		h := &fakeHost{answers: []string{"y"}}
		outs, _, _ := run(t, h, []doctor.Check{start, kernel}, Options{Phase: doctor.PhaseUser})
		if !ranKernel(h) || len(outs) != 2 || outs[0].Status != doctor.OK {
			t.Errorf("a re-run with the service up must run the kernel fix: ran %v, outs %+v", h.ran, outs)
		}
	})
}

// A control, bidirectional or separator character never reaches the printed
// command raw: a newline would start a second command when the line is pasted.
func TestQuoteArgvEscapesWhatWouldBreakTheLine(t *testing.T) {
	for name, c := range map[string]struct{ arg, want string }{
		"newline": {"/tmp/x\nreboot", `'/tmp/x\nreboot'`},
		"return":  {"/tmp/x\rreboot", `'/tmp/x\rreboot'`},
		"escape":  {"w\x1b[2J", `'w\x1b[2J'`},
		"bidi":    {"a\u202eb", `'a\u202eb'`},
		"quote":   {"it's\n", `'it'\''s\n'`},
		"plain":   {"/opt/whr", `/opt/whr`},
		"space":   {"/tmp/x y", `'/tmp/x y'`},
	} {
		t.Run(name, func(t *testing.T) {
			got := QuoteArgv([]string{"whr", c.arg})
			if got != "whr "+c.want || strings.ContainsAny(got, "\n\r\x1b\u202e") {
				t.Fatalf("QuoteArgv = %q, want %q", got, "whr "+c.want)
			}
		})
	}
}

// Account lookup errors are fake: the wizard must neither offer nor execute
// creation while the legacy account's existence remains unknown.
type uncertainLegacyRunner struct{ failure string }

func (r uncertainLegacyRunner) Output(_ context.Context, argv ...string) ([]byte, error) {
	if strings.Join(argv, " ") == "dscl . -read /Users/workharbor UniqueID" {
		return nil, errors.New("exit status 56")
	}
	return nil, errors.New(r.failure)
}

func TestUncertainLegacyAccountNeverOffersOrRunsCreation(t *testing.T) {
	for _, failure := range []string{"exit status 1: Operation not permitted", "exit status 70: odd output"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", failure, dry), func(t *testing.T) {
				checks := doctor.Checks(doctor.Deps{GOOS: "darwin", Runner: uncertainLegacyRunner{failure}})
				h := &fakeHost{answers: []string{"y", "y"}}
				outs, out, errOut := run(t, h, checks, Options{Phase: doctor.PhaseHost, Only: []string{"workharbor-user"}, DryRun: dry})
				if len(outs) != 1 || outs[0].Status != doctor.NotVerified || outs[0].Fixed {
					t.Fatalf("outcomes: %+v", outs)
				}
				if len(h.ran) != 0 || strings.Contains(out+errOut+strings.Join(h.shown, "\n"), "addUser") {
					t.Fatalf("creation offered or executed: ran %v output %q %q shown %v", h.ran, out, errOut, h.shown)
				}
				if !strings.Contains(errOut, "Inspect the legacy account lookup failure") || !strings.Contains(errOut, "retry") {
					t.Errorf("missing recovery guidance: %q", errOut)
				}
			})
		}
	}
}

// A host step whose commands come from a builder gets its sudo -v too, before
// the first built command, and once for the whole run.
func TestSudoIsPrimedForCommandsABuilderReturns(t *testing.T) {
	var a, b bool
	build := func(arg string) *doctor.Fix {
		return &doctor.Fix{
			Cmds: []doctor.Cmd{{Argv: []string{"x", "<value>"}}},
			Build: func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) {
				return []doctor.Cmd{{Sudo: true, Argv: []string{"x", arg}}}, nil
			},
		}
	}
	h := &fakeHost{answers: []string{"y", "y"}}
	steps := []doctor.Check{step("one", doctor.PhaseHost, &a, build("v1")), step("two", doctor.PhaseHost, &b, build("v2"))}
	run(t, h, steps, Options{Phase: doctor.PhaseHost})
	if got, want := strings.Join(h.ran, "|"), "sudo -v|sudo x v1|sudo x v2"; got != want {
		t.Errorf("ran %q, want %q", got, want)
	}
}

// A providedElsewhere check that the context cut short is not an answer: the
// step is not recorded as not run, so a resume does not skip it (#362).
func TestACutShortNeedsCheckInterruptsInsteadOfSkipping(t *testing.T) {
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	var k bool
	start := step("container-start", doctor.PhaseUser, new(bool), nil)
	start.Provides = "svc"
	start.Run = func(context.Context) (doctor.Status, string) { cancel(); return doctor.Fail, "cut short" }
	kernel := step("container-kernel", doctor.PhaseUser, &k, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"kernel"}}}})
	kernel.Needs = "svc"
	var so, se bytes.Buffer
	_, err := Run(ctx, []doctor.Check{start, kernel}, &fakeHost{answers: []string{"y"}}, Options{Phase: doctor.PhaseUser, Only: []string{"container-kernel"}, Out: &so, Err: &se})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the interruption", err)
	}
	if strings.Contains(se.String(), "not run: it needs") {
		t.Errorf("recorded as not run: %q", se.String())
	}
}

func TestSummaryNamesTheLogPath(t *testing.T) {
	var b bytes.Buffer
	outs := []Outcome{{Step: "config-dir", Status: doctor.OK}}
	lp := filepath.Join(t.TempDir(), "run.log")
	lg, err := runlog.Open(lp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()
	Summary(&b, outs, Options{RunLog: lg})
	if !strings.Contains(b.String(), "log  "+lp) {
		t.Errorf("no log path in\n%s", b.String())
	}
	b.Reset()
	Summary(&b, outs, Options{})
	if strings.Contains(b.String(), "log  ") {
		t.Errorf("log line without a path:\n%s", b.String())
	}
}

func TestYesAnswersOnlyUndoableQuestions(t *testing.T) {
	var b bytes.Buffer
	h := &fakeHost{}
	r := &runner{h: h, o: Options{Yes: true}, ui: render.Writer{W: &b}}
	a, err := r.ask("Ready to run this?", render.DefaultYes)
	if err != nil || a != render.Yes || !strings.Contains(b.String(), "yes: Ready") {
		t.Errorf("undoable: %v %v %q", a, err, b.String())
	}
	if len(h.asked) != 0 {
		t.Errorf("asked despite --yes: %v", h.asked)
	}
	a, _ = r.ask("Delete it?", render.DefaultNo)
	if a == render.Yes || len(h.asked) != 1 {
		t.Errorf("irreversible must be asked: %v %v", a, h.asked)
	}
}
