package setup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup/protocol"
)

// realRunHost runs every command through the real Terminal.Run, but with its own
// argv instead of the one asked for (never a real sudo), so the errors are the
// ones os/exec gives: "signal: killed", not the context's own error.
type realRunHost struct {
	fakeHost
	argv []string
	runs []string
}

func (h *realRunHost) Run(ctx context.Context, c doctor.Cmd) error {
	h.runs = append(h.runs, strings.Join(c.Full(), " "))
	return Terminal{Err: io.Discard}.Run(ctx, doctor.Cmd{Argv: h.argv})
}

func interruptRun(t *testing.T, h *realRunHost, steps []doctor.Check, phase doctor.Phase, timeout time.Duration) (*rec, error) {
	t.Helper()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r := &rec{ev: &events{}}
	var so, se bytes.Buffer
	start := time.Now()
	_, err := Run(ctx, steps, h, Options{Phase: phase, Out: &so, Err: &se, Log: r})
	if time.Since(start) > 4*time.Second {
		t.Errorf("the run took %v: the command was not stopped", time.Since(start))
	}
	return r, err
}

func wantInterrupted(t *testing.T, r *rec, err error) {
	t.Helper()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the deadline", err)
	}
	if end := r.find(protocol.EventRunEnd, ""); len(end) != 1 || end[0].Outcome != protocol.RunInterrupted {
		t.Errorf("run.end = %+v, want interrupted", end)
	}
	if after := r.find(protocol.EventStepAfter, "one"); len(after) != 1 || after[0].Outcome != protocol.OutInterrupted {
		t.Errorf("step.after = %+v, want interrupted", after)
	}
}

func TestARealCommandKilledByTheDeadlineInterruptsTheFirstStep(t *testing.T) {
	var a, b bool
	h := &realRunHost{fakeHost: fakeHost{answers: []string{"y", "y"}}, argv: []string{"sleep", "5"}}
	steps := []doctor.Check{
		step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}}),
		step("two", doctor.PhaseUser, &b, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"y"}}}}),
	}
	r, err := interruptRun(t, h, steps, doctor.PhaseUser, 300*time.Millisecond)
	wantInterrupted(t, r, err)
	if len(h.runs) != 1 || len(r.find(protocol.EventStepBefore, "two")) != 0 {
		t.Errorf("a step after the interrupt started: %v", h.runs)
	}
}

func TestARealCommandKilledByTheDeadlineInterruptsTheLastStep(t *testing.T) {
	var a bool
	h := &realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"sleep", "5"}}
	steps := []doctor.Check{step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})}
	r, err := interruptRun(t, h, steps, doctor.PhaseUser, 300*time.Millisecond)
	wantInterrupted(t, r, err)
}

func TestADeadlineDuringTheRecheckOfTheLastStepInterrupts(t *testing.T) {
	h := &realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"true"}}
	ran := false
	c := doctor.Check{
		Name: "one", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}},
		Run: func(ctx context.Context) (doctor.Status, string) {
			if !ran {
				ran = true
				return doctor.Fail, "broken"
			}
			<-ctx.Done() // the re-check is cut short and says nothing wrong
			return doctor.Fail, "unknown"
		},
	}
	r, err := interruptRun(t, h, []doctor.Check{c}, doctor.PhaseUser, 300*time.Millisecond)
	wantInterrupted(t, r, err)
}

func TestADeadlineDuringTheSudoPrimeInterrupts(t *testing.T) {
	var a bool
	h := &realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"sleep", "5"}}
	steps := []doctor.Check{step("one", doctor.PhaseHost, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"x"}}}})}
	r, err := interruptRun(t, h, steps, doctor.PhaseHost, 300*time.Millisecond)
	wantInterrupted(t, r, err)
	if err == nil || strings.Contains(err.Error(), "did not accept the password") {
		t.Errorf("err = %v, want the interrupt and not a refused password", err)
	}
	if len(h.runs) != 1 || h.runs[0] != "sudo -v" {
		t.Errorf("ran %v, want only sudo -v", h.runs)
	}
}

