package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/runtime/apple"
)

// shellTarget is the API's answer: the environment and the variables of a run.
type shellTarget struct {
	EnvID   string   `json:"env_id"`
	Runtime string   `json:"runtime"`
	User    string   `json:"user"`
	Dir     string   `json:"dir"`
	Env     []string `json:"env"`
	Cmd     []string `json:"cmd"`
}

func newWsShell(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "shell <workspace>",
		Short:             "Open a shell in a workspace's agent environment, to sign in to the agent",
		ValidArgsFunction: s.completeWorkspaces,
		Long: "Opens an interactive shell in the workspace's environment as the agent's user, with the agent's CLI " +
			"first on PATH and the agent's auth directory (CLAUDE_CONFIG_DIR) and the egress proxy set as for a run, so " +
			"that you can sign in with the vendor's own command (design D40; the first-run guide, step 8). The shell " +
			"uses absolute image paths /bin/bash or /bin/sh, with PATH restricted to the read-only tool store and fixed system directories. " +
			"The image's shells, libraries (including libc), system files and environment must be trusted: sanitisation inside /bin/sh " +
			"happens after its loader and libc initialise. Bash avoids startup files, readline and history from the agent's home (bash with " +
			"--noprofile --norc --noediting, so without line editing at its prompt, and INPUTRC=/dev/null and HISTFILE=/dev/null; sh with " +
			"ENV=/dev/null; its behaviour depends on the image's /bin/sh implementation and is unverified on the target image; " +
			"the variables that name another place to read terminfo, termcap, locale or startup files from are unset). " +
			"Not blocked: the image's own /etc files, and any program you start in the shell, such as less or vim, which still reads " +
			"~/.terminfo. This command asks the supervisor only " +
			"which environment and variables to use, then starts the runtime's own interactive exec as its child: " +
			"your terminal is attached to the environment directly, and nothing you type or the agent's CLI writes " +
			"passes through whr, the supervisor, a file, a log or the web UI. whr adds no secret and types nothing for " +
			"you. It is refused while a run of the workspace is unfinished. Preparation holds the environment through a stop, start " +
			"and readiness check, including when already running, preserving its home/login volumes and proxy. The environment stays held " +
			"while the shell is open: a run waits until you leave. If the hold connection ends, whr ends the shell child. Provisional (issue #282).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			open := s.env.TTY
			if open == nil {
				open = func() (TTY, error) { return osTerminal(s.env.Stdin, s.env.Stdout) }
			}
			if _, err := open(); err != nil { // only to see that there is a terminal; nothing is read or put in raw mode
				return usageError{"whr ws shell needs a terminal: standard input and output are not one"}
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			t, held, closeHold, err := c.openSignInShell(cmd.Context(), "/v1/workspaces/"+url.PathEscape(args[0])+"/shell")
			if err != nil {
				return err
			}
			defer closeHold()
			if t.Runtime != "apple-container" {
				return fmt.Errorf("this whr cannot open a shell in a %q environment", clean(t.Runtime))
			}
			env := t.Env
			if term := s.env.Getenv("TERM"); termName.MatchString(term) {
				env = append(append([]string(nil), env...), "TERM="+term)
			}
			if strings.HasPrefix(t.User, "-") || strings.HasPrefix(t.Dir, "-") {
				return fmt.Errorf("the shell description has a user or directory that starts with %q: refused", "-")
			}
			bin, argv, err := apple.InteractiveCommand(t.EnvID, runtime.InteractiveRequest{Cmd: t.Cmd, Env: env, Dir: t.Dir, User: t.User})
			if err != nil {
				return err
			}
			fmt.Fprintln(s.env.Stderr, "whr: opening a shell in the environment of "+clean(args[0])+" as the agent's user; sign in with `claude auth login`, leave with exit")
			run := s.env.ShellChild
			if run == nil {
				run = runShellChild
			}
			if s.env.ShellSignals != nil {
				restore := s.env.ShellSignals()
				defer restore()
			}
			code, err := run(held, bin, argv, os.Environ())
			if held.Err() != nil {
				return fmt.Errorf("the sign-in shell hold ended: the runtime's child was ended")
			}
			if err != nil {
				return fmt.Errorf("start the runtime's shell: %w", err)
			}
			if code != 0 {
				return childExit{code}
			}
			return nil
		},
	}
}

// Preserve the runtime child's exit status through Execute.
type childExit struct{ code int }

func (e childExit) Error() string { return "" }
func (e childExit) ExitCode() int { return e.code }
