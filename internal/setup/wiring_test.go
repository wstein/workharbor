package setup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup/answers"
	"github.com/wstein/workharbor/internal/setup/protocol"
)

// events is the order of what happened, across the host and the log.
type events struct{ list []string }

func (e *events) add(s string) { e.list = append(e.list, s) }

// wireHost is a Host whose Run fixes the step and whose Ask is scripted.
type wireHost struct {
	fakeHost
	ev    *events
	fixed *bool
	asks  []render.Answer // answers to Ask, in order
	asked []string        // the questions of Ask
	// failRun makes every command fail.
	failRun error
}

func (h *wireHost) Run(ctx context.Context, c doctor.Cmd) error {
	h.ev.add("run:" + strings.Join(c.Full(), " "))
	if h.failRun != nil {
		return h.failRun
	}
	*h.fixed = true
	return h.fakeHost.Run(ctx, c)
}

func (h *wireHost) Ask(q string, _ render.Default) (render.Answer, error) {
	h.asked = append(h.asked, q)
	if len(h.asks) == 0 {
		return render.No, nil
	}
	a := h.asks[0]
	h.asks = h.asks[1:]
	return a, nil
}

// rec is a Recorder that keeps the entries and shares the event order.
type rec struct {
	ev      *events
	entries []protocol.Entry
	failOn  string // an event name whose Append fails
	err     error
}

func (r *rec) Append(e protocol.Entry) error {
	if e.Event == r.failOn {
		return r.err
	}
	r.entries = append(r.entries, e)
	r.ev.add(e.Event + ":" + e.Step + ":" + e.Source + ":" + e.Answer + e.Outcome)
	return nil
}

func (r *rec) find(event, step string) []protocol.Entry {
	var out []protocol.Entry
	for _, e := range r.entries {
		if e.Event == event && e.Step == step {
			out = append(out, e)
		}
	}
	return out
}

type rig struct {
	h     *wireHost
	r     *rec
	fixed bool
	ev    *events
}

func newRig() *rig {
	g := &rig{ev: &events{}}
	g.h = &wireHost{ev: g.ev, fixed: &g.fixed}
	g.r = &rec{ev: g.ev}
	return g
}

// check is a step whose check passes once a command ran.
func (g *rig) check(name string, phase doctor.Phase, fix *doctor.Fix) doctor.Check {
	return doctor.Check{Name: name, Phase: phase, Fix: fix, Run: func(context.Context) (doctor.Status, string) {
		if g.fixed {
			return doctor.OK, "fine"
		}
		return doctor.Fail, "broken"
	}}
}

func cmdFix(sudo bool, argv ...string) *doctor.Fix {
	return &doctor.Fix{Cmds: []doctor.Cmd{{Sudo: sudo, Argv: argv}}}
}

func fileFor(c doctor.Check, answer string) *answers.File {
	return &answers.File{Answers: []answers.Entry{{Step: c.Name, Fix: answers.FixDigest(c), Answer: answer}}}
}

const fileDigest = "abababababababababababababababababababababababababababababababab"

func (g *rig) opts(phase doctor.Phase, f *answers.File) Options {
	o := Options{Phase: phase, Log: g.r, Account: "workharbor", Home: "/Users/workharbor"}
	if f != nil {
		o.Answers, o.AnswersDigest = f, fileDigest
	}
	return o
}

func (g *rig) run(t *testing.T, steps []doctor.Check, o Options) ([]Outcome, string, error) {
	t.Helper()
	var so, se bytes.Buffer
	o.Out, o.Err = &so, &se
	outs, err := Run(bg, steps, g.h, o)
	return outs, se.String(), err
}