func TestAFailingCommandWithALiveContextStaysAFailure(t *testing.T) {
	var a bool
	h := &realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"false"}}
	steps := []doctor.Check{step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})}
	r, err := interruptRun(t, h, steps, doctor.PhaseUser, time.Minute)
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if after := r.find(protocol.EventStepAfter, "one"); len(after) != 1 || after[0].Outcome != protocol.OutFixFailed {
		t.Errorf("step.after = %+v, want fix_failed", after)
	}
	if end := r.find(protocol.EventRunEnd, ""); len(end) != 1 || end[0].Outcome != protocol.RunLeft {
		t.Errorf("run.end = %+v, want left", end)
	}
}

func runWith(ctx context.Context, t *testing.T, h Host, steps []doctor.Check) (*rec, error) {
	t.Helper()
	r := &rec{ev: &events{}}
	var so, se bytes.Buffer
	_, err := Run(ctx, steps, h, Options{Phase: doctor.PhaseUser, Out: &so, Err: &se, Log: r, Resume: []string{"whr", "setup"}})
	return r, err
}

func runEnd(t *testing.T, r *rec) string {
	t.Helper()
	end := r.find(protocol.EventRunEnd, "")
	if len(end) != 1 {
		t.Fatalf("run.end = %+v", end)
	}
	return end[0].Outcome
}

// The command dies of the signal before whr's own signal handling has cancelled
// the context: still an interrupt, deterministically.
func TestACommandKilledByCtrlCBeforeTheContextIsCancelledIsAnInterrupt(t *testing.T) {
	for _, sig := range []string{"INT", "TERM", "HUP"} {
		var a, b bool
		h := &realRunHost{fakeHost: fakeHost{answers: []string{"y", "y"}}, argv: []string{"sh", "-c", "kill -" + sig + " $$"}}
		steps := []doctor.Check{
			step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}}),
			step("two", doctor.PhaseUser, &b, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"y"}}}}),
		}
		r, err := runWith(context.Background(), t, h, steps)
		var ie *InterruptedError
		if !errors.As(err, &ie) || !errors.Is(err, context.Canceled) || ie.Step != "one" || !strings.Contains(ie.Resume, "--from one") {
			t.Errorf("%s: err = %v", sig, err)
		}
		if after := r.find(protocol.EventStepAfter, "one"); len(after) != 1 || after[0].Outcome != protocol.OutInterrupted {
			t.Errorf("%s: step.after = %+v", sig, after)
		}
		if got := runEnd(t, r); got != protocol.RunInterrupted || len(h.runs) != 1 {
			t.Errorf("%s: run.end %s, runs %v", sig, got, h.runs)
		}
	}
	// any other death is an ordinary failure
	var a bool
	h := &realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"sh", "-c", "kill -KILL $$"}}
	r, err := runWith(context.Background(), t, h, []doctor.Check{step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})})
	if err != nil || runEnd(t, r) != protocol.RunLeft {
		t.Errorf("SIGKILL: err %v, run.end %s", err, runEnd(t, r))
	}
}

// A cancel that lands while a step is only being checked ends the run as
// interrupted, whatever the check said and whether or not it was the last step.
func TestACancelDuringAStepWithNothingToFixInterrupts(t *testing.T) {
	for name, status := range map[string]doctor.Status{
		"not verified, no fix": doctor.NotVerified, "warn": doctor.Warn, "skipped": doctor.Skipped, "ok": doctor.OK,
	} {
		ctx, cancel := context.WithCancel(context.Background())
		c := doctor.Check{Name: "one", Phase: doctor.PhaseUser, Run: func(context.Context) (doctor.Status, string) {
			cancel()
			return status, "cut short"
		}}
		r, err := runWith(ctx, t, &fakeHost{}, []doctor.Check{c})
		if !errors.Is(err, context.Canceled) || runEnd(t, r) != protocol.RunInterrupted {
			t.Errorf("%s: err %v, run.end %s", name, err, runEnd(t, r))
		}
		cancel()
	}
}

type cancelOnAsk struct {
	fakeHost
	cancel context.CancelFunc
}

