package setup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// askHost is a Host that also answers the [Y/n/q] prompts, and records the
// default each one offered.
type askHost struct {
	fakeHost
	answers2 []render.Answer
	defaults []render.Default
	qs       []string
}

func (a *askHost) Ask(q string, d render.Default) (render.Answer, error) {
	a.qs, a.defaults = append(a.qs, q), append(a.defaults, d)
	if len(a.answers2) == 0 {
		return render.No, nil
	}
	r := a.answers2[0]
	a.answers2 = a.answers2[1:]
	return r, nil
}

func fixStep(name string, st doctor.Status, detail string, fix *doctor.Fix) doctor.Check {
	return doctor.Check{
		Name: name, Phase: doctor.PhaseHost, Title: "title of " + name, Fix: fix,
		Run: func(context.Context) (doctor.Status, string) { return st, detail },
	}
}

func goldenFile(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) //nolint:gosec // a golden file of this package
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden file; got:\n%s", name, got)
	}
}

func goldenSteps() []doctor.Check {
	create := &doctor.Fix{
		Cmds:  []doctor.Cmd{{Sudo: true, Argv: []string{"sysadminctl", "-addUser", "workharbor", "-fullName", "WorkHarbor", "-password", "-"}}},
		Guide: "Then log in as workharbor and run `whr setup`.",
	}
	guided := &doctor.Fix{Guide: "Open System Settings and turn the toggle off.", Open: "x-apple.systempreferences:x"}
	return []doctor.Check{
		fixStep("autologout", doctor.OK, "key not set: the system default applies (default off)", guided),
		fixStep("workharbor-user", doctor.Fail, "there is no user workharbor", create),
		fixStep("tailscale", doctor.NotVerified, "tailscale did not answer: exit status 1: raw words", nil),
		fixStep("power", doctor.Fail, "sleep is on", guided),
	}
}

func TestGoldenSetupOutput(t *testing.T) {
	for name, style := range map[string]render.Style{
		"setup_tty":     render.Detect(true, "", false),
		"setup_plain":   render.Detect(false, "", false),
		"setup_nocolor": render.Detect(true, "1", false),
	} {
		var out, errb bytes.Buffer
		o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb, Style: style, Resume: []string{"whr", "setup", "host"}}
		outs, err := Run(bg, goldenSteps(), &fakeHost{}, o)
		if err != nil {
			t.Fatal(err)
		}
		Summary(&errb, outs, o)
		goldenFile(t, name, errb.String())
		if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "fail\tworkharbor-user\tthere is no user workharbor\n") {
			t.Errorf("%s: stdout is data and stays plain: %q", name, out.String())
		}
	}
}

// One message per problem: the fact is in the report line once, never again as
// heading or detail, and raw tool text is only shown with Verbose.
func TestEachProblemIsPrintedOnceAndRawToolTextOnlyWhenVerbose(t *testing.T) {
	var out, errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb}
	if _, err := Run(bg, goldenSteps(), &fakeHost{}, o); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(errb.String(), "there is no user workharbor"); n != 1 {
		t.Errorf("the problem is printed %d times:\n%s", n, errb.String())
	}
	if strings.Contains(errb.String(), "raw words") {
		t.Errorf("raw tool text without --verbose:\n%s", errb.String())
	}
	errb.Reset()
	o.Verbose = true
	if _, err := Run(bg, goldenSteps(), &fakeHost{}, o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "raw words") || !strings.Contains(errb.String(), "tool output:") {
		t.Errorf("--verbose lacks the raw tool text:\n%s", errb.String())
	}
	if strings.Count(errb.String(), "raw words") != 1 {
		t.Errorf("raw tool text twice:\n%s", errb.String())
	}
}

func TestStepHeaderNumbersTheSteps(t *testing.T) {
	var errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &bytes.Buffer{}, Err: &errb}
	if _, err := Run(bg, goldenSteps(), &fakeHost{}, o); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Step 1 of 4 - title of autologout", "Step 2 of 4 - title of workharbor-user", "Step 4 of 4 -", "Legend"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("lacks %q", want)
		}
	}
	if strings.Index(errb.String(), "Legend") > strings.Index(errb.String(), "Step 1 of 4") {
		t.Error("the legend comes at the start")
	}
}