func TestAnAnswerReplacesTheOuterPromptAndIsRecordedWithTheFileDigest(t *testing.T) {
	g := newRig()
	c := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	outs, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run)))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.h.asked)+len(g.h.fakeHost.asked) != 0 {
		t.Errorf("the outer prompt was asked: %v %v", g.h.asked, g.h.fakeHost.asked)
	}
	if len(g.h.ran) != 1 || !outs[0].Fixed {
		t.Fatalf("ran %v, outs %+v", g.h.ran, outs)
	}
	start := g.r.entries[0]
	if start.Event != protocol.EventRunStart || start.Source != protocol.SourceAnswers || start.Answers != fileDigest {
		t.Errorf("run.start %+v", start)
	}
	b := g.r.find(protocol.EventStepBefore, "config-dir")
	if len(b) != 1 || b[0].Source != protocol.SourceAnswers || b[0].Answers != fileDigest || b[0].Answer != protocol.AnswerRun || b[0].Fix != answers.FixDigest(c) {
		t.Errorf("step.before %+v", b)
	}
	a := g.r.find(protocol.EventStepAfter, "config-dir")
	if len(a) != 1 || a[0].Outcome != protocol.OutFixed || a[0].Exit == nil || *a[0].Exit != 0 || a[0].Ran != protocol.RanDigest([][]string{{"mkdir", "x"}}) {
		t.Errorf("step.after %+v", a)
	}
	if end := g.r.entries[len(g.r.entries)-1]; end.Event != protocol.EventRunEnd || end.Outcome != protocol.RunDone {
		t.Errorf("run.end %+v", end)
	}
	if outs[0].Decision != answers.Run || outs[0].Fix != answers.FixDigest(c) {
		t.Errorf("decision %q fix %q", outs[0].Decision, outs[0].Fix)
	}
}

func TestStepBeforeIsWrittenBeforeTheFixRuns(t *testing.T) {
	g := newRig()
	c := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	if _, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run))); err != nil {
		t.Fatal(err)
	}
	before, ran := -1, -1
	for i, e := range g.ev.list {
		if strings.HasPrefix(e, "step.before:config-dir") {
			before = i
		}
		if strings.HasPrefix(e, "run:mkdir") {
			ran = i
		}
	}
	if before < 0 || ran < 0 || before > ran {
		t.Fatalf("order %v", g.ev.list)
	}
}

func TestASkipAnswerRunsNothing(t *testing.T) {
	g := newRig()
	c := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	outs, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Skip)))
	if err != nil || len(g.h.ran) != 0 || outs[0].Fixed || !outs[0].Asked {
		t.Fatalf("%v ran %v outs %+v", err, g.h.ran, outs)
	}
	if a := g.r.find(protocol.EventStepAfter, "config-dir"); len(a) != 1 || a[0].Outcome != protocol.OutDeclined {
		t.Errorf("%+v", a)
	}
	if outs[0].Decision != answers.Skip {
		t.Errorf("decision %q", outs[0].Decision)
	}
}

func TestAChangedCommandIsAskedAgain(t *testing.T) {
	g := newRig()
	old := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "old"))
	f := fileFor(old, answers.Run)
	now := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "new"))
	g.h.asks = []render.Answer{render.No}
	outs, _, err := g.run(t, []doctor.Check{now}, g.opts(doctor.PhaseUser, f))
	if err != nil || len(g.h.asked) != 1 || len(g.h.ran) != 0 || outs[0].Fixed {
		t.Fatalf("%v asked %v ran %v", err, g.h.asked, g.h.ran)
	}
	if b := g.r.find(protocol.EventStepBefore, "config-dir"); len(b) != 1 || b[0].Source != protocol.SourceInteractive || b[0].Answers != "" || b[0].Answer != protocol.AnswerSkip {
		t.Errorf("%+v", b)
	}
}

func TestTheHostPhaseNeverConsultsTheFile(t *testing.T) {
	g := newRig()
	c := g.check("power", doctor.PhaseHost, cmdFix(false, "pmset"))
	g.h.asks = []render.Answer{render.No}
	// a file that names the host step with its real digest is ignored
	f := fileFor(c, answers.Run)
	outs, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseHost, f))
	if err != nil || len(g.h.asked) != 1 || len(g.h.ran) != 0 || outs[0].Fixed {
		t.Fatalf("%v asked %v ran %v", err, g.h.asked, g.h.ran)
	}
	for _, e := range g.r.entries {
		if e.Source == protocol.SourceAnswers || e.Answers != "" {
			t.Errorf("the host phase recorded the file: %+v", e)
		}
	}
	if e := g.r.entries[0]; e.Source != protocol.SourceInteractive || e.Phase != "host" {
		t.Errorf("run.start %+v", e)
	}
}

