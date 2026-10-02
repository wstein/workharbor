package domain

import "time"

// EventConsole is the audit entry of a console shell opened (design D43, §7.7):
// a shell in the console is the human working in a VM that reads every workspace,
// so who opened it and which workspaces were writable is kept. It belongs to no
// task, so it is in the supervisor's own stream.
const EventConsole EventKind = "supervisor.console"

// ConsoleOpened is the payload of EventConsole.
type ConsoleOpened struct {
	Actor     string   `json:"actor"`
	Workspace string   `json:"workspace,omitempty"` // the workspace the shell started in
	ReadWrite []string `json:"read_write"`          // the workspaces the console has writable
}

// NewConsoleEvent returns the audit entry of a console shell.
func NewConsoleEvent(c ConsoleOpened, at time.Time) Event {
	if c.ReadWrite == nil {
		c.ReadWrite = []string{}
	}
	return newEvent(SupervisorStream, EventConsole, c, at)
}