func TestPromptDefaultsPerStepClass(t *testing.T) {
	cmd := []doctor.Cmd{{Argv: []string{"echo", "x"}}}
	for name, tc := range map[string]struct {
		fix  *doctor.Fix
		want []render.Default
	}{
		"reversible command":   {&doctor.Fix{Cmds: cmd}, []render.Default{render.DefaultYes}},
		"irreversible command": {&doctor.Fix{Cmds: cmd, Irreversible: true}, []render.Default{render.DefaultNo}},
		"guided":               {&doctor.Fix{Guide: "do it", Open: "x"}, []render.Default{render.DefaultYes, render.DefaultYes}},
	} {
		h := &askHost{answers2: []render.Answer{render.Yes, render.Yes}}
		s := fixStep("a", doctor.Fail, "broken", tc.fix)
		if _, err := Run(bg, []doctor.Check{s}, h, Options{Phase: doctor.PhaseHost, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); err != nil {
			t.Fatal(err)
		}
		if len(h.defaults) != len(tc.want) {
			t.Errorf("%s: asked %v", name, h.qs)
			continue
		}
		for i := range tc.want {
			if h.defaults[i] != tc.want[i] {
				t.Errorf("%s: question %q has default %v, want %v", name, h.qs[i], h.defaults[i], tc.want[i])
			}
		}
	}
}

func TestQuitStopsCleanlyWithAResumeHintAndRunsNothing(t *testing.T) {
	h := &askHost{answers2: []render.Answer{render.Quit}}
	steps := []doctor.Check{
		fixStep("first", doctor.OK, "fine", nil),
		fixStep("power", doctor.Fail, "broken", &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"pmset"}}}}),
		fixStep("later", doctor.Fail, "broken", &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}}),
	}
	outs, err := Run(bg, steps, h, Options{Phase: doctor.PhaseHost, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Resume: []string{"whr", "setup", "host"}})
	var q *QuitError
	if !errors.As(err, &q) || !errors.Is(err, render.ErrQuit) {
		t.Fatalf("err = %v", err)
	}
	if q.Resume != "whr setup host --from power" {
		t.Errorf("resume hint %q", q.Resume)
	}
	if len(h.ran) != 0 || len(outs) != 1 {
		t.Errorf("ran %v, outs %+v: quitting runs nothing and leaves the later steps alone", h.ran, outs)
	}
}

func TestQuitWithOnlyResumesWithTheStepsLeft(t *testing.T) {
	h := &askHost{answers2: []render.Answer{render.Quit}}
	steps := []doctor.Check{
		fixStep("a", doctor.Fail, "x", &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}}),
		fixStep("b", doctor.Fail, "x", &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}}),
	}
	_, err := Run(bg, steps, h, Options{Phase: doctor.PhaseHost, Only: []string{"a", "b"}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Resume: []string{"whr", "setup", "host"}})
	var q *QuitError
	if !errors.As(err, &q) || q.Resume != "whr setup host --only a --only b" {
		t.Fatalf("err = %v", err)
	}
}

func TestAHostWithoutPromptsFallsBackToConfirm(t *testing.T) {
	h := &fakeHost{answers: []string{"y"}}
	if got, err := Ask(h, "Go?", render.DefaultYes); err != nil || got != render.Yes {
		t.Errorf("%v %v", got, err)
	}
	if got, _ := Ask(h, "Go?", render.DefaultYes); got != render.No {
		t.Errorf("no answer is no: %v", got)
	}
}

func TestTerminalAsksWithThePromptsOfTheStepClass(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		def  render.Default
		want render.Answer
		sfx  string
	}{
		"Enter yes": {"\n", render.DefaultYes, render.Yes, "[Y/n/q]"},
		"Enter no":  {"\n", render.DefaultNo, render.No, "[y/N/q]"},
		"q":         {"q\n", render.DefaultYes, render.Quit, "[Y/n/q]"},
	} {
		var errb bytes.Buffer
		term := Terminal{In: bufioReader(tc.in), Err: &errb}
		got, err := term.Ask("Ready to create the account?", tc.def)
		if err != nil || got != tc.want || !strings.Contains(errb.String(), "Ready to create the account? "+tc.sfx) {
			t.Errorf("%s: %v %v %q", name, got, err, errb.String())
		}
	}
}

func bufioReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

