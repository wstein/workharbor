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

// EventConsoleSSH is the audit entry of SSH access to the console (issue #32): a
// certificate issued for a client's key, and a connection started. Who got in, and
// when, is kept; the certificate itself is not (it is a public value, but the key
// ID and serial identify it in sshd's log).
const EventConsoleSSH EventKind = "supervisor.console_ssh"

// ConsoleSSH is the payload of EventConsoleSSH.
type ConsoleSSH struct {
	Action     string    `json:"action"` // "certificate" or "connection"
	Actor      string    `json:"actor"`
	KeyID      string    `json:"key_id,omitempty"`
	Serial     uint64    `json:"serial,omitempty"`
	Forwarding bool      `json:"forwarding,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitzero"`
}

// NewConsoleSSHEvent returns the audit entry of SSH access to the console.
func NewConsoleSSHEvent(c ConsoleSSH, at time.Time) Event {
	return newEvent(SupervisorStream, EventConsoleSSH, c, at)
}