func TestAStepThatNamesAnotherAccountIsAsked(t *testing.T) {
	g := newRig()
	c := g.check("account", doctor.PhaseUser, cmdFix(false, "x"))
	c.UseUser = func(doctor.Status) string { return "legacy" }
	g.h.asks = []render.Answer{render.No}
	if _, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run))); err != nil {
		t.Fatal(err)
	}
	if len(g.h.asked) != 1 || len(g.h.ran) != 0 {
		t.Fatalf("asked %v ran %v", g.h.asked, g.h.ran)
	}
}

func TestSudoFromABuilderIsNotDecidedByAFile(t *testing.T) {
	build := func(sudo bool) *doctor.Fix {
		return &doctor.Fix{
			Cmds: []doctor.Cmd{{Argv: []string{"x", "<value>"}}},
			Build: func(_ context.Context, p doctor.Prompter) ([]doctor.Cmd, error) {
				v, err := p.Line("a value?")
				if err != nil {
					return nil, err
				}
				return []doctor.Cmd{{Sudo: sudo, Argv: []string{"x", v}}}, nil
			},
		}
	}
	t.Run("refused asks again, no leaves it", func(t *testing.T) {
		g := newRig()
		c := g.check("thing", doctor.PhaseUser, build(true))
		g.h.lines = []string{"v1"}
		g.h.asks = []render.Answer{render.No}
		outs, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run)))
		if err != nil || len(g.h.asked) != 1 || len(g.h.ran) != 0 || outs[0].Fixed {
			t.Fatalf("%v asked %v ran %v", err, g.h.asked, g.h.ran)
		}
		if len(g.h.fakeHost.asked) != 1 { // the builder's own prompt stays interactive
			t.Errorf("builder prompts %v", g.h.fakeHost.asked)
		}
		b := g.r.find(protocol.EventStepBefore, "thing")
		if len(b) != 2 || b[0].Source != protocol.SourceAnswers || b[1].Source != protocol.SourceInteractive || b[1].Answer != protocol.AnswerSkip {
			t.Errorf("before entries %+v", b)
		}
		if outs[0].Decision != "" {
			t.Errorf("a re-asked step must not be saved: %q", outs[0].Decision)
		}
	})
	t.Run("yes runs it after the question", func(t *testing.T) {
		g := newRig()
		c := g.check("thing", doctor.PhaseUser, build(true))
		g.h.lines = []string{"v1"}
		g.h.asks = []render.Answer{render.Yes}
		if _, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run))); err != nil {
			t.Fatal(err)
		}
		if strings.Join(g.h.ran, "|") != "sudo x v1" {
			t.Errorf("ran %v", g.h.ran)
		}
		for _, e := range g.h.ran {
			if e == "sudo -v" {
				t.Error("the sudo -v path was taken for a decision from a file")
			}
		}
	})
	t.Run("without sudo the file decides", func(t *testing.T) {
		g := newRig()
		c := g.check("thing", doctor.PhaseUser, build(false))
		g.h.lines = []string{"v1"}
		if _, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run))); err != nil {
			t.Fatal(err)
		}
		if len(g.h.asked) != 0 || strings.Join(g.h.ran, "|") != "x v1" {
			t.Errorf("asked %v ran %v", g.h.asked, g.h.ran)
		}
	})
}

func TestAQuitIsNeverAutomaticAndIsRecorded(t *testing.T) {
	g := newRig()
	c := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	g.h.asks = []render.Answer{render.Quit}
	// the file has no answer for the step, so the person is asked and quits
	outs, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, &answers.File{}))
	var q *QuitError
	if !errors.As(err, &q) || len(outs) != 0 {
		t.Fatalf("%v %+v", err, outs)
	}
	b := g.r.find(protocol.EventStepBefore, "config-dir")
	if len(b) != 1 || b[0].Answer != protocol.AnswerQuit || b[0].Source != protocol.SourceInteractive {
		t.Errorf("%+v", b)
	}
	if a := g.r.find(protocol.EventStepAfter, "config-dir"); len(a) != 1 || a[0].Outcome != protocol.OutQuit {
		t.Errorf("%+v", a)
	}
	if end := g.r.entries[len(g.r.entries)-1]; end.Event != protocol.EventRunEnd || end.Outcome != protocol.RunQuit {
		t.Errorf("%+v", end)
	}
}

