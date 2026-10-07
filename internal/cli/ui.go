package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup"
)

// isTTY reports whether w is a terminal. A test sets Env.IsTTY.
func (st *state) isTTY(w io.Writer) bool {
	if st.env.IsTTY != nil {
		return st.env.IsTTY(w)
	}
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor of this process
}

// style is how human text on w is drawn: colour and symbols only when both
// stdout and w are terminals, TERM is not dumb, NO_COLOR is empty and neither
// --plain nor --no-color is given. FORCE_COLOR and --color=always force colour.
func (st *state) style(w io.Writer, plain bool) render.Style {
	g := st.env.Getenv
	return render.DetectEnv(render.Env{
		TTY:         st.isTTY(st.env.Stdout) && st.isTTY(w),
		Term:        g("TERM"),
		NoColor:     g("NO_COLOR"),
		ForceColor:  g("FORCE_COLOR"),
		NoColorFlag: st.noColor || st.color == "never",
		ColorAlways: st.color == "always",
		Plain:       plain,
		Cols:        termCols(w),
	})
}

// termCols is the width of the terminal behind w, or 0 when w is not one.
func termCols(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	n, _, err := term.GetSize(int(f.Fd())) //nolint:gosec // a file descriptor fits an int
	if err != nil {
		return 0
	}
	return n
}

// quitError ends `whr setup` after the person answered q: a distinct exit code,
// not a failure, and the hint was already printed.
type quitError struct{}

func (quitError) Error() string { return "" }
func (quitError) ExitCode() int { return exitcode.Quit }

// interruptedError ends `whr setup` after Ctrl-C, SIGTERM or a deadline: a code
// of its own, so a script can tell it from a refused command line.
type interruptedError struct{}

func (interruptedError) Error() string {
	return "interrupted"
}
func (interruptedError) ExitCode() int { return exitcode.Interrupted }

// printInterrupted says which step was cut short, or not yet started, and the
// command that goes on.
func printInterrupted(ui render.Writer, e *setup.InterruptedError) {
	switch e.When {
	case setup.BeforeStep:
		ui.Report(render.LevelSkipped, "interrupted: no step was running, the next one had not started")
		ui.Action("to go on, run")
	case setup.AfterLastStep:
		ui.Report(render.LevelSkipped, "interrupted: every step had finished")
		ui.Action("to check the last step again, run")
	default:
		ui.Report(render.LevelSkipped, "interrupted: the step that was running did not finish")
		ui.Action("to check it and go on, run")
	}
	ui.Command(e.Resume)
}

// printQuit says where to go on after q.
func printQuit(ui render.Writer, q *setup.QuitError) {
	ui.Report(render.LevelSkipped, "stopped at your request, nothing more was run")
	ui.Action("to go on later, run")
	ui.Command(q.Resume)
}

// printDoctorHuman is the readable report of `whr doctor` on stderr: grouped by
// phase, the names aligned, each problem once, the fix as a copyable command,
// a one-line summary and the numbered list of what to do now.
func printDoctorHuman(ui render.Writer, rs []doctor.Result, verbose bool) {
	width := 0
	for _, r := range rs {
		width = max(width, len(r.Check))
	}
	ui.Legend()
	var counts render.Counts
	var todo []render.TodoItem
	for _, group := range []struct {
		phase doctor.Phase
		title string
	}{
		{doctor.PhaseHost, "Host steps (whr setup host, as the administrator)"},
		{doctor.PhaseUser, "User steps (whr setup, as workharbor)"},
		{"", "Shared checks"},
	} {
		first := true
		for _, r := range rs {
			if r.Phase != group.phase {
				continue
			}
			if first {
				ui.Section(group.title)
				first = false
			}
			reason, tool := render.SplitTool(clean(strings.Join(strings.Fields(r.Detail), " ")))
			ui.Report(setup.Level(r.Status), fmt.Sprintf("%-*s  %s", width, r.Check, reason))
			if verbose && tool != "" {
				ui.Tool(tool)
			}
			switch r.Status {
			case doctor.OK:
				counts.OK++
			case doctor.Fail:
				counts.Fail++
			case doctor.NotVerified:
				counts.NotVerified++
			case doctor.Warn:
				counts.Warn++
			case doctor.Skipped:
				counts.Skipped++
			}
			if r.Fix != "" {
				fix := clean(r.Fix)
				ui.Action("to fix " + r.Check + ", run")
				ui.Command(fix)
				todo = append(todo, render.TodoItem{Text: r.Check + ": " + reason, Commands: []string{fix}})
			}
		}
	}
	ui.Rule()
	ui.Summary(counts)
	ui.Todo(todo)
}
