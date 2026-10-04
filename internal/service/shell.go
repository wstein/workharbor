package service

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
)

// ShellConfig is what the agent shell needs besides the workspace: where the
// agent's CLI and home are in the environment. It holds no secret.
type ShellConfig struct {
	// Bin is the directory of the agent's CLI in the environment, put first on
	// PATH. It must be an absolute directory below the read-only /tools mount.
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

// shellScript replaces the image's PATH with the read-only tool store and fixed
// system directories, selecting /bin/bash or /bin/sh by absolute image path.
// The image's shells, libraries (including libc), system files and mounts must
// be trusted. Unsetting variables happens inside /bin/sh, after its loader and
// libc initialise; this is not protection from an untrusted image environment.
// Programs the human starts, including the vendor CLI, have their own startup
// and terminal behaviour. Their output is not filtered. The script starts a shell
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
// readline and read no terminfo (unverified for the target image); other /bin/sh
// implementations may behave differently. HOME stays set for the CLI. NOT blocked: the image's own /etc files
// (/etc/inputrc, /etc/bash.bashrc, /etc/terminfo) belong to the image, which has
// to come from the human's configuration or the repository's default branch; and
// any program the human starts in the shell (less, vim, clear) still reads
// ~/.terminfo, because it is the agent's home and the terminal database path is
// the program's. The CLI's own output still reaches the terminal as it is: whr
// has no part in the stream, so it cannot filter it (an accepted risk for
// wh/design to name, #223). The directory arrives as $1, so no path is spliced
// into the script.
const shellScript = `PATH="$1:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"; export PATH; unset TERMINFO TERMINFO_DIRS TERMCAP LOCPATH GCONV_PATH NLSPATH BASH_ENV CDPATH PROMPT_COMMAND; INPUTRC=/dev/null; HISTFILE=/dev/null; EDITRC=/dev/null; export INPUTRC HISTFILE EDITRC; if [ -x /bin/bash ]; then exec /bin/bash --noprofile --norc --noediting -i; fi; ENV=/dev/null; export ENV; exec /bin/sh -i`

// ShellTarget finds a workspace's environment, restarts it even when warm and
// returns what the human's terminal needs to open a shell in it as the agent's
// user, to sign in to the agent (D40). It is refused while a run of the workspace
// is unfinished (live or interrupted: the reconciler would resume it), under the
// environment hold and restart lock. Preparation blocks run ownership and rebuilds
// until stop, start and readiness succeed, keeping the existing mounts and proxy.
// The hold ends before the target is returned; it does not cover the terminal:
// the caller's process then replaces itself with the runtime's exec, so nothing
// holds the environment (Service.HoldEnvironment would need a process that stays
// and waits), and a run may start while the shell is open (review of #281, L1). It adds no secret, opens no terminal
// and writes no terminal data: the caller replaces its own process with the runtime's
// interactive exec.
func (w *Workspaces) ShellTarget(ctx context.Context, workspace string) (ShellTarget, error) {
	if w.cfg.Shell == nil {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "this supervisor has no agent shell")
	}
	if !w.svc.rt.Capabilities().InteractiveExec {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "the %s runtime does not report an interactive exec, so there is no agent shell", w.svc.rt.Name())
	}
	if bin := w.cfg.Shell.Bin; !strings.HasPrefix(bin, "/tools/") || path.Clean(bin) != bin || strings.ContainsAny(bin, ":\x00\r\n") {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "the agent shell needs an absolute CLI directory below the read-only /tools mount")
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
	release, err := w.svc.HoldEnvironment(ctx, ws)
	if err != nil {
		return ShellTarget{}, err
	}
	defer release()
	current, err := w.svc.store.Workspace(ctx, string(ws.ID))
	if err != nil {
		return ShellTarget{}, err
	}
	if current.EnvID != ws.EnvID {
		return ShellTarget{}, domain.NewConflict(domain.RuleEnvRunning, "the environment of workspace %s changed while preparing the shell: try again", ws.Name)
	}
	if err := w.restartForShell(ctx, ws); err != nil {
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

func (w *Workspaces) restartForShell(ctx context.Context, ws domain.Workspace) error {
	w.svc.freshMu.Lock()
	defer w.svc.freshMu.Unlock()
	w.svc.forgetEnvStarted(ws.EnvID)
	if err := w.svc.stopLeftover(ctx, "", ws.EnvID, false, nil); err != nil {
		return err
	}
	if err := w.svc.startEnv(ctx, string(ws.EnvID)); err != nil {
		return err
	}
	return w.svc.waitReady(ctx, ws.EnvID)
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
