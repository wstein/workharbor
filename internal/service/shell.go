package service

import (
	"context"
	"fmt"

	"github.com/wstein/workharbor/internal/domain"
)

// ShellConfig is what the agent shell needs besides the workspace: where the
// agent's CLI and home are in the environment. It holds no secret.
type ShellConfig struct {
	// Bin is the directory of the agent's CLI in the environment, put first on
	// PATH.
	Bin string
	// Env are the variables a run has besides the proxy's: the agent's home and its
	// auth directory (CLAUDE_CONFIG_DIR, never $HOME's own dotfiles).
	Env []string
	// Dir is the directory the shell starts in: the agent's home.
	Dir string
}

// ShellTarget is the answer to a request for the agent shell: the environment and
// the variables of a run, which the human's own `whr` process hands to the
// runtime's interactive exec (design §7.3, "The sign-in shell"). The supervisor
// carries no terminal: it never sees what the human types or what the agent's CLI
// writes, and this value is not stored. It holds no secret.
type ShellTarget struct {
	EnvID   string   `json:"env_id"`
	Runtime string   `json:"runtime"` // the adapter's name, which says how to exec
	User    string   `json:"user"`    // the agent's user, "uid:gid"
	Dir     string   `json:"dir"`
	Env     []string `json:"env"` // KEY=VALUE: the run's variables and the egress proxy's, as agentEnv sets them
	Cmd     []string `json:"cmd"` // the shell, with the agent's CLI first on PATH
}

// shellScript puts the agent's CLI first on PATH and starts an interactive shell
// that does not read the agent-writable files it would otherwise trust: the
// agent's home is a volume the agent writes, so what a shell, readline or the
// terminal database finds there is the agent's, and would run in, rebind keys of,
// or send bytes to the human's terminal beside the login (review of #281, M1 to
// M3). bash gets --noprofile --norc (no startup file) and --noediting: no
// readline, so neither ~/.inputrc (a binding could append a command to the typed
// line) nor ~/.terminfo (an entry could make every edit write bytes the agent
// chose, such as an OSC 52 clipboard write) is read. The cost is line editing at
// the bash prompt; the login code is pasted into the CLI's own prompt, not
// bash's. INPUTRC=/dev/null stays as a second wall. HISTFILE=/dev/null keeps what
// the human types out of a history file the next run's agent reads (M2). The
// variables that name another place to read from are unset (TERMINFO,
// TERMINFO_DIRS, TERMCAP, LOCPATH, GCONV_PATH, NLSPATH, BASH_ENV, CDPATH,
// PROMPT_COMMAND) and EDITRC=/dev/null covers a libedit shell. sh, the fallback
// when the image has no bash, gets ENV=/dev/null; dash and busybox ash have no
// readline and read no terminfo (reasoned from their documentation, not
// measured). HOME stays set for the CLI. NOT blocked: the image's own /etc files
// (/etc/inputrc, /etc/bash.bashrc, /etc/terminfo) belong to the image, which has
// to come from the human's configuration or the repository's default branch; and
// any program the human starts in the shell (less, vim, clear) still reads
// ~/.terminfo, because it is the agent's home and the terminal database path is
// the program's. The CLI's own output still reaches the terminal as it is: whr
// has no part in the stream, so it cannot filter it (an accepted risk for
// wh/design to name, #223). The directory arrives as $1, so no path is spliced
// into the script.
const shellScript = `PATH="$1:$PATH"; export PATH; unset TERMINFO TERMINFO_DIRS TERMCAP LOCPATH GCONV_PATH NLSPATH BASH_ENV CDPATH PROMPT_COMMAND; INPUTRC=/dev/null; HISTFILE=/dev/null; EDITRC=/dev/null; export INPUTRC HISTFILE EDITRC; if command -v bash >/dev/null 2>&1; then exec bash --noprofile --norc --noediting -i; fi; ENV=/dev/null; export ENV; exec sh -i`

// ShellTarget finds a workspace's environment, makes sure it is running and
// returns what the human's terminal needs to open a shell in it as the agent's
// user, to sign in to the agent (D40). It is refused while a run of the workspace
// is unfinished (live or interrupted: the reconciler would resume it), under the
// lock a run's start takes, like a rebuild. The check is made at open time only:
// the caller's process then replaces itself with the runtime's exec, so nothing
// holds the environment (Service.HoldEnvironment would need a process that stays
// and waits), and a run may start while the shell is open (review of #281, L1). It adds no secret, opens no terminal
// and writes nothing: the caller replaces its own process with the runtime's
// interactive exec.
func (w *Workspaces) ShellTarget(ctx context.Context, workspace string) (ShellTarget, error) {
	if w.cfg.Shell == nil {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "this supervisor has no agent shell")
	}
	if !w.svc.rt.Capabilities().InteractiveExec {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "the %s runtime does not report an interactive exec, so there is no agent shell", w.svc.rt.Name())
	}
	ws, err := w.svc.store.Workspace(ctx, workspace)
	if err != nil {
		return ShellTarget{}, err
	}
	if ws.EnvID == "" {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	if err := w.noRunLive(ctx, ws); err != nil {
		return ShellTarget{}, err
	}
	if w.svc.rebuilding(ws.ID) {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "workspace %s is being rebuilt: try again when it is done", ws.Name)
	}
	if err := w.ensureEnvironment(ctx, ws); err != nil {
		return ShellTarget{}, fmt.Errorf("make the environment of workspace %s ready: %w", ws.Name, err)
	}
	env := append(append([]string(nil), w.cfg.Shell.Env...), w.svc.agentEnv(ctx, ws.EnvID)...)
	return ShellTarget{
		EnvID:   string(ws.EnvID),
		Runtime: w.svc.rt.Name(),
		User:    w.cfg.Spec(ws).User,
		Dir:     w.cfg.Shell.Dir,
		Env:     env,
		Cmd:     []string{"/bin/sh", "-c", shellScript, "whr-shell", w.cfg.Shell.Bin},
	}, nil
}

// noRunLive refuses a workspace that has an unfinished run, naming it.
func (w *Workspaces) noRunLive(ctx context.Context, ws domain.Workspace) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	open, err := w.svc.store.UnfinishedRuns(ctx, ws.EnvID)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		r := open[0]
		return domain.NewConflict(domain.RuleAgentActive, "workspace %s has run %s (%s) of agent %s: finish, stop or fail it before you sign in, because a shell in its environment would share the agent's login", ws.Name, r.ID, r.State, r.AgentID)
	}
	return nil
}
