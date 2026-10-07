package doctor

import "context"

// Phase says which account runs a step (design D46): the administrator's part of
// the host setup, or the `whr` user's part in its desktop session. A check with
// no phase is shared: `whr doctor` and both parts of the wizard run it.
type Phase string

const (
	PhaseHost Phase = "host" // `whr setup host`, as the administrator
	PhaseUser Phase = "user" // `whr setup`, as workharbor in its desktop session
)

// Cmd is one command a fix runs: an argument vector, never a shell string, so
// nothing is parsed twice and nothing in it can add a command. With Sudo it runs
// as `sudo <argv>`, one privileged command at a time, shown first. A file that
// must be root's is written to a private temporary file first and installed with
// `sudo install`, never through a shell or a redirect.
type Cmd struct {
	Argv []string
	Sudo bool
	// SecretPrompt, if set, says the command reads one secret (a password) from
	// its standard input. The host asks for it with that prompt, with terminal
	// echo off, and writes it to the command's stdin as one line. The secret is
	// never in Argv, the environment, a log or a report, and the command is not
	// given the terminal as its input (issue #378).
	SecretPrompt string
}

// Full is the argument vector that is run, with sudo in front when it is needed.
func (c Cmd) Full() []string {
	if c.Sudo {
		return append([]string{"sudo"}, c.Argv...)
	}
	return c.Argv
}

// Prompter is how a fix asks the human for a value.
type Prompter interface {
	// Line asks a question and reads an answer that may be echoed.
	Line(question string) (string, error)
	// Secret asks for a value that is not echoed and never shown again.
	Secret(question string) (string, error)
	// Confirm asks a yes or no question; anything but y is no.
	Confirm(question string) (bool, error)
	// Show prints text for the human, such as a diff.
	Show(text string)
}

// Fix is what `whr setup` offers when a check does not pass: the commands it
// would run, or an in-process action, or a text for what a command line cannot
// do. Everything is shown before anything runs.
type Fix struct {
	// Cmds are run in order; the first failure stops the fix. When Build is set
	// they are only the preview shown before it, with a placeholder where a value
	// is still to be asked.
	Cmds []Cmd
	// Build, if set, asks for what the commands need and returns the real ones.
	// It runs after the confirmation, so nothing is asked in a dry run.
	Build func(ctx context.Context, p Prompter) ([]Cmd, error)
	// Do is an in-process action (writing a secret, a configuration), described
	// by Desc. It runs before Cmds.
	Do   func(ctx context.Context, p Prompter) error
	Desc string
	// Guide is shown for what macOS or a third party keeps for the human, and
	// Open is the System Settings pane or link that helps with it.
	Guide string
	Open  string
	// Irreversible marks a fix that cannot be undone by running it again or
	// reverting by hand (removing the account's administrator rights): the
	// wizard then defaults to no, [y/N/q], where it defaults to yes otherwise.
	Irreversible bool
}

// Runner is what the checks use to look at the machine: they only read. A test
// passes a fake, so no check touches the machine except through it. The fixes run
// through the wizard's own, larger interface (package setup).
type Runner interface {
	// Output runs a read-only command and returns what it printed. It attaches no
	// terminal and never uses sudo.
	Output(ctx context.Context, argv ...string) ([]byte, error)
}