func (h *cancelOnAsk) Confirm(q string) (bool, error) {
	h.cancel()
	return h.fakeHost.Confirm(q)
}

func TestNoFixRunsAfterACancelAtTheQuestion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var fixed, did, built bool
	fix := &doctor.Fix{
		Cmds:  []doctor.Cmd{{Argv: []string{"x"}}},
		Do:    func(context.Context, doctor.Prompter) error { did = true; return nil },
		Build: func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) { built = true; return nil, nil },
	}
	h := &cancelOnAsk{fakeHost: fakeHost{answers: []string{"y"}}, cancel: cancel}
	r, err := runWith(ctx, t, h, []doctor.Check{step("one", doctor.PhaseUser, &fixed, fix)})
	if did || built || len(h.ran) != 0 {
		t.Errorf("a fix started after the cancel: do %v build %v ran %v", did, built, h.ran)
	}
	if !errors.Is(err, context.Canceled) || runEnd(t, r) != protocol.RunInterrupted {
		t.Errorf("err %v, run.end %s", err, runEnd(t, r))
	}
	if after := r.find(protocol.EventStepAfter, "one"); len(after) != 1 || after[0].Outcome != protocol.OutInterrupted {
		t.Errorf("step.after = %+v", after)
	}
}

func TestACancelBeforeTheQuestionAsksNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var fixed bool
	c := step("one", doctor.PhaseUser, &fixed, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})
	run := c.Run
	c.Run = func(ctx context.Context) (doctor.Status, string) { cancel(); return run(ctx) }
	h := &fakeHost{answers: []string{"y"}}
	r, err := runWith(ctx, t, h, []doctor.Check{c})
	if len(h.asked) != 0 || len(h.ran) != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("asked %v ran %v err %v", h.asked, h.ran, err)
	}
	if got := runEnd(t, r); got != protocol.RunInterrupted {
		t.Errorf("run.end %s", got)
	}
}

type cancelAfterRun struct {
	fakeHost
	cancel context.CancelFunc
	fixed  *bool
}

func (h *cancelAfterRun) Run(ctx context.Context, c doctor.Cmd) error {
	_ = h.fakeHost.Run(ctx, c)
	*h.fixed = true
	h.cancel()
	return nil
}

func TestAStepThatFixedItselfIsNotRecordedInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var fixed bool
	h := &cancelAfterRun{fakeHost: fakeHost{answers: []string{"y"}}, cancel: cancel, fixed: &fixed}
	r, err := runWith(ctx, t, h, []doctor.Check{step("one", doctor.PhaseUser, &fixed, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})})
	if after := r.find(protocol.EventStepAfter, "one"); len(after) != 1 || after[0].Outcome != protocol.OutFixed {
		t.Errorf("step.after = %+v, want fixed", after)
	}
	if !errors.Is(err, context.Canceled) || runEnd(t, r) != protocol.RunInterrupted {
		t.Errorf("err %v, run.end %s", err, runEnd(t, r))
	}
}

func TestABuilderThatReturnsNoSudoCommandGetsNoSudoValidation(t *testing.T) {
	var a bool
	fix := &doctor.Fix{
		Cmds: []doctor.Cmd{{Argv: []string{"x"}}},
		Build: func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) {
			return []doctor.Cmd{{Argv: []string{"x", "v"}}}, nil
		},
	}
	h := &fakeHost{answers: []string{"y"}}
	run(t, h, []doctor.Check{step("one", doctor.PhaseHost, &a, fix)}, Options{Phase: doctor.PhaseHost})
	if got := strings.Join(h.ran, "|"); got != "x v" {
		t.Errorf("ran %q", got)
	}
}

func TestOutputClassifiesSignalDeathAsInterrupt(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	// a command killed by SIGTERM before any context ends: Ctrl-C reached it first
	_, err := Terminal{}.Output(context.Background(), "sh", "-c", "kill -TERM $$")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("signalled command: err = %v, want a cancel", err)
	}
	// a deadline that kills a real child
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = Terminal{}.Output(ctx, "sleep", "5")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
		t.Errorf("deadline: err = %v after %v", err, time.Since(start))
	}
	// a plain failure and a signal that is not an interrupt stay failures
	for _, script := range []string{"exit 3", "kill -KILL $$"} {
		if _, err := (Terminal{}).Output(context.Background(), "sh", "-c", script); err == nil || errors.Is(err, context.Canceled) {
			t.Errorf("%q: err = %v, want a plain failure", script, err)
		}
	}
}

