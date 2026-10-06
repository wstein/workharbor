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
	"path/filepath"
	"strings"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/launchd"
	"github.com/wstein/workharbor/internal/render"
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
// the person answers q, Run returns what it did so far and a *QuitError.
func Run(ctx context.Context, steps []doctor.Check, h Host, o Options) ([]Outcome, error) {
	chosen, err := Select(steps, o)
	if err != nil {
		return nil, err
	}
	ui := render.Writer{W: o.Err, S: o.Style}
	if len(chosen) > 0 {
		ui.Legend()
	}
	var outs []Outcome
	sudoReady := false
	provided := map[string]bool{} // services a step has brought up or found running
	for i, s := range chosen {
		if err := ctx.Err(); err != nil {
			return outs, err
		}
		title := s.Title
		if title == "" {
			title = s.Name
		}
		ui.Header(i+1, len(chosen), title)
		st, detail := s.Run(ctx)
		fmt.Fprintf(o.Out, "%s\t%s\t%s\n", st, s.Name, oneLine(detail))
		report(ui, o, st, detail)
		out := Outcome{Step: s.Name, Status: st, Detail: detail}
		if s.UseUser != nil {
			out.UseUser = s.UseUser(st)
		}
		if st == doctor.Warn && !s.FixOnWarn || st == doctor.Skipped {
			outs = append(outs, out)
			continue
		}
		if st == doctor.OK {
			if s.Provides != "" {
				provided[s.Provides] = true
			}
			outs = append(outs, out)
			continue
		}
		out.Todo = todoFor(title, s.Fix)
		if s.Fix == nil {
			ui.Report(render.LevelNotVerified, "whr has no fix for this step")
			outs = append(outs, out)
			continue
		}
		showFix(ui, s.Fix)
		if s.Needs != "" && !provided[s.Needs] && !o.DryRun && !providedElsewhere(ctx, steps, chosen, s.Needs) {
			ui.Report(render.LevelSkipped, fmt.Sprintf("not run: it needs %s, which no step before it brought up", s.Needs))
			out.Asked = true
			outs = append(outs, out)
			continue
		}
		if o.DryRun {
			if f := s.Fix; f.Guide != "" && hasCommands(f) {
				ui.Action("what happens next: " + oneLine(f.Guide))
			}
			ui.Report(render.LevelSkipped, "dry run: nothing is run")
			out.Asked = true
			outs = append(outs, out)
			continue
		}
		fixed, err := apply(ctx, h, s, o, ui, &sudoReady)
		if errors.Is(err, render.ErrQuit) {
			return outs, &QuitError{Step: s.Name, Resume: nextCommand(o, s.Name, names(chosen[i:]))}
		}
		if err != nil {
			reason, tool := render.SplitTool(oneLine(err.Error()))
			ui.Report(render.LevelFail, reason)
			if o.Verbose && tool != "" {
				ui.Tool(tool)
			}
		}
		out.Asked = !fixed
		if fixed {
			st, detail = s.Run(ctx)
			out.Status, out.Detail = st, detail
			out.Fixed = st == doctor.OK
			if out.Fixed && s.Provides != "" {
				provided[s.Provides] = true
			}
			fmt.Fprintf(o.Out, "%s\t%s\t%s\n", st, s.Name, oneLine(detail))
			report(ui, o, st, detail)
		}
		outs = append(outs, out)
	}
	return outs, nil
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
			ui.Action("this opens: " + f.Open)
		}
	case f.Desc != "":
		ui.Action(f.Desc)
	case len(f.Cmds) == 1:
		ui.Action("run this command")
	default:
		ui.Action("run these commands")
	}
	for _, c := range f.Cmds {
		ui.Command(quoteArgv(c.Full()))
	}
	if hasCommands(f) && f.Open != "" {
		ui.Action("this opens: " + f.Open)
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
			it.Commands = append(it.Commands, quoteArgv(c.Full()))
		}
		if f.Guide != "" {
			it.Text += ": " + oneLine(f.Guide)
		}
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
	line := render.Summary(o.Style, counts)
	if o.DryRun {
		line = strings.TrimSuffix(line, "\n") + " (dry run: nothing was changed)\n"
	}
	fmt.Fprint(w, line)
	fmt.Fprintf(w, "  done: %s\n", listOrNone(done))
	fmt.Fprintf(w, "  left: %s\n", listOrNone(left))
	if first != "" {
		next := ""
		if useUser != "" {
			argv := append([]string(nil), o.Resume...)
			if len(argv) == 0 {
				argv = []string{"whr", "setup"}
			}
			next = quoteArgv(append(argv, "--user", useUser))
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
	return quoteArgv(argv)
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

// quoteArgv writes an argument vector so a human can read where each argument
// begins; it is for display only. A control, bidirectional or separator
// character is never printed raw (a newline would start a second command when
// the line is pasted, an escape sequence would reach the terminal): the
// argument is quoted with the visible escape textsafe.Escape gives it.
func quoteArgv(argv []string) string {
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

// apply asks, then runs the fix. It returns whether a fix ran (so the check is
// worth running again) and what stopped it; render.ErrQuit when the person
// answered q.
func apply(ctx context.Context, h Host, s doctor.Check, o Options, ui render.Writer, sudoReady *bool) (bool, error) {
	f := s.Fix
	yes := func(question string, d render.Default) (bool, error) {
		a, err := Ask(h, question, d)
		if err != nil {
			return false, err
		}
		if a == render.Quit {
			return false, render.ErrQuit
		}
		return a == render.Yes, nil
	}
	if !hasCommands(f) { // guided: for what a command line cannot do
		if f.Open != "" {
			if ok, err := yes("Open it now?", render.DefaultYes); err != nil {
				return false, err
			} else if ok {
				if err := h.Open(ctx, f.Open); err != nil {
					ui.Report(render.LevelFail, "could not open it: "+oneLine(err.Error()))
				}
			}
		}
		done, err := yes("Done with this step? The check runs again.", render.DefaultYes)
		return done && err == nil, err
	}
	d := render.DefaultYes
	if f.Irreversible { // it cannot be undone: Enter is no
		d = render.DefaultNo
	}
	ok, err := yes("Ready to run this?", d)
	if err != nil || !ok {
		return false, err
	}
	if f.Guide != "" {
		ui.Action("what happens next: " + oneLine(f.Guide))
	}
	if o.Phase == doctor.PhaseHost && !*sudoReady && usesSudo(f) {
		// One sudo -v, no background refresh: root stays reachable only while the
		// human is here, and sudo asks again if it expires.
		ui.Action("sudo asks for your password once, so the commands above need it only once (no background refresh)")
		ui.Command("sudo -v")
		if err := h.Run(ctx, doctor.Cmd{Sudo: true, Argv: []string{"-v"}}); err != nil {
			return false, fmt.Errorf("sudo did not accept the password: %w", err)
		}
		*sudoReady = true
	}
	if f.Do != nil {
		if err := f.Do(ctx, h); err != nil {
			return false, err
		}
	}
	cmds := f.Cmds
	if f.Build != nil {
		var err error
		if cmds, err = f.Build(ctx, h); err != nil {
			return false, err
		}
		for _, c := range cmds { // the real commands, shown before they run
			ui.Command(quoteArgv(c.Full()))
		}
	}
	for _, c := range cmds {
		if err := h.Run(ctx, c); err != nil {
			return false, fmt.Errorf("%s failed: %w", quoteArgv(c.Full()), err)
		}
	}
	if f.Guide != "" && f.Open != "" {
		if ok, err := yes("Open the page that helps with the rest?", render.DefaultYes); err != nil {
			return true, err
		} else if ok {
			_ = h.Open(ctx, f.Open)
		}
	}
	return true, nil
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
