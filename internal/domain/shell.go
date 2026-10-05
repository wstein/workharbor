package domain

import "time"

// EventSignInShell records a sign-in shell target handed to an API caller.
// It contains metadata only, never terminal data or the target's variables.
const EventSignInShell EventKind = "supervisor.signin_shell"

// SignInShellOpened identifies the caller and environment, without session content.
type SignInShellOpened struct {
	Actor     string `json:"actor"`
	Workspace string `json:"workspace"`
	EnvID     ID     `json:"env_id"`
}

// NewSignInShellEvent returns the metadata-only audit entry.
func NewSignInShellEvent(s SignInShellOpened, at time.Time) Event {
	return newEvent(SupervisorStream, EventSignInShell, s, at)
}