// No line of the default output is wider than 90 runes, and the raw text of a
// tool (exit status, its error message) is neither on the human lines nor on the
// data line unless --verbose (issue #369).
func TestOutputLinesStayNarrowAndHoldNoRawToolText(t *testing.T) {
	raw := "defaults did not answer, so the setting is not known: exit status 1: Error: Could not find key 'com.apple.autologout.AutoLogOutDelay' in domain 'kCFPreferencesAnyApplication'."
	long := &doctor.Fix{
		Guide: "Open System Settings → Privacy & Security → Advanced and turn off \"Log out automatically after inactivity\". whr does not change it for you.",
		Open:  "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension",
	}
	steps := append(goldenSteps(), fixStep("autologout2", doctor.NotVerified, raw, long))
	var out, errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb, Resume: []string{"whr", "setup", "host"}}
	outs, err := Run(bg, steps, &fakeHost{}, o)
	if err != nil {
		t.Fatal(err)
	}
	Summary(&errb, outs, o)
	for _, l := range strings.Split(errb.String()+out.String(), "\n") {
		if n := len([]rune(l)); n > 90 {
			t.Errorf("line of %d runes: %q", n, l)
		}
	}
	for _, s := range []string{"exit status", "AutoLogOutDelay", "kCFPreferences"} {
		if strings.Contains(errb.String()+out.String(), s) {
			t.Errorf("raw tool text %q in the default output:\n%s%s", s, errb.String(), out.String())
		}
	}
}

// pauseHost counts the pages it was asked to pause at and quits at quitAt (0: never).
type pauseHost struct {
	fakeHost
	pauses, quitAt int
}

func (p *pauseHost) Pause() error {
	p.pauses++
	if p.pauses == p.quitAt {
		return render.ErrQuit
	}
	return nil
}

func TestPagedRunPausesBeforeEveryStepAndQuitsAtQ(t *testing.T) {
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Paged: true, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	h := &pauseHost{}
	if _, err := Run(bg, goldenSteps(), h, o); err != nil {
		t.Fatal(err)
	}
	if h.pauses != 4 {
		t.Errorf("pauses = %d, want one per step page (4)", h.pauses)
	}
	h = &pauseHost{quitAt: 2}
	outs, err := Run(bg, goldenSteps(), h, o)
	var q *QuitError
	if !errors.As(err, &q) || q.Step != "workharbor-user" || len(outs) != 1 {
		t.Errorf("q at the second page: outs=%d err=%v", len(outs), err)
	}
}

// Ctrl-C at the Press Enter prompt stops the run before the step starts.
func TestCtrlCAtThePauseStopsBeforeTheStep(t *testing.T) {
	ctx, cancel := context.WithCancel(bg)
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Paged: true, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	h := &cancelPauseHost{cancel: cancel}
	outs, err := Run(ctx, goldenSteps(), h, o)
	var ie *InterruptedError
	if !errors.As(err, &ie) || ie.When != BeforeStep || len(outs) != 0 {
		t.Errorf("outs=%d err=%v", len(outs), err)
	}
}

type cancelPauseHost struct {
	fakeHost
	cancel context.CancelFunc
}

func (p *cancelPauseHost) Pause() error { p.cancel(); return nil }

// Without a terminal nothing pauses: no prompt, and a blocking input is never read.
func TestUnpagedRunHasNoPressEnterAndNeverBlocks(t *testing.T) {
	pr, pw := io.Pipe() // an input that never gives a byte
	defer pw.Close()
	var errb bytes.Buffer
	h := Terminal{In: bufio.NewReader(pr), Err: &errb}
	done := make(chan struct{})
	go func() {
		defer close(done)
		o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &bytes.Buffer{}, Err: &errb}
		_, _ = Run(bg, goldenSteps(), h, o)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the run blocked on the input")
	}
	if strings.Contains(errb.String(), "Press Enter") {
		t.Errorf("a Press Enter prompt without a terminal:\n%s", errb.String())
	}
}

