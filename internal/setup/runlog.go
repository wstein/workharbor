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
	ui.KV("cause", cause)
	if next != "" {
		ui.Action(next)
	}
	if tail := l.Tail(runlog.TailLines); tail != "" {
		ui.Note("last output:")
		ui.Tool(tail)
	}
	if p := l.Path(); p != "" {
		ui.KV("log", p)
	}
}