func TestAnAppendErrorStopsTheRunBeforeTheFix(t *testing.T) {
	g := newRig()
	c := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	g.r.failOn, g.r.err = protocol.EventStepBefore, errors.New("disk full")
	_, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run)))
	if err == nil || !strings.Contains(err.Error(), "setup protocol") {
		t.Fatalf("%v", err)
	}
	if len(g.h.ran) != 0 {
		t.Fatalf("the fix ran although step.before could not be written: %v", g.h.ran)
	}
}

func TestAnAppendErrorAtRunStartRunsNothing(t *testing.T) {
	g := newRig()
	c := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	g.r.failOn, g.r.err = protocol.EventRunStart, protocol.ErrConflict
	_, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, nil))
	if !errors.Is(err, protocol.ErrConflict) || len(g.h.ran) != 0 {
		t.Fatalf("%v ran %v", err, g.h.ran)
	}
}

func TestAnAppendErrorAfterTheFixStopsTheRun(t *testing.T) {
	g := newRig()
	a := g.check("one", doctor.PhaseUser, cmdFix(false, "a"))
	b := doctor.Check{Name: "two", Phase: doctor.PhaseUser, Fix: cmdFix(false, "b"), Run: func(context.Context) (doctor.Status, string) { return doctor.Fail, "broken" }}
	g.r.failOn, g.r.err = protocol.EventStepAfter, errors.New("disk full")
	g.h.asks = []render.Answer{render.Yes, render.Yes}
	_, _, err := g.run(t, []doctor.Check{a, b}, g.opts(doctor.PhaseUser, nil))
	if err == nil {
		t.Fatal("no error")
	}
	for _, r := range g.h.ran {
		if r == "b" {
			t.Fatal("a later step ran after the protocol failed")
		}
	}
}

func TestUnattendedAsksNothingAndLeavesWhatTheFileDoesNotDecide(t *testing.T) {
	g := newRig()
	decided := g.check("config-dir", doctor.PhaseUser, cmdFix(false, "mkdir", "x"))
	open := doctor.Check{Name: "other", Phase: doctor.PhaseUser, Fix: cmdFix(false, "y"), Run: func(context.Context) (doctor.Status, string) { return doctor.Fail, "broken" }}
	guided := doctor.Check{Name: "guided", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Guide: "do it yourself"}, Run: func(context.Context) (doctor.Status, string) { return doctor.Fail, "broken" }}
	o := g.opts(doctor.PhaseUser, fileFor(decided, answers.Run))
	o.Unattended = true
	outs, _, err := g.run(t, []doctor.Check{decided, open, guided}, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.h.asked) != 0 || len(g.h.fakeHost.asked) != 0 {
		t.Fatalf("something was asked: %v %v", g.h.asked, g.h.fakeHost.asked)
	}
	if strings.Join(g.h.ran, "|") != "mkdir x" {
		t.Errorf("ran %v", g.h.ran)
	}
	if !outs[0].Fixed || outs[0].NeedsHuman || !outs[1].NeedsHuman || !outs[2].NeedsHuman {
		t.Errorf("outs %+v", outs)
	}
	if a := g.r.find(protocol.EventStepAfter, "other"); len(a) != 1 || a[0].Outcome != protocol.OutNeedsHuman {
		t.Errorf("%+v", a)
	}
	if end := g.r.entries[len(g.r.entries)-1]; end.Outcome != protocol.RunNeedsHuman {
		t.Errorf("run.end %+v", end)
	}
}