func TestTerminalPauseTakesEnterAndQ(t *testing.T) {
	var errb bytes.Buffer
	if err := (Terminal{In: bufio.NewReader(strings.NewReader("\n")), Err: &errb}).Pause(); err != nil {
		t.Errorf("Enter: %v", err)
	}
	if !strings.Contains(errb.String(), "Press Enter to continue (q to quit)") {
		t.Errorf("prompt: %q", errb.String())
	}
	if err := (Terminal{In: bufio.NewReader(strings.NewReader("q\n")), Err: &errb}).Pause(); !errors.Is(err, render.ErrQuit) {
		t.Errorf("q: %v", err)
	}
	if err := (Terminal{In: bufio.NewReader(strings.NewReader("Quit\n")), Err: &errb}).Pause(); !errors.Is(err, render.ErrQuit) {
		t.Errorf("quit: %v", err)
	}
	if err := (Terminal{In: bufio.NewReader(strings.NewReader("")), Err: &errb}).Pause(); err != nil {
		t.Errorf("closed input must not block or fail: %v", err)
	}
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// A page is the legend, one step or the summary: none is longer than 25 lines,
// and no line is wider than 90 runes.
func TestGoldenPagesStayShort(t *testing.T) {
	for _, name := range []string{"setup_tty", "setup_plain", "setup_nocolor"} {
		b, err := os.ReadFile(filepath.Join("testdata", name+".golden")) //nolint:gosec // a golden file of this package
		if err != nil {
			t.Fatal(err)
		}
		page := 0
		for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if strings.Contains(line, " Step ") && strings.HasPrefix(line, "-") || strings.HasPrefix(line, "\x1b") && strings.Contains(line, " Step ") {
				page = 0
			}
			page++
			if page > 25 {
				t.Errorf("%s: a page is longer than 25 lines at %q", name, line)
			}
			if n := utf8.RuneCountInString(ansi.ReplaceAllString(line, "")); n > 90 {
				t.Errorf("%s: %d runes wide: %q", name, n, line)
			}
		}
	}
}

// The last page (the last step, the summary and the next steps) of a host with
// many failing steps: how long it gets.
func TestLastPageLengthWithManyFailingSteps(t *testing.T) {
	guided := &doctor.Fix{Guide: "Open System Settings and turn the toggle off."}
	var steps []doctor.Check
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		steps = append(steps, fixStep("step-"+n, doctor.Fail, "broken "+n, guided))
	}
	var out, errb bytes.Buffer
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Out: &out, Err: &errb, Resume: []string{"whr", "setup", "host"}}
	outs, err := Run(bg, steps, &fakeHost{}, o)
	if err != nil {
		t.Fatal(err)
	}
	Summary(&errb, outs, o)
	lines := strings.Split(strings.TrimRight(errb.String(), "\n"), "\n")
	last := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "-- Step 8 of 8") {
			last = i
		}
	}
	if n := len(lines) - last; n > 25 {
		t.Errorf("the last page is %d lines long, want at most 25", n)
	}
}

func TestPauseReturnsOnCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Terminal{In: bufio.NewReader(pr), Err: io.Discard}.PauseContext(ctx)
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pause kept waiting for Enter after the cancel")
	}
}

// ctxPauseHost is a ContextPauser that waits for the context, as Terminal does:
// Run must call PauseContext, never the plain Pause that would wait for Enter.
type ctxPauseHost struct {
	fakeHost
	plainPause bool
}

func (p *ctxPauseHost) Pause() error { p.plainPause = true; return nil }

func (p *ctxPauseHost) PauseContext(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestRunUsesPauseContextAndStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel()
	o := Options{Phase: doctor.PhaseHost, DryRun: true, Paged: true, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	h := &ctxPauseHost{}
	outs, err := Run(ctx, goldenSteps(), h, o)
	var ie *InterruptedError
	if !errors.As(err, &ie) || ie.When != BeforeStep || len(outs) != 0 {
		t.Errorf("outs=%d err=%v", len(outs), err)
	}
	if h.plainPause {
		t.Error("Run called Pause although the host is a ContextPauser")
	}
}

// A dry run labels the commands PLAN; a real run keeps ACTION.
func TestShowFixLabelsPlanInADryRun(t *testing.T) {
	fix := &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"echo", "hi"}}}}
	for _, dry := range []bool{true, false} {
		var b bytes.Buffer
		showFix(render.Writer{W: &b}, fix, dry)
		got := b.String()
		if dry != (strings.Contains(got, "PLAN") && !strings.Contains(got, "ACTION")) ||
			!dry != (strings.Contains(got, "ACTION") && !strings.Contains(got, "PLAN")) {
			t.Errorf("dry=%v:\n%s", dry, got)
		}
	}
}