// sigHost is a fake host that, like the real Terminal, records an interrupt a
// command died of.
type sigHost struct {
	fakeHost
	sig *Interrupts
}

func (h *sigHost) Interrupted() bool { return h.sig.Seen() }
func (h *sigHost) ResetInterrupts()  { h.sig.Reset() }

// selfKillCheck is a check whose real command kills itself with SIGINT while the
// context is still live, as a child does that Ctrl-C reached before whr did;
// the check can say only a status. With firstFails the first call reports a
// plain failure, so the step has a fix and the killed command is the re-check.
func selfKillCheck(name string, sig *Interrupts, status doctor.Status, fix *doctor.Fix, firstFails bool) doctor.Check {
	calls := 0
	return doctor.Check{Name: name, Phase: doctor.PhaseUser, Fix: fix, Run: func(ctx context.Context) (doctor.Status, string) {
		calls++
		if firstFails && calls == 1 {
			return doctor.Fail, "broken"
		}
		if _, err := (Terminal{Sig: sig}).Output(ctx, "sh", "-c", "kill -INT $$"); err != nil {
			return status, err.Error()
		}
		return doctor.OK, "ok"
	}}
}

func TestACheckKilledBySignalStopsTheRunAndNamesItsStep(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	fix := &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}}
	for _, st := range []doctor.Status{doctor.NotVerified, doctor.Warn, doctor.Skipped} {
		for _, last := range []bool{true, false} {
			for _, recheck := range []bool{false, true} {
				if recheck && st != doctor.NotVerified {
					continue
				}
				name := string(st) + map[bool]string{true: " last", false: " first of two"}[last] + map[bool]string{true: " recheck", false: ""}[recheck]
				var b bool
				sig := &Interrupts{}
				one := selfKillCheck("one", sig, st, nil, false)
				if recheck {
					one = selfKillCheck("one", sig, st, fix, true)
				}
				steps := []doctor.Check{one}
				if !last {
					steps = append(steps, step("two", doctor.PhaseUser, &b, fix))
				}
				h := &sigHost{fakeHost: fakeHost{answers: []string{"y", "y"}}, sig: sig}
				r := &rec{ev: &events{}}
				// the context is never cancelled: only the signal death tells
				_, err := Run(context.Background(), steps, h, Options{Phase: doctor.PhaseUser, Out: io.Discard, Err: io.Discard, Log: r})
				var ie *InterruptedError
				if !errors.As(err, &ie) || ie.Step != "one" || ie.When != DuringStep {
					t.Errorf("%s: err = %v", name, err)
				}
				if ie != nil && !strings.Contains(ie.Resume, "--from one") {
					t.Errorf("%s: resume %q does not name step one", name, ie.Resume)
				}
				if after := r.find(protocol.EventStepAfter, "one"); len(after) != 1 || after[0].Outcome != protocol.OutInterrupted {
					t.Errorf("%s: step.after = %+v", name, after)
				}
				if len(r.find(protocol.EventStepBefore, "two")) != 0 {
					t.Errorf("%s: step two started", name)
				}
				if runEnd(t, r) != protocol.RunInterrupted {
					t.Errorf("%s: run.end %s", name, runEnd(t, r))
				}
			}
		}
	}
}

