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

// Options say what to run.
type Options struct {
	Phase  doctor.Phase
	DryRun bool
	Only   []string // run only these steps, optional ones too
	From   string   // start at this step
	// Out gets the data (one line per step), Err the human text.
	Out, Err io.Writer
}

// Outcome is what became of one step.
type Outcome struct {
	Step   string
	Status doctor.Status
	Detail string
	Fixed  bool // a fix ran and the check passed afterwards
	Asked  bool // a fix was offered and declined or left undone
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
func Run(ctx context.Context, steps []doctor.Check, h Host, o Options) ([]Outcome, error) {
	chosen, err := Select(steps, o)
	if err != nil {
		return nil, err
	}
	var outs []Outcome
	sudoReady := false
	for _, s := range chosen {
		if err := ctx.Err(); err != nil {
			return outs, err
		}
		st, detail := s.Run(ctx)
		fmt.Fprintf(o.Out, "%s\t%s\t%s\n", st, s.Name, oneLine(detail))
		out := Outcome{Step: s.Name, Status: st, Detail: detail}
		if st == doctor.OK {
			fmt.Fprintf(o.Err, "%s: already done: %s\n", s.Name, oneLine(detail))
			outs = append(outs, out)
			continue
		}
		if s.Fix == nil {
			fmt.Fprintf(o.Err, "%s: %s: %s\n  this step has no fix\n", s.Name, s.Title, oneLine(detail))
			outs = append(outs, out)
			continue
		}
		fmt.Fprintf(o.Err, "\n%s: %s\n  %s\n", s.Name, s.Title, oneLine(detail))
		show(o.Err, s.Fix)
		if o.DryRun {
			fmt.Fprintln(o.Err, "  (dry run: nothing is run)")
			out.Asked = true
			outs = append(outs, out)
			continue
		}
		fixed, err := apply(ctx, h, s, o, &sudoReady)
		if err != nil {
			fmt.Fprintf(o.Err, "  %s: %v\n", s.Name, err)
		}
		out.Asked = !fixed
		if fixed {
			st, detail = s.Run(ctx)
			out.Status, out.Detail = st, detail
			out.Fixed = st == doctor.OK
			fmt.Fprintf(o.Out, "%s\t%s\t%s\n", st, s.Name, oneLine(detail))
		}
		outs = append(outs, out)
	}
	return outs, nil
}

// show prints a fix: what it does and the exact commands, as argument vectors
// written out for reading. They are never run through a shell.
func show(w io.Writer, f *doctor.Fix) {
	if f.Desc != "" {
		fmt.Fprintf(w, "  does: %s\n", f.Desc)
	}
	for _, c := range f.Cmds {
		fmt.Fprintf(w, "  $ %s\n", quoteArgv(c.Full()))
	}
	if f.Guide != "" {
		fmt.Fprintf(w, "  %s\n", f.Guide)
	}
	if f.Open != "" {
		fmt.Fprintf(w, "  opens: %s\n", f.Open)
	}
}

// quoteArgv writes an argument vector so a human can read where each argument
// begins; it is for display only.
func quoteArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"'$`\\<>|&;*?") {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// apply asks, then runs the fix. It returns whether a fix ran (so the check is
// worth running again) and what stopped it.
func apply(ctx context.Context, h Host, s doctor.Check, o Options, sudoReady *bool) (bool, error) {
	f := s.Fix
	if f.Do == nil && f.Build == nil && len(f.Cmds) == 0 { // guided: for what a command line cannot do
		if f.Open != "" {
			if ok, err := h.Confirm("Open it now?"); err != nil {
				return false, err
			} else if ok {
				if err := h.Open(ctx, f.Open); err != nil {
					fmt.Fprintf(o.Err, "  could not open it: %v\n", err)
				}
			}
		}
		done, err := h.Confirm("Done with this step? The check runs again")
		return done && err == nil, err
	}
	ok, err := h.Confirm("Run this?")
	if err != nil || !ok {
		return false, err
	}
	if o.Phase == doctor.PhaseHost && !*sudoReady && usesSudo(f) {
		// One sudo -v, no background refresh: root stays reachable only while the
		// human is here, and sudo asks again if it expires.
		fmt.Fprintf(o.Err, "  $ sudo -v   (once, so the commands above ask for your password only once; no background refresh)\n")
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
			fmt.Fprintf(o.Err, "  $ %s\n", quoteArgv(c.Full()))
		}
	}
	for _, c := range cmds {
		if err := h.Run(ctx, c); err != nil {
			return false, fmt.Errorf("%s failed: %w", quoteArgv(c.Full()), err)
		}
	}
	if f.Guide != "" && f.Open != "" {
		if ok, _ := h.Confirm("Open the page that helps with the rest?"); ok {
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
	ErrNotInstalled = errors.New("whr is not an installed binary in an admin-owned prefix")
)

// GuardHost refuses `whr setup host` as root and as the whr user: the
// administrator's part is not for the supervisor's account (D46).
func GuardHost(user string, uid int, whrUser string) error {
	if uid == 0 {
		return ErrRoot
	}
	if user == whrUser {
		return fmt.Errorf("%w: %s is workharbor's own account, which must not change the host; run `whr setup host` as your administrator", ErrWrongUser, user)
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
// git working tree nowhere, and under one of the admin-owned prefixes (D24).
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
