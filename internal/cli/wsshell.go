package cli

import (
	"encoding/json"
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
			"reads no startup file, ~/.inputrc, ~/.terminfo or history file of the agent's home, which the agent writes (bash with " +
			"--noprofile --norc --noediting, so without line editing at its prompt, and INPUTRC=/dev/null and HISTFILE=/dev/null; sh with " +
			"ENV=/dev/null; the variables that name another place to read terminfo, termcap, locale or startup files from are unset). " +
			"Not blocked: the image's own /etc files, and any program you start in the shell, such as less or vim, which still reads " +
			"~/.terminfo. This command asks the supervisor only " +
			"which environment and variables to use, then replaces itself with the runtime's own interactive exec: " +
			"your terminal is attached to the environment directly, and nothing you type or the agent's CLI writes " +
			"passes through whr, the supervisor, a file, a log or the web UI. whr adds no secret and types nothing for " +
			"you. It is refused while a run of the workspace is unfinished, and checked when the shell opens only: do " +
			"not start a run while it is open. Provisional (issue #281).",
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
			_, data, err := c.Do(cmd.Context(), "POST", "/v1/workspaces/"+url.PathEscape(args[0])+"/shell", nil, "")
			if err != nil {
				return err
			}
			var t shellTarget
			if err := json.Unmarshal(data, &t); err != nil {
				return fmt.Errorf("the shell description is not what this whr expects: %w", err)
			}
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
			run := s.env.Exec
			if run == nil {
				run = execReplace
			}
			// Past this call whr is gone, on success: the terminal is the runtime's.
			if err := run(bin, argv, os.Environ()); err != nil {
				return fmt.Errorf("start the runtime's shell: %w", err)
			}
			return nil
		},
	}
}
