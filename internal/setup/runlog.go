package setup

import (
	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/runlog"
)

// StepStatus is the word of a step in the run log: ok, fail or unknown.
func StepStatus(st doctor.Status) string {
	switch st {
	case doctor.OK, doctor.Warn:
		return "ok"
	case doctor.Fail:
		return "fail"
	}
	return "unknown"
}

// FailureSummary shows what a failed step leaves the person with: the cause,
// the next action, the last lines of the step's output and the log path. The
// log may be nil (nothing is logged, nothing is tailed).
func FailureSummary(ui render.Writer, l *runlog.Log, cause, next string) {
	FailureSummaryCmd(ui, l, cause, next, "")
}

// FailureSummaryCmd is FailureSummary with a command that ends the next
// action: it is set apart when it does not fit, never wrapped.
func FailureSummaryCmd(ui render.Writer, l *runlog.Log, cause, next, cmd string) {
	ui.KV("cause", cause)
	switch {
	case cmd != "":
		ui.ActionCmd(next, cmd)
	case next != "":
		ui.Action(next)
	}
	if tail := l.Tail(runlog.TailLines); tail != "" {
		ui.Note("last output:")
		ui.ToolTail(tail)
	}
	if p := l.Path(); p != "" {
		ui.KV("log", p)
	}
}

// causeOf is the line that says why a command failed: the last line the tool
// printed, which is its own error ("whr tools build: open x: no such file").
// Without output it is the reason whr composed.
func causeOf(l *runlog.Log, reason string) string {
	if last := runlog.LastLines(l.Tail(1), 1); last != "" {
		return last
	}
	return reason
}