func TestUnattendedRefusesSudoFromABuilderAndAFixThatWouldAsk(t *testing.T) {
	g := newRig()
	sudoBuild := &doctor.Fix{
		Cmds: []doctor.Cmd{{Argv: []string{"x"}}},
		Build: func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) {
			return []doctor.Cmd{{Sudo: true, Argv: []string{"x"}}}, nil
		},
	}
	c := g.check("thing", doctor.PhaseUser, sudoBuild)
	askBuild := &doctor.Fix{
		Cmds: []doctor.Cmd{{Argv: []string{"y"}}},
		Build: func(_ context.Context, p doctor.Prompter) ([]doctor.Cmd, error) {
			if _, err := p.Secret("a secret?"); err != nil {
				return nil, err
			}
			return []doctor.Cmd{{Argv: []string{"y"}}}, nil
		},
	}
	doAsk := &doctor.Fix{
		Do: func(_ context.Context, p doctor.Prompter) error {
			_, err := p.Confirm("really?")
			return err
		},
		Desc: "asks in the action",
	}
	e := doctor.Check{Name: "do-asks", Phase: doctor.PhaseUser, Fix: doAsk, Run: func(context.Context) (doctor.Status, string) { return doctor.Fail, "broken" }}
	g.h.secrets = []string{"hunter2"} // a prompt that would work if it were asked
	d := doctor.Check{Name: "asks", Phase: doctor.PhaseUser, Fix: askBuild, Run: func(context.Context) (doctor.Status, string) { return doctor.Fail, "broken" }}
	o := g.opts(doctor.PhaseUser, &answers.File{Answers: []answers.Entry{
		{Step: "thing", Fix: answers.FixDigest(c), Answer: answers.Run},
		{Step: "asks", Fix: answers.FixDigest(d), Answer: answers.Run},
		{Step: "do-asks", Fix: answers.FixDigest(e), Answer: answers.Run},
	}})
	o.Unattended = true
	outs, _, err := g.run(t, []doctor.Check{c, d, e}, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.h.ran) != 0 || len(g.h.asked) != 0 {
		t.Fatalf("ran %v asked %v", g.h.ran, g.h.asked)
	}
	if !outs[0].NeedsHuman {
		t.Errorf("sudo from a builder: %+v", outs[0])
	}
	for i, name := range []string{"asks", "do-asks"} {
		if outs[i+1].Fixed || !outs[i+1].NeedsHuman || g.r.find(protocol.EventStepAfter, name)[0].Outcome != protocol.OutNeedsHuman {
			t.Errorf("a fix that asks needs a person unattended (%s): %+v", name, outs[i+1])
		}
	}
}

func TestADryRunWithAnswersSaysWhichQuestionsStayOpen(t *testing.T) {
	g := newRig()
	yes := g.check("yes-step", doctor.PhaseUser, cmdFix(false, "a"))
	skip := g.check("skip-step", doctor.PhaseUser, cmdFix(false, "b"))
	open := g.check("open-step", doctor.PhaseUser, cmdFix(false, "c"))
	sudo := g.check("sudo-step", doctor.PhaseUser, cmdFix(true, "d"))
	f := &answers.File{Answers: []answers.Entry{
		{Step: "yes-step", Fix: answers.FixDigest(yes), Answer: answers.Run},
		{Step: "skip-step", Fix: answers.FixDigest(skip), Answer: answers.Skip},
	}}
	o := g.opts(doctor.PhaseUser, f)
	o.DryRun = true
	_, errOut, err := g.run(t, []doctor.Check{yes, skip, open, sudo}, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"the answers file says run: it would run without asking", "the answers file says skip: it would be skipped", "the question stays open: the answers file has no matching answer", "the question stays open: the step runs a command with sudo"} {
		if !strings.Contains(strings.Join(strings.Fields(errOut), " "), want) {
			t.Errorf("lacks %q\n%s", want, errOut)
		}
	}
	if len(g.h.ran) != 0 || len(g.r.entries) != 0 {
		t.Errorf("a dry run ran %v or logged %d lines", g.h.ran, len(g.r.entries))
	}
	// without answers the text is the old one
	o2 := g.opts(doctor.PhaseUser, nil)
	o2.DryRun = true
	_, errOut, _ = g.run(t, []doctor.Check{open}, o2)
	if strings.Contains(errOut, "stays open") {
		t.Errorf("a plain dry run mentions answers:\n%s", errOut)
	}
}

func TestInteractiveAnswersAreCollectedForSaving(t *testing.T) {
	g := newRig()
	a := g.check("one", doctor.PhaseUser, cmdFix(false, "a"))
	b := doctor.Check{Name: "two", Phase: doctor.PhaseUser, Fix: cmdFix(false, "b"), Run: func(context.Context) (doctor.Status, string) { return doctor.Fail, "broken" }}
	g.h.asks = []render.Answer{render.Yes, render.No}
	outs, _, err := g.run(t, []doctor.Check{a, b}, g.opts(doctor.PhaseUser, nil))
	if err != nil {
		t.Fatal(err)
	}
	if outs[0].Decision != answers.Run || outs[1].Decision != answers.Skip || outs[0].Fix != answers.FixDigest(a) {
		t.Errorf("%+v", outs)
	}
}

