// Package setup is the first-time setup wizard (design D46, issue #104): the
// steps of the manual's host setup as checks with fixes. A step is one value, a
// doctor check that may carry a fix, so `whr doctor` and `whr setup` cannot
// disagree. The wizard checks, shows the fix, runs it after a confirmation and
// checks again.
//
// Every command and every output format of macOS's tools in the steps is
// unverified until the wizard has set up the reference Mac mini (issue #73).
package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/launchd"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/runlog"
	"github.com/wstein/workharbor/internal/setup/answers"
	"github.com/wstein/workharbor/internal/setup/protocol"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Host is the one interface the wizard acts through: commands, opening a URL,
// confirmation and the prompts. The real one is a terminal; a test passes a fake
// and so never runs sudo or changes a system.
type Host interface {
	doctor.Runner
	doctor.Prompter
	// Run runs a fix command with the terminal attached, so that sudo can ask for
	// a password and an interactive tool can talk to the human.
	Run(ctx context.Context, c doctor.Cmd) error
	// Open opens a URL or a System Settings pane.
	Open(ctx context.Context, target string) error
}

// Asker is what a Host adds to ask [Y/n/q] questions: Enter takes the default
// of the step's class, q quits. A Host without it is asked through Confirm,
// whose answer is only yes or no.
type Asker interface {
	Ask(question string, d render.Default) (render.Answer, error)
}

// Pauser is what a Host adds to page the output: Pause says "Press Enter to
// continue (q to quit)" and waits. It returns render.ErrQuit for q.
type Pauser interface {
	Pause() error
}

// ContextPauser is a Pauser that also stops waiting when the context ends
// (Ctrl-C at the prompt), instead of waiting for Enter.
type ContextPauser interface {
	PauseContext(ctx context.Context) error
}

func pause(ctx context.Context, p Pauser) error {
	if c, ok := p.(ContextPauser); ok {
		return c.PauseContext(ctx)
	}
	return p.Pause()
}

// Ask asks through the Host's Asker, or through Confirm when it has none.
func Ask(h Host, question string, d render.Default) (render.Answer, error) {
	if a, ok := h.(Asker); ok {
		return a.Ask(question, d)
	}
	ok, err := h.Confirm(question)
	if err != nil || !ok {
		return render.No, err
	}
	return render.Yes, nil
}

// QuitError is returned by Run when the person answered q. It says where to go
// on; the command maps it to its own exit code, which is not a failure's.
type QuitError struct {
	Step   string
	Resume string // the command that goes on, for the hint
}

func (e *QuitError) Error() string {
	return fmt.Sprintf("stopped at step %s; to go on, run: %s", e.Step, e.Resume)
}

// Is makes errors.Is(err, render.ErrQuit) true.
func (e *QuitError) Is(target error) bool { return target == render.ErrQuit }

// InterruptedError is returned by Run when Ctrl-C, SIGTERM or a deadline stopped
// it. It wraps the cause (a context error) and says where to go on.
type InterruptedError struct {
	Step   string
	When   InterruptedWhen // where in Step the run was stopped
	Resume string          // the command that goes on, for the hint
	Err    error
}

// InterruptedWhen says whether the step an InterruptedError names had started.
type InterruptedWhen int

const (
	// DuringStep: the step had started (its check or its fix was cut short).
	DuringStep InterruptedWhen = iota
	// BeforeStep: the step had not started; it is the next one.
	BeforeStep
	// AfterLastStep: every step had finished; Step is the last one.
	AfterLastStep
)

func (e *InterruptedError) Error() string {
	switch e.When {
	case BeforeStep:
		return fmt.Sprintf("interrupted before step %s; to go on, run: %s", e.Step, e.Resume)
	case AfterLastStep:
		return fmt.Sprintf("interrupted after step %s; to check it again, run: %s", e.Step, e.Resume)
	}
	return fmt.Sprintf("interrupted in step %s; to check it and go on, run: %s", e.Step, e.Resume)
}

func (e *InterruptedError) Unwrap() error { return e.Err }

// Level is the report level of a status.
func Level(st doctor.Status) render.Level {
	switch st {
	case doctor.OK:
		return render.LevelOK
	case doctor.Fail:
		return render.LevelFail
	case doctor.Warn:
		return render.LevelWarn
	case doctor.Skipped:
		return render.LevelSkipped
	}
	return render.LevelNotVerified
}

