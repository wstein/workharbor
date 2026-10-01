package service

import (
	"fmt"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
)

// Briefing is the supervisor's first message to an agent that resumes after a
// pause, a cancel or an interruption (design D27). Left to its own account, a
// resumed agent can be wrong about what happened: in spike #7 a resumed Claude
// Code session told the model that a command that had been killed mid-run
// "was never executed". The briefing says what the supervisor knows: the
// process ended, a tool call that was running or waiting for approval may have
// had effects that are unknown or partial, which Decisions were superseded, and
// that the workspace must be checked before anything is repeated or skipped.
// instruction is the caller's own prompt, if any, and follows the briefing.
func Briefing(task domain.Task, run domain.Run, superseded []domain.Decision, instruction string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Supervisor briefing for run %s of task %s.\n\n", run.ID, task.ID)
	b.WriteString("Your previous process ended and you are being resumed. This may have been a supervisor restart, a pause or a cancel.\n")
	b.WriteString("A tool call that was running, or waiting for approval, when it ended may have had effects that are unknown or only partial. ")
	b.WriteString("Do not assume it was executed, and do not assume it was not.\n")
	if len(superseded) > 0 {
		b.WriteString("\nThese requests were open and are superseded; nobody answered them:\n")
		for _, d := range superseded {
			line := d.Subject
			if d.Input != "" {
				line += ": " + oneLine(d.Input)
			}
			fmt.Fprintf(&b, "- %s (%s)\n", line, d.Kind)
		}
		b.WriteString("Ask again if you still need them.\n")
	}
	b.WriteString("\nCheck the workspace first (git status, git diff, the files you were changing) before you repeat or skip any step.\n")
	if strings.TrimSpace(instruction) != "" {
		b.WriteString("\n" + strings.TrimSpace(instruction) + "\n")
	}
	return b.String()
}

// oneLine keeps an untrusted input to a short single line.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200]) + "…"
	}
	return s
}