func TestACommandThatFailsIsRecordedWithItsExitStatus(t *testing.T) {
	g := newRig()
	c := g.check("one", doctor.PhaseUser, cmdFix(false, "a"))
	g.h.failRun = errors.New("boom")
	g.h.asks = []render.Answer{render.Yes}
	outs, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, nil))
	if err != nil || outs[0].Fixed {
		t.Fatalf("%v %+v", err, outs)
	}
	a := g.r.find(protocol.EventStepAfter, "one")
	if len(a) != 1 || a[0].Outcome != protocol.OutFixFailed || a[0].Ran != protocol.RanDigest([][]string{{"a"}}) {
		t.Errorf("%+v", a)
	}
	if end := g.r.entries[len(g.r.entries)-1]; end.Outcome != protocol.RunLeft {
		t.Errorf("run.end %+v", end)
	}
}

func TestNoLogMeansNothingIsWritten(t *testing.T) {
	g := newRig()
	c := g.check("one", doctor.PhaseUser, cmdFix(false, "a"))
	o := g.opts(doctor.PhaseUser, nil)
	o.Log = nil
	g.h.asks = []render.Answer{render.Yes}
	if _, _, err := g.run(t, []doctor.Check{c}, o); err != nil || len(g.r.entries) != 0 {
		t.Fatalf("%v %d", err, len(g.r.entries))
	}
}

func TestAUserStepWithSudoIsAskedEvenWhenTheFileHoldsItsDigest(t *testing.T) {
	g := newRig()
	c := g.check("needs-root", doctor.PhaseUser, cmdFix(true, "pmset"))
	g.h.asks = []render.Answer{render.No}
	// a hand-made file that names the step with its real digest
	if _, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, fileFor(c, answers.Run))); err != nil {
		t.Fatal(err)
	}
	if len(g.h.asked) != 1 || len(g.h.ran) != 0 {
		t.Fatalf("asked %v ran %v", g.h.asked, g.h.ran)
	}
}

func TestGuidedQuitIsRecordedAsInteractive(t *testing.T) {
	g := newRig()
	c := g.check("guided", doctor.PhaseUser, &doctor.Fix{Guide: "do it yourself", Open: "https://example.com"})
	g.h.asks = []render.Answer{render.Quit}
	_, _, err := g.run(t, []doctor.Check{c}, g.opts(doctor.PhaseUser, nil))
	if !errors.Is(err, render.ErrQuit) {
		t.Fatalf("quit: %v", err)
	}
	b := g.r.find(protocol.EventStepBefore, "guided")
	if len(b) != 2 || b[1].Source != protocol.SourceInteractive || b[1].Answer != protocol.AnswerQuit {
		t.Fatalf("decisions: %+v", b)
	}
	if a := g.r.find(protocol.EventStepAfter, "guided"); len(a) != 1 || a[0].Outcome != protocol.OutQuit {
		t.Fatalf("result: %+v", a)
	}
}

func TestUnattendedMissingPrerequisiteNeedsHuman(t *testing.T) {
	g := newRig()
	c := g.check("kernel", doctor.PhaseUser, cmdFix(false, "install-kernel"))
	c.Needs = "container-system"
	c.Run = func(context.Context) (doctor.Status, string) { return doctor.NotVerified, "system not running" }
	o := g.opts(doctor.PhaseUser, fileFor(c, answers.Run))
	o.Unattended = true
	outs, _, err := g.run(t, []doctor.Check{c}, o)
	if err != nil || len(outs) != 1 || !outs[0].NeedsHuman || len(g.h.ran) != 0 || len(g.h.asked) != 0 {
		t.Fatalf("outs %+v ran %v: %v", outs, g.h.ran, err)
	}
	if e := g.r.find(protocol.EventStepAfter, c.Name); len(e) != 1 || e[0].Outcome != protocol.OutNeedsHuman {
		t.Fatalf("protocol %+v", e)
	}
}