// Options say what to run.
type Options struct {
	Phase  doctor.Phase
	DryRun bool
	Only   []string // run only these steps, optional ones too
	From   string   // start at this step
	// Resume is the command that started the run, up to its flags, as words
	// ("whr", "setup", "host", "--dev", "--user", "u"), without --dry-run,
	// --only and --from. The summary's next command continues from it.
	Resume []string
	// Out gets the data (one line per step), Err the human text.
	Out, Err io.Writer
	// Style says how Err is drawn (colour and symbols only on a terminal); the
	// zero value is plain ASCII. Verbose adds the raw text of the tools.
	Style   render.Style
	Verbose bool
	// Paged stops after the legend page and after every step page for Enter,
	// when the Host is a Pauser. Off for runs that must not block (no terminal,
	// unattended, dry run).
	Paged bool

	// Answers is the answer file of a user-phase run (issue #337); nil asks
	// every step. A host-phase run never uses one, whatever it is given: the
	// file answers only the outer "Ready to run this?" question of an eligible
	// step (answers.Eligible), never a prompt a fix asks. AnswersDigest is the
	// digest of the file's bytes (protocol.AnswersDigest).
	Answers       *answers.File
	AnswersDigest string
	// Unattended asks nothing: a step the file does not answer is left for a
	// person (Outcome.NeedsHuman), and a fix that would ask a value fails. The
	// command refuses it for the host phase.
	Unattended bool
	// Log gets the setup protocol of the run; nil writes none. A failure to
	// write it stops the run (a dry run writes none). Account is the account
	// running it and Home its home directory, shown as ~ in the logged flags.
	Log Recorder
	// Yes (--yes) answers the questions of undoable steps with yes; the
	// others are still asked.
	Yes bool
	// RunLog is the text log of the run (issue #379): one line per step, and on
	// a failure its cause, next action and the tail of the step's output.
	RunLog  *runlog.Log
	Account string
	Home    string
}

// Recorder is where the setup protocol is written: *protocol.Log.
type Recorder interface {
	Append(e protocol.Entry) error
}

// Outcome is what became of one step.
type Outcome struct {
	Step   string
	Status doctor.Status
	Detail string
	Fixed  bool // a fix ran and the check passed afterwards
	Asked  bool // a fix was offered and declined or left undone
	// UseUser is the account the step says to use instead of the requested one
	// (the legacy account): the next command names it.
	UseUser string
	// Todo is what the person has to do for a step that is left.
	Todo render.TodoItem
	// NeedsHuman: an unattended run left the step for a person to do.
	NeedsHuman bool
	// Decision is the answer (answers.Run or answers.Skip) the run took for the
	// step's outer prompt, from the person or from the answers file, and Fix the
	// digest of its fix: what --save-answers saves. Both are empty when the step
	// was not decided that way, or was asked again.
	Decision, Fix string
}