func TestAFixThatExits130OrSudoThatFailsAtCtrlCIsAnInterrupt(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	// a fix that traps SIGINT and exits 130, with a live context
	var a bool
	sig := &Interrupts{}
	h := &sigHostRun{realRunHost: realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"sh", "-c", "exit 130"}}, sig: sig}
	steps := []doctor.Check{step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})}
	r := &rec{ev: &events{}}
	_, err := Run(context.Background(), steps, h, Options{Phase: doctor.PhaseUser, Out: io.Discard, Err: io.Discard, Log: r})
	var ie *InterruptedError
	if !errors.As(err, &ie) || runEnd(t, r) != protocol.RunInterrupted {
		t.Errorf("exit 130: err = %v, run.end %s", err, runEnd(t, r))
	}
	// sudo -v exits 1 at Ctrl-C and the context ends a moment later
	old := interruptGrace
	interruptGrace = 2 * time.Second
	defer func() { interruptGrace = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(60*time.Millisecond, cancel)
	h2 := &realRunHost{fakeHost: fakeHost{answers: []string{"y"}}, argv: []string{"false"}}
	steps = []doctor.Check{step("one", doctor.PhaseHost, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: true, Argv: []string{"x"}}}})}
	r = &rec{ev: &events{}}
	_, err = Run(ctx, steps, h2, Options{Phase: doctor.PhaseHost, Out: io.Discard, Err: io.Discard, Log: r})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "did not accept the password") || runEnd(t, r) != protocol.RunInterrupted {
		t.Errorf("sudo -v exit 1: err = %v, run.end %s", err, runEnd(t, r))
	}
}

// sigHostRun is realRunHost with Terminal's record of interrupts: its Run goes
// through a Terminal that shares sig.
type sigHostRun struct {
	realRunHost
	sig *Interrupts
}

func (h *sigHostRun) Run(ctx context.Context, c doctor.Cmd) error {
	h.runs = append(h.runs, strings.Join(c.Full(), " "))
	return Terminal{Err: io.Discard, Sig: h.sig}.Run(ctx, doctor.Cmd{Argv: h.argv})
}
func (h *sigHostRun) Interrupted() bool { return h.sig.Seen() }
func (h *sigHostRun) ResetInterrupts()  { h.sig.Reset() }

func TestApplyAsksNothingWhenInterruptedBeforeTheQuestion(t *testing.T) {
	for name, mk := range map[string]func() (context.Context, Host){
		"context": func() (context.Context, Host) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, &fakeHost{answers: []string{"y"}}
		},
		"signal death": func() (context.Context, Host) {
			sig := &Interrupts{}
			sig.note()
			return context.Background(), &sigHost{fakeHost: fakeHost{answers: []string{"y"}}, sig: sig}
		},
	} {
		ctx, h := mk()
		var a bool
		s := step("one", doctor.PhaseUser, &a, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})
		r := &rec{ev: &events{}}
		rn := &runner{h: h, p: h, o: Options{Phase: doctor.PhaseUser}, ui: render.Writer{W: io.Discard}, rc: &recorder{log: r, phase: doctor.PhaseUser}}
		var out Outcome
		res, err := rn.apply(ctx, s, &out)
		if !errors.Is(err, context.Canceled) || res.outcome != protocol.OutInterrupted {
			t.Errorf("%s: err = %v, outcome %q", name, err, res.outcome)
		}
		var asked, ran int
		switch f := h.(type) {
		case *fakeHost:
			asked, ran = len(f.asked), len(f.ran)
		case *sigHost:
			asked, ran = len(f.asked), len(f.ran)
		}
		if asked != 0 || ran != 0 {
			t.Errorf("%s: asked %d, ran %d: nothing may be asked or run", name, asked, ran)
		}
	}
}

func TestACancelBetweenStepsSaysTheNextStepHadNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var a, b bool
	one := step("one", doctor.PhaseUser, &a, nil)
	two := step("two", doctor.PhaseUser, &b, &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"x"}}}})
	h := &fakeHost{}
	one.Run = func(context.Context) (doctor.Status, string) { cancel(); return doctor.OK, "ok" }
	_, err := Run(ctx, []doctor.Check{one, two}, h, Options{Phase: doctor.PhaseUser, Out: io.Discard, Err: io.Discard})
	var ie *InterruptedError
	if !errors.As(err, &ie) || ie.Step != "two" || ie.When != BeforeStep || !strings.Contains(ie.Error(), "before step two") {
		t.Errorf("err = %v", err)
	}
}