// Select returns the steps to run: those of the phase, from --from on, or only
// the ones named by --only. An unknown name is an error that lists the known
// ones. Optional steps run only when named.
func Select(steps []doctor.Check, o Options) ([]doctor.Check, error) {
	var phase []doctor.Check
	known := make([]string, 0, len(steps))
	for _, s := range steps {
		if s.Phase == o.Phase {
			phase = append(phase, s)
			known = append(known, s.Name)
		}
	}
	has := func(n string) bool {
		for _, k := range known {
			if k == n {
				return true
			}
		}
		return false
	}
	for _, n := range append(append([]string(nil), o.Only...), o.From) {
		if n != "" && !has(n) {
			return nil, fmt.Errorf("no step %q in this phase; the steps are %s", n, strings.Join(known, ", "))
		}
	}
	var out []doctor.Check
	started := o.From == ""
	for _, s := range phase {
		if s.Name == o.From {
			started = true
		}
		if !started {
			continue
		}
		if len(o.Only) > 0 {
			if !contains(o.Only, s.Name) {
				continue
			}
		} else if s.Optional {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Run runs the selected steps in order. A step whose check passes does nothing.
// With DryRun the checks still run, for real, and the fixes are only printed.
// Each step prints its header, one report line (the reason once), and for a fix
// the ACTION and the commands; the raw text of a tool only with Verbose. When
// the person answers q, Run returns what it did so far and a *QuitError. With
// o.Log it writes the setup protocol: run.start, per step step.before (before
// its fix runs) and step.after, run.end; a failed write stops the run.
func Run(ctx context.Context, steps []doctor.Check, h Host, o Options) ([]Outcome, error) {
	chosen, err := Select(steps, o)
	if err != nil {
		return nil, err
	}
	if o.Phase == doctor.PhaseHost {
		o.Answers, o.AnswersDigest = nil, "" // the host phase is never answered from a file
	}
	if o.DryRun {
		o.Log = nil // nothing is changed, so nothing is recorded
	}
	ui := render.Writer{W: o.Err, S: o.Style}
	if len(chosen) > 0 {
		ui.Legend()
	}
	if len(chosen) > 0 {
		var found, missing []string
		for _, t := range []string{"sudo", "brew", "container"} {
			if !toolFound(t) {
				missing = append(missing, t)
			} else {
				found = append(found, t)
			}
		}
		will := fmt.Sprintf("check %d steps and change nothing without a question (--yes answers the undoable ones).", len(chosen))
		if o.DryRun {
			will = fmt.Sprintf("check %d steps and change nothing (dry run).", len(chosen))
		}
		state := "a new run"
		if o.Resume != nil && o.From != "" {
			state = "an earlier run, go on at " + o.From
		}
		ui.Put(render.Preflight(o.Style, found, missing, state, will))
	}
	rc := &recorder{log: o.Log, account: o.Account, phase: o.Phase}
	start := protocol.Entry{Event: protocol.EventRunStart, Source: protocol.SourceInteractive, Flags: protocol.Flags(o.Resume, o.Home)}
	if o.Answers != nil {
		start.Source, start.Answers = protocol.SourceAnswers, o.AnswersDigest
	}
	if err := rc.add(start); err != nil {
		return nil, err
	}
	if rs, ok := h.(InterruptSeen); ok {
		rs.ResetInterrupts()
	}
	rn := &runner{h: h, p: h, o: o, ui: ui, rc: rc}
	if o.Unattended {
		rn.p = noPrompt{h}
	}
	var outs []Outcome
	provided := map[string]bool{} // services a step has brought up or found running
	// interrupted ends the run at chosen[i] with the cause, which is a context error
	interrupted := func(i int, when InterruptedWhen, cause error) ([]Outcome, error) {
		if len(chosen) == 0 {
			return rc.finish(outs, protocol.RunInterrupted, cause)
		}
		i = min(i, len(chosen)-1)
		return rc.finish(outs, protocol.RunInterrupted, &InterruptedError{Step: chosen[i].Name, When: when, Resume: nextCommand(o, chosen[i].Name, names(chosen[i:])), Err: cause})
	}
	for i, s := range chosen {
		if err := Interrupted(ctx, h); err != nil {
			return interrupted(i, BeforeStep, err)
		}
		title := s.Title
		if title == "" {
			title = s.Name
		}
		if p, ok := h.(Pauser); ok && o.Paged {
			if err := pause(ctx, p); errors.Is(err, render.ErrQuit) {
				if _, e := rc.finish(nil, protocol.RunQuit, nil); e != nil {
					return outs, e
				}
				return outs, &QuitError{Step: s.Name, Resume: nextCommand(o, s.Name, names(chosen[i:]))}
			}
			if err := Interrupted(ctx, h); err != nil { // Ctrl-C at the prompt: stop now, not after the step
				return interrupted(i, BeforeStep, err)
			}
		}
		ui.Header(i+1, len(chosen), title)
		st, detail := s.Run(ctx)
		if err := Interrupted(ctx, h); err != nil && st != doctor.OK { // the check was cut short: its answer means nothing, and no step after it starts
			outs = append(outs, Outcome{Step: s.Name, Status: st, Detail: detail})
			if e := rc.add(protocol.Entry{Event: protocol.EventStepAfter, Step: s.Name, Outcome: protocol.OutInterrupted, Status: string(st)}); e != nil {
				return rc.finish(outs, protocol.RunError, e)
			}
			return interrupted(i, DuringStep, err)
		}
		dataLine(o, st, s.Name, detail)
		report(ui, o, st, detail)
		out := Outcome{Step: s.Name, Status: st, Detail: detail}
		if s.UseUser != nil {
			out.UseUser = s.UseUser(st)
		}
		after := func(outcome string, exit *int, ran [][]string) error {
			e := protocol.Entry{Event: protocol.EventStepAfter, Step: s.Name, Outcome: outcome, Status: string(out.Status), Exit: exit}
			if ran != nil {
				e.Ran = protocol.RanDigest(ran)
			}
			return rc.add(e)
		}
		stop := func(err error) ([]Outcome, error) { return rc.finish(outs, protocol.RunError, err) }
		if st == doctor.Warn && !s.FixOnWarn || st == doctor.Skipped {
			outs = append(outs, out)
			outcome := protocol.OutWarnAccepted
			if st == doctor.Skipped {
				outcome = protocol.OutSkipped
			}
			if err := after(outcome, nil, nil); err != nil {
				return stop(err)
			}
			continue
		}
		if st == doctor.OK {
			if s.Provides != "" {
				provided[s.Provides] = true
			}
			outs = append(outs, out)
			if err := after(protocol.OutAlreadyDone, nil, nil); err != nil {
				return stop(err)
			}
			continue
		}
		out.Todo = todoFor(title, s.Fix)
		if s.Fix == nil {
			ui.Report(render.LevelNotVerified, "whr has no fix for this step")
			outs = append(outs, out)
			if err := after(protocol.OutNoFix, nil, nil); err != nil {
				return stop(err)
			}
			continue
		}
		showFix(ui, s.Fix)
		needsMissing := s.Needs != "" && !provided[s.Needs] && !o.DryRun && !providedElsewhere(ctx, steps, chosen, s.Needs)
		if needsMissing {
			if err := Interrupted(ctx, h); err != nil { // the check was cut short: "not provided" means nothing, so resume must run this step
				outs = append(outs, out)
				if e := rc.add(protocol.Entry{Event: protocol.EventStepAfter, Step: s.Name, Outcome: protocol.OutInterrupted, Status: string(out.Status)}); e != nil {
					return rc.finish(outs, protocol.RunError, e)
				}
				return interrupted(i, DuringStep, err)
			}
			ui.Report(render.LevelSkipped, fmt.Sprintf("not run: it needs %s, which no step before it brought up", s.Needs))
			out.Asked = true
			out.NeedsHuman = o.Unattended
			outs = append(outs, out)
			outcome := protocol.OutNotRun
			if o.Unattended {
				outcome = protocol.OutNeedsHuman
			}
			if err := after(outcome, nil, nil); err != nil {
				return stop(err)
			}
			continue
		}
		if o.DryRun {
			if f := s.Fix; f.Guide != "" && hasCommands(f) {
				ui.Action("what happens next: " + oneLine(f.Guide))
			}
			ui.Report(render.LevelSkipped, "dry run: nothing is run"+rn.dryRunQuestion(s, out))
			out.Asked = true
			outs = append(outs, out)
			continue
		}
		o.RunLog.ResetOutput() // a failure tail is this step's output only
		res, err := rn.apply(ctx, s, &out)
		var fatal fatalError
		if errors.As(err, &fatal) {
			return stop(fatal.err)
		}
		if errors.Is(err, render.ErrQuit) {
			if e := after(protocol.OutQuit, res.exit, res.ran); e != nil {
				return stop(e)
			}
			if _, e := rc.finish(nil, protocol.RunQuit, nil); e != nil {
				return outs, e
			}
			return outs, &QuitError{Step: s.Name, Resume: nextCommand(o, s.Name, names(chosen[i:]))}
		}
		if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			ui.Report(render.LevelSkipped, "interrupted: nothing more runs")
		} else if err != nil {
			reason, tool := render.SplitTool(oneLine(err.Error()))
			ui.Report(render.LevelFail, reason)
			if o.Verbose && tool != "" {
				ui.Tool(tool)
			}
			o.RunLog.Step(s.Name, "fail", reason)
			FailureSummary(ui, o.RunLog, reason, "fix the cause, then run: "+nextCommand(o, s.Name, names(chosen[i:])))
		}
		out.Asked = !res.fixed
		out.NeedsHuman = res.outcome == protocol.OutNeedsHuman
		if res.decision != "" {
			out.Decision, out.Fix = res.decision, answers.FixDigest(s)
		}
		outcome := res.outcome
		if res.fixed {
			st, detail = s.Run(ctx)
			out.Status, out.Detail = st, detail
			out.Fixed = st == doctor.OK
			outcome = protocol.OutNotFixed
			if out.Fixed {
				outcome = protocol.OutFixed
			}
			if out.Fixed && s.Provides != "" {
				provided[s.Provides] = true
			}
			dataLine(o, st, s.Name, detail)
			report(ui, o, st, detail)
			if st == doctor.Fail {
				FailureSummary(ui, o.RunLog, oneLine(detail), "fix the cause, then run: "+nextCommand(o, s.Name, names(chosen[i:])))
			}
		}
		if ctxErr := Interrupted(ctx, h); ctxErr != nil && !out.Fixed { // also during the re-check, which has no error to return; a step that fixed itself stays fixed
			outcome = protocol.OutInterrupted
			if err == nil {
				err = ctxErr
			} else if !errors.Is(err, ctxErr) {
				err = fmt.Errorf("%w: %w", ctxErr, err)
			}
		}
		outs = append(outs, out)
		var ran [][]string
		if res.fixed || outcome == protocol.OutFixFailed || outcome == protocol.OutInterrupted {
			ran = res.ran
			if ran == nil {
				ran = [][]string{}
			}
		}
		if err := after(outcome, res.exit, ran); err != nil {
			return stop(err)
		}
		if outcome == protocol.OutInterrupted { // Ctrl-C, SIGTERM or a deadline: no step after it starts
			return interrupted(i, DuringStep, err)
		}
	}
	if err := Interrupted(ctx, h); err != nil { // cut short after the last step finished
		return interrupted(len(chosen)-1, AfterLastStep, err)
	}
	end := protocol.RunDone
	for _, out := range outs {
		switch {
		case out.NeedsHuman:
			end = protocol.RunNeedsHuman
		case (out.Status == doctor.Fail || out.Status == doctor.NotVerified) && end == protocol.RunDone:
			end = protocol.RunLeft
		}
	}
	return rc.finish(outs, end, nil)
}

// recorder writes the setup protocol; with no log it does nothing.
type recorder struct {
	log     Recorder
	account string
	phase   doctor.Phase
}

func (r *recorder) add(e protocol.Entry) error {
	if r.log == nil {
		return nil
	}
	e.Account, e.Cmd, e.Phase = r.account, protocol.CmdSetup, string(r.phase)
	if err := r.log.Append(e); err != nil {
		return fatalError{fmt.Errorf("setup protocol: %w", err)}
	}
	return nil
}

// finish writes run.end and returns outs with err, or with the failure to write
// it when there is no other error. A log that is already broken is not written.
func (r *recorder) finish(outs []Outcome, outcome string, err error) ([]Outcome, error) {
	if r.log == nil || errors.Is(err, protocol.ErrBroken) {
		return outs, unwrapFatal(err)
	}
	if e := r.add(protocol.Entry{Event: protocol.EventRunEnd, Outcome: outcome}); e != nil && err == nil {
		err = e
	}
	return outs, unwrapFatal(err)
}

// fatalError is a failure of the protocol: the run stops, a step does not fail.
type fatalError struct{ err error }

func (e fatalError) Error() string { return e.err.Error() }
func (e fatalError) Unwrap() error { return e.err }

func unwrapFatal(err error) error {
	var f fatalError
	if errors.As(err, &f) {
		return f.err
	}
	return err
}

// ErrUnattended is what a prompt answers in an unattended run.
var ErrUnattended = errors.New("this needs a person, and --unattended asks nothing")

// noPrompt is the Prompter of an unattended run: every question fails.
type noPrompt struct{ Host }

func (noPrompt) Line(string) (string, error)   { return "", ErrUnattended }
func (noPrompt) Secret(string) (string, error) { return "", ErrUnattended }
func (noPrompt) Confirm(string) (bool, error)  { return false, ErrUnattended }

// runner is what applying a fix needs of one run.
// lookPath finds a tool on PATH; a test replaces it.
var lookPath = exec.LookPath

// managedBrew is where whr's own steps install and call Homebrew.
const managedBrew = "/opt/homebrew/bin/brew"

// toolFound says whether a tool is there. Homebrew is looked up at its managed
// prefix first: a brew elsewhere on PATH is not the one the steps use.
func toolFound(name string) bool {
	if name == "brew" {
		if _, err := lookPath(managedBrew); err == nil {
			return true
		}
	}
	_, err := lookPath(name)
	return err == nil
}

// ask asks through the Host, or answers yes for an undoable step under --yes.
func (r *runner) ask(question string, d render.Default) (render.Answer, error) {
	if a, ok := render.AutoYes(r.ui, question, d, r.o.Yes); ok {
		return a, nil
	}
	return Ask(r.h, question, d)
}

type runner struct {
	h         Host
	p         doctor.Prompter // what a fix's Build and Do ask through
	o         Options
	ui        render.Writer
	rc        *recorder
	sudoReady bool
}

// dryRunQuestion says, in a dry run with answers or --unattended, what would
// become of the step's question.
func (r *runner) dryRunQuestion(s doctor.Check, out Outcome) string {
	if r.o.Answers == nil && !r.o.Unattended {
		return ""
	}
	if r.o.Answers != nil && r.o.Phase == doctor.PhaseUser && out.UseUser == "" && hasCommands(s.Fix) {
		if a, ok := r.o.Answers.Lookup(s); ok {
			if a == answers.Skip {
				return "; the answers file says skip: it would be skipped"
			}
			return "; the answers file says run: it would run without asking (commands that a builder returns and that use sudo are asked first)"
		}
	}
	why := "the answers file has no matching answer (a new or changed command)"
	if ok, reason := answers.Eligible(s); !ok {
		why = reason
	} else if r.o.Answers == nil {
		why = "no answers file"
	}
	if r.o.Unattended {
		return "; the question stays open and --unattended leaves the step for you: " + why
	}
	return "; the question stays open: " + why
}

// dataLine writes the one data line of a step. The raw text a tool printed
// (exit status and message) stays out of it unless Verbose: the human line
// states the same finding, so it is not printed twice.
func dataLine(o Options, st doctor.Status, name, detail string) {
	detail = oneLine(detail)
	if !o.Verbose {
		detail, _ = render.SplitTool(detail)
	}
	o.RunLog.Step(name, StepStatus(st), detail)
	fmt.Fprintf(o.Out, "%s\t%s\t%s\n", st, name, detail)
}

// report prints a step's result once: its reason, and the raw text of the tool
// behind it only with --verbose.
func report(ui render.Writer, o Options, st doctor.Status, detail string) {
	reason, tool := render.SplitTool(oneLine(detail))
	ui.Report(Level(st), reason)
	if o.Verbose && tool != "" {
		ui.Tool(tool)
	}
}

func hasCommands(f *doctor.Fix) bool {
	return f.Do != nil || f.Build != nil || len(f.Cmds) > 0
}

// showFix prints what a fix does as ACTION lines and the exact commands as
// copyable ones. A guided fix (no command) shows its guide as the ACTION.
func showFix(ui render.Writer, f *doctor.Fix) {
	switch {
	case !hasCommands(f):
		ui.Action(oneLine(f.Guide))
		if f.Open != "" {
			ui.Action("this opens:\n" + f.Open)
		}
	case f.Desc != "":
		ui.Action(f.Desc)
	case len(f.Cmds) == 1:
		ui.Action("run this command")
	default:
		ui.Action("run these commands")
	}
	for _, c := range f.Cmds {
		ui.Command(QuoteArgv(c.Full()))
	}
	if hasCommands(f) && f.Open != "" {
		ui.Action("this opens:\n" + f.Open)
	}
}

// todoFor is what the person has to do for a step that is left.
func todoFor(title string, f *doctor.Fix) render.TodoItem {
	it := render.TodoItem{Text: title}
	switch {
	case f == nil:
		it.Text += ": whr has no fix for this step; see the manual"
	case !hasCommands(f):
		it.Text += ": " + oneLine(f.Guide)
	default:
		if f.Desc != "" {
			it.Text += ": " + f.Desc
		}
		for _, c := range f.Cmds {
			it.Commands = append(it.Commands, QuoteArgv(c.Full()))
		}
		it.After = oneLine(f.Guide) // after the commands: the person acts first
	}
	return it
}

// providedElsewhere reports whether a step that the run did not select provides
// the service and its check passes: `--only container-kernel` runs against a
// system that already runs.
func providedElsewhere(ctx context.Context, steps, chosen []doctor.Check, service string) bool {
	for _, c := range steps {
		if c.Provides != service || contains(names(chosen), c.Name) {
			continue
		}
		if st, _ := c.Run(ctx); st == doctor.OK {
			return true
		}
	}
	return false
}

func names(cs []doctor.Check) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

// Summary writes what a run did, for the human: a one-line count, the steps
// that are done, the ones that are left with why, the command that goes on, and
// a numbered list of what to do now. A step that is optional or was left alone
// by a warn counts as done only when it passed. It writes nothing for an empty
// run.
func Summary(w io.Writer, outs []Outcome, o Options) {
	var done, left, leftNames []string
	var todo []render.TodoItem
	var counts render.Counts
	first, useUser := "", ""
	for _, out := range outs {
		switch out.Status {
		case doctor.OK:
			counts.OK++
			done = append(done, out.Step)
		case doctor.Warn:
			counts.Warn++
		case doctor.Skipped:
			counts.Skipped++
		default:
			if out.Status == doctor.Fail {
				counts.Fail++
			} else {
				counts.NotVerified++
			}
			left = append(left, out.Step+" ("+string(out.Status)+")")
			leftNames = append(leftNames, out.Step)
			if out.Todo.Text != "" {
				todo = append(todo, out.Todo)
			}
			if first == "" {
				first, useUser = out.Step, out.UseUser
			}
		}
	}
	if len(outs) == 0 {
		return
	}
	ui := render.Writer{W: w, S: o.Style}
	ui.Rule()
	line := fmt.Sprintf("Summary: %d ok, %d need action, %d not verified", counts.OK, counts.Fail, counts.NotVerified)
	if counts.Warn > 0 {
		line += fmt.Sprintf(", %d weaker than recommended", counts.Warn)
	}
	if o.DryRun {
		line += " (dry run: nothing was changed)"
	}
	fmt.Fprintln(w, line)
	if o.Verbose { // the step names and raw statuses are internal detail
		fmt.Fprintf(w, "  done: %s\n", wrapList(listOrNone(done), 8))
		fmt.Fprintf(w, "  left: %s\n", wrapList(listOrNone(left), 8))
	}
	if first != "" {
		next := ""
		if useUser != "" {
			argv := append([]string(nil), o.Resume...)
			if len(argv) == 0 {
				argv = []string{"whr", "setup"}
			}
			next = QuoteArgv(append(argv, "--user", useUser))
		} else {
			next = nextCommand(o, first, leftNames)
		}
		fmt.Fprintf(w, "  next: %s\n", next)
		todo = append(todo, render.TodoItem{Text: "Then go on with the steps that are left", Commands: []string{next}})
	}
	for _, out := range outs {
		if out.Step == "container-kernel" && out.Status != doctor.OK {
			fmt.Fprintln(w, "  no Linux kernel is installed or verified: containers cannot boot until the container-kernel step passes")
		}
	}
	ui.Todo(todo)
}

// wrapList wraps a comma-separated list at 80 columns, continuation lines
// indented by pad.
func wrapList(list string, pad int) string {
	return strings.ReplaceAll(render.Wrap(list, pad), "\n", "\n"+strings.Repeat(" ", pad))
}

// nextCommand is the command that goes on: the phase and the flags of this run
// (so --dev, --user and --prefix survive), then --from the first step left, or,
// when the run was limited by --only, --only the steps left, because --from
// would also run steps nobody selected.
func nextCommand(o Options, first string, left []string) string {
	argv := append([]string(nil), o.Resume...)
	if len(argv) == 0 {
		argv = []string{"whr", "setup"}
	}
	if len(o.Only) > 0 {
		for _, n := range left {
			argv = append(argv, "--only", n)
		}
	} else {
		argv = append(argv, "--from", first)
	}
	return QuoteArgv(argv)
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

// QuoteArgv writes an argument vector so a human can read where each argument
// begins; it is for display only. A control, bidirectional or separator
// character is never printed raw (a newline would start a second command when
// the line is pasted, an escape sequence would reach the terminal): the
// argument is quoted with the visible escape textsafe.Escape gives it.
func QuoteArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if e := textsafe.Escape(a); e != a {
			parts[i] = "'" + strings.ReplaceAll(e, "'", `'\''`) + "'"
		} else if a == "" || strings.ContainsAny(a, " \t\"'$`\\<>|&;*?") {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// applied is what apply did.
type applied struct {
	fixed   bool       // a fix ran, so the check is worth running again
	outcome string     // the protocol outcome when it did not (declined, needs_human, fix_failed ...)
	exit    *int       // the exit status of the last command, when one ran
	ran     [][]string // the argument vectors that ran, sudo included
	// decision is the answer of the outer prompt that --save-answers may keep:
	// from the person or the file, and not one the wizard had to ask again.
	decision string
}

// apply decides, then runs the fix. It returns what it did and what stopped it:
// render.ErrQuit when the person answered q, a fatalError when the protocol
// could not be written, any other error for a fix that failed.
//
// The answers file replaces only the outer "Ready to run this?" prompt of an
// eligible user-phase step whose check just ran (Lookup is called here, after
// s.Run and before any other Run); the prompts a fix asks and "Open it now?"
// stay interactive. A decision from the file is not taken for a step that names
// another account to use, and not for a sudo command a builder returns: those
// are asked.
func (r *runner) apply(ctx context.Context, s doctor.Check, out *Outcome) (res applied, err error) {
	f, ui, o := s.Fix, r.ui, r.o
	fix := answers.FixDigest(s)
	before := func(answer, source string) error {
		e := protocol.Entry{Event: protocol.EventStepBefore, Step: s.Name, Fix: fix, Answer: answer, Source: source, Status: string(out.Status)}
		if source == protocol.SourceAnswers {
			e.Answers = o.AnswersDigest
		}
		return r.rc.add(e)
	}
	defer func() {
		if err != nil && res.outcome == "" && !errors.Is(err, render.ErrQuit) {
			res.outcome = protocol.OutFixFailed
			if errors.Is(err, ErrUnattended) {
				res.outcome = protocol.OutNeedsHuman
			}
			if Interrupted(ctx, r.h) != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				res.outcome = protocol.OutInterrupted
			}
		}
	}()
	if err := Interrupted(ctx, r.h); err != nil { // Ctrl-C came before the question: ask nothing, run nothing
		res.outcome = protocol.OutInterrupted
		if e := before(protocol.AnswerNone, protocol.SourceNone); e != nil {
			return res, e
		}
		return res, err
	}
	// decide records an answer of the outer prompt and says whether to go on.
	decide := func(a render.Answer, source string) (bool, error) {
		switch a {
		case render.Quit:
			if err := before(protocol.AnswerQuit, source); err != nil {
				return false, err
			}
			return false, render.ErrQuit
		case render.No:
			res.outcome = protocol.OutDeclined
			return false, before(protocol.AnswerSkip, source)
		}
		return true, before(protocol.AnswerRun, source)
	}
	if !hasCommands(f) { // guided: for what a command line cannot do
		if err := before(protocol.AnswerNone, protocol.SourceNone); err != nil {
			return res, err
		}
		if o.Unattended {
			res.outcome = protocol.OutNeedsHuman
			ui.Report(render.LevelSkipped, "left for you: --unattended asks nothing")
			return res, nil
		}
		// manual work is never answered by --yes: only the person knows it is done
		yes := func(question string, manual bool) (bool, error) {
			ask := r.ask
			if manual {
				ask = func(q string, d render.Default) (render.Answer, error) { return Ask(r.h, q, d) }
			}
			a, err := ask(question, render.DefaultYes)
			if err != nil {
				return false, err
			}
			if a == render.Quit {
				if err := before(protocol.AnswerQuit, protocol.SourceInteractive); err != nil {
					return false, err
				}
				return false, render.ErrQuit
			}
			return a == render.Yes, nil
		}
		if f.Open != "" {
			if ok, err := yes("Open it now?", false); err != nil {
				return res, err
			} else if ok {
				if err := r.h.Open(ctx, f.Open); err != nil {
					ui.Report(render.LevelFail, "could not open it: "+oneLine(err.Error()))
				}
			}
		}
		done, err := yes("Done with this step? The check runs again.", true)
		res.fixed = done && err == nil
		if !res.fixed && err == nil {
			res.outcome = protocol.OutDeclined
		}
		return res, err
	}
	d := render.DefaultYes
	if f.Irreversible { // it cannot be undone: Enter is no
		d = render.DefaultNo
	}
	source := protocol.SourceInteractive
	var ans string
	if o.Phase == doctor.PhaseUser && o.Answers != nil && out.UseUser == "" {
		if a, ok := o.Answers.Lookup(s); ok {
			ans, source = a, protocol.SourceAnswers
		}
	}
	switch {
	case source == protocol.SourceAnswers:
		if ans == answers.Skip {
			res.outcome, res.decision = protocol.OutDeclined, ans
			return res, before(protocol.AnswerSkip, source)
		}
		res.decision = ans
		if err := before(protocol.AnswerRun, source); err != nil {
			return res, err
		}
	case o.Unattended:
		res.outcome = protocol.OutNeedsHuman
		ui.Report(render.LevelSkipped, "left for you: no answer in the file, and --unattended asks nothing")
		return res, before(protocol.AnswerNone, protocol.SourceNone)
	default:
		a, err := r.ask("Ready to run this?", d)
		if err != nil {
			res.outcome = protocol.OutNotRun
			if e := before(protocol.AnswerNone, protocol.SourceNone); e != nil {
				return res, e
			}
			return res, err
		}
		switch a {
		case render.Yes:
			res.decision = answers.Run
		case render.No:
			res.decision = answers.Skip
		}
		if ok, err := decide(a, source); err != nil || !ok {
			return res, err
		}
	}
	if f.Guide != "" {
		ui.Action("what happens next: " + oneLine(f.Guide))
	}
	if usesSudo(f) && source != protocol.SourceAnswers {
		if err := r.primeSudo(ctx); err != nil {
			return res, err
		}
	}
	if err := Interrupted(ctx, r.h); err != nil { // Ctrl-C came during the question: Do and Build never start
		return res, err
	}
	if f.Do != nil {
		if err := f.Do(ctx, r.p); err != nil {
			return res, err
		}
	}
	cmds := f.Cmds
	if f.Build != nil {
		var err error
		if cmds, err = f.Build(ctx, r.p); err != nil {
			return res, err
		}
		for _, c := range cmds { // the real commands, shown before they run
			ui.Command(QuoteArgv(c.Full()))
		}
		if source == protocol.SourceAnswers && anySudo(cmds) {
			// the digest does not cover what a builder returns: a file never
			// decides sudo, so ask again
			res.decision = ""
			if o.Unattended {
				res.outcome = protocol.OutNeedsHuman
				ui.Report(render.LevelSkipped, "left for you: the commands use sudo, which no file decides, and --unattended asks nothing")
				return res, nil
			}
			ui.Report(render.LevelSkipped, "the answers file does not decide commands that use sudo: you are asked")
			a, err := r.ask("Ready to run these commands?", d)
			if err != nil {
				return res, err
			}
			if ok, err := decide(a, protocol.SourceInteractive); err != nil || !ok {
				return res, err
			}
		}
	}
	if f.Build != nil && anySudo(cmds) {
		// the commands a builder returns were not known before: validate sudo now
		if err := r.primeSudo(ctx); err != nil {
			return res, err
		}
	}
	for _, c := range cmds {
		res.ran = append(res.ran, append([]string(nil), c.Full()...))
		if err := r.h.Run(ctx, c); err != nil {
			res.exit = exitStatus(err)
			return res, fmt.Errorf("%s failed: %w", QuoteArgv(c.Full()), err)
		}
		zero := 0
		res.exit = &zero
	}
	res.fixed = true
	if f.Guide != "" && f.Open != "" && !o.Unattended {
		a, err := r.ask("Open the page that helps with the rest?", render.DefaultYes)
		if err != nil {
			return res, err
		}
		if a == render.Quit {
			if err := before(protocol.AnswerQuit, protocol.SourceInteractive); err != nil {
				return res, err
			}
			return res, render.ErrQuit
		}
		if a == render.Yes {
			_ = r.h.Open(ctx, f.Open)
		}
	}
	return res, nil
}

// interruptGrace is how long a failed sudo -v waits to see whether an interrupt
// is the reason.
var interruptGrace = 300 * time.Millisecond

// primeSudo runs one sudo -v in the host phase, no background refresh: root
// stays reachable only while the human is here, and sudo asks again if it
// expires.
func (r *runner) primeSudo(ctx context.Context) error {
	if r.o.Phase != doctor.PhaseHost || r.sudoReady {
		return nil
	}
	if err := Interrupted(ctx, r.h); err != nil { // stopped already: no password prompt
		return err
	}
	r.ui.Action("sudo asks for your password once, so the commands above need it only once (no background refresh)")
	r.ui.Command("sudo -v")
	if err := r.h.Run(ctx, doctor.Cmd{Sudo: true, Argv: []string{"-v"}}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// sudo catches Ctrl-C and exits 1, often before whr's own signal handling
		// has cancelled the context: give it a moment before calling this a refusal
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interruptGrace):
		}
		if e := Interrupted(ctx, r.h); e != nil {
			return e
		}
		return fmt.Errorf("sudo did not accept the password: %w", err)
	}
	r.sudoReady = true
	return nil
}

func anySudo(cmds []doctor.Cmd) bool {
	for _, c := range cmds {
		if c.Sudo {
			return true
		}
	}
	return false
}

// exitStatus is the exit status of a command that failed, nil when the error
// carries none (it did not start, or a signal ended it).
func exitStatus(err error) *int {
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		n := ee.ExitCode()
		return &n
	}
	return nil
}

func usesSudo(f *doctor.Fix) bool {
	for _, c := range f.Cmds {
		if c.Sudo {
			return true
		}
	}
	return false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Errors of the guards.
var (
	ErrRoot         = errors.New("setup never runs as root: it runs each privileged command through sudo, one at a time, after showing it")
	ErrWrongUser    = errors.New("this part runs as another user")
	ErrNotInstalled = errors.New("whr is not an installed binary in an allowed prefix")
)

// GuardHost refuses `whr setup host` as root and as a standard whr user: the
// administrator's part is not for a standard account, which cannot sudo anyway.
// An administrator account that is the whr account may run it (D49, D46);
// admin says whether the account running this is one.
func GuardHost(user string, uid int, whrUser string, admin bool) error {
	if uid == 0 {
		return ErrRoot
	}
	if user == whrUser && !admin {
		return fmt.Errorf("%w: %s is workharbor's own standard account, which must not change the host; run `whr setup host` as your administrator", ErrWrongUser, user)
	}
	return nil
}

// GuardUser refuses `whr setup` as root, as any user but the configured one, and
// outside the user's graphical session: the same guard as `whr service install`
// (issue #38), because Apple Container's services are in that session's launchd
// domain and not reachable from SSH or `sudo -iu`.
func GuardUser(ctx context.Context, m launchd.Manager, user string, uid int, whrUser string) error {
	if uid == 0 {
		return ErrRoot
	}
	if user != whrUser {
		return fmt.Errorf("%w: it is for %s, and this is %s", ErrWrongUser, whrUser, user)
	}
	if err := m.CheckSession(ctx); err != nil {
		return fmt.Errorf("%w: open Terminal in %s's desktop session (on the Mac itself, or over Screen Sharing) and run it there", err, whrUser)
	}
	return nil
}

// CheckInstalled refuses a whr that is not the installed one: it must lie in a
// git working tree nowhere, and under one of the allowed prefixes. Ownership
// is checked separately by doctor; development prefixes are explicitly selected.
func CheckInstalled(path string, prefixes ...string) error {
	if err := launchd.CheckBinary(path); err != nil {
		return fmt.Errorf("%w: %w", ErrNotInstalled, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for _, p := range prefixes {
		if rp, err := filepath.EvalSymlinks(p); err == nil {
			p = rp
		}
		if rel, err := filepath.Rel(p, resolved); err == nil && !strings.HasPrefix(rel, "..") {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not under %s", ErrNotInstalled, resolved, strings.Join(prefixes, " or "))
}
