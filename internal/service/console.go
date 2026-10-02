package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/sshca"
)

// Labels of the console environment (design D43). They are the only record of
// it: a console is not a task's or a workspace's environment, so the reconciler
// never touches it, and the runtime's own list says whether one exists.
const (
	ConsoleLabel   = "whr.console"    // "1" on the console environment
	ConsoleRWLabel = "whr.console.rw" // the IDs of the workspaces mounted read-write, comma-separated and sorted
)

// ConsoleConfig is what the console operations need besides the service.
type ConsoleConfig struct {
	// Spec returns the console's spec with the given workspaces mounted
	// read-write: the console image, limits, network and egress allowlist, the
	// workspace roots read-only and a home volume. It sets the two labels.
	Spec func(rw []domain.Workspace) runtime.Spec
	// Prepare is runtime.Prepare with this host's filesystem, roots and volume
	// ownership.
	Prepare func(runtime.Spec) (runtime.PreparedSpec, error)
	// EnsureImage makes sure the console image exists, building it on first use.
	// Optional.
	EnsureImage func(ctx context.Context) error
	// Dir returns the directory a workspace has in the console. Optional: without
	// it a shell starts in /workspaces.
	Dir func(w domain.Workspace) string
	// SSH is the certificate authority of the console's SSH access (issue #32).
	// Optional: without it the SSH operations answer ErrNoSSH.
	SSH *sshca.CA
	// SSHCmd replaces the in-guest sshd launcher. For tests; empty in production.
	SSHCmd []string
}

// Consoles opens and closes the console environment of design D43: an
// environment without an agent, for the human's shell work across workspaces.
type Consoles struct {
	svc *Service
	cfg ConsoleConfig

	mu     sync.Mutex
	shells int                           // the shells open now
	open   map[*countedTerminal]struct{} // the same, to close them at shutdown

	sshConns int                   // the SSH connections open now
	openSSH  map[*sshConn]struct{} // the same, to close them at shutdown
}

// NewConsoles returns the console operations of a service.
func NewConsoles(s *Service, cfg ConsoleConfig) *Consoles { return &Consoles{svc: s, cfg: cfg} }

// ConsoleInfo says what the open console is.
type ConsoleInfo struct {
	EnvID     string   `json:"env_id"`
	ReadWrite []string `json:"read_write"` // the names of the workspaces mounted read-write
	Reused    bool     `json:"reused"`     // an open console served the request
}

// current finds the console environment, if the runtime has one. At most one
// exists: the home volume is writable, so a second would be refused.
func (c *Consoles) current(ctx context.Context) (*runtime.Info, error) {
	infos, err := c.svc.rt.List(ctx, c.svc.cfg.Owner)
	if err != nil {
		return nil, err
	}
	for i := range infos {
		if infos[i].Labels[ConsoleLabel] == "1" {
			return &infos[i], nil
		}
	}
	return nil, nil
}

// writable resolves workspaces by name or ID and returns them sorted by ID, so
// the same set always gives the same label.
func (c *Consoles) writable(ctx context.Context, names []string) ([]domain.Workspace, error) {
	seen := map[domain.ID]bool{}
	var out []domain.Workspace
	for _, n := range names {
		ws, err := c.svc.store.Workspace(ctx, n)
		if err != nil {
			return nil, err
		}
		if !seen[ws.ID] {
			seen[ws.ID] = true
			out = append(out, ws)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func idsOf(ws []domain.Workspace) string {
	ids := make([]string, len(ws))
	for i, w := range ws {
		ids[i] = string(w.ID)
	}
	return strings.Join(ids, ",")
}

// Open makes sure a console is running with exactly the requested workspaces
// mounted read-write, and returns it. With none requested, every workspace is
// read-only. A console that is already open with the same set is reused,
// started if it was stopped; one open with another set is a conflict, because
// changing the mounts of an environment others may be using would pull the
// floor from under their shells: close it first.
func (c *Consoles) Open(ctx context.Context, readWrite []string) (ConsoleInfo, error) {
	rw, err := c.writable(ctx, readWrite)
	if err != nil {
		return ConsoleInfo{}, err
	}
	names := make([]string, len(rw))
	for i, w := range rw {
		names[i] = w.Name
	}
	if cur, err := c.current(ctx); err != nil {
		return ConsoleInfo{}, err
	} else if cur != nil {
		if cur.Labels[ConsoleRWLabel] != idsOf(rw) {
			return ConsoleInfo{}, domain.NewConflict(domain.RuleEnvRunning,
				"the console is open with other writable workspaces: close it first, then open it with the workspaces you want writable")
		}
		if cur.State != domain.EnvRunning {
			if err := c.svc.rt.Start(ctx, cur.ID); err != nil {
				return ConsoleInfo{}, fmt.Errorf("start the console %s: %w", cur.ID, err)
			}
		}
		if err := c.svc.waitReady(ctx, domain.ID(cur.ID)); err != nil {
			return ConsoleInfo{}, err
		}
		return ConsoleInfo{EnvID: cur.ID, ReadWrite: names, Reused: true}, nil
	}

	if c.cfg.EnsureImage != nil {
		if err := c.cfg.EnsureImage(ctx); err != nil {
			return ConsoleInfo{}, fmt.Errorf("the console image: %w", err)
		}
	}
	spec := c.cfg.Spec(rw)
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}
	spec.Labels[ConsoleLabel] = "1"
	spec.Labels[ConsoleRWLabel] = idsOf(rw)
	prep, err := c.cfg.Prepare(spec)
	if err != nil {
		return ConsoleInfo{}, err
	}
	env, err := c.svc.rt.Provision(ctx, prep)
	if err != nil {
		return ConsoleInfo{}, err
	}
	// The home volume is the human's own and outlives the console, so a failed
	// open takes back the environment, its network and its sidecar, not the volume.
	fail := func(err error) (ConsoleInfo, error) {
		bg := context.WithoutCancel(ctx)
		_ = c.svc.rt.Stop(bg, env)
		_ = c.svc.rt.Delete(bg, env)
		return ConsoleInfo{}, err
	}
	if err := c.svc.rt.Start(ctx, env); err != nil {
		return fail(fmt.Errorf("start the console %s: %w", env, err))
	}
	if err := c.svc.waitReady(ctx, domain.ID(env)); err != nil {
		return fail(err)
	}
	return ConsoleInfo{EnvID: env, ReadWrite: names}, nil
}

// Status returns the open console, or nil when there is none.
func (c *Consoles) Status(ctx context.Context) (*ConsoleInfo, error) {
	cur, err := c.current(ctx)
	if err != nil || cur == nil {
		return nil, err
	}
	var names []string
	if ids := cur.Labels[ConsoleRWLabel]; ids != "" {
		for _, id := range strings.Split(ids, ",") {
			if ws, err := c.svc.store.Workspace(ctx, id); err == nil {
				names = append(names, ws.Name)
			}
		}
	}
	return &ConsoleInfo{EnvID: cur.ID, ReadWrite: names, Reused: true}, nil
}

// Close stops and deletes the console environment with its network and
// sidecar. The home volume stays: it holds the human's own dotfiles and shell
// history, and the next console mounts it again. Closing when none is open is
// not an error.
func (c *Consoles) Close(ctx context.Context) error {
	cur, err := c.current(ctx)
	if err != nil || cur == nil {
		return err
	}
	if err := c.svc.rt.Stop(ctx, cur.ID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
		return fmt.Errorf("stop the console %s: %w", cur.ID, err)
	}
	if err := c.svc.rt.Delete(ctx, cur.ID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
		return fmt.Errorf("delete the console %s: %w", cur.ID, err)
	}
	return nil
}

// ShellRequest is a shell to open in the console.
type ShellRequest struct {
	Workspace  string // the workspace to start in, by name or ID; empty for the workspaces' root
	Term       string // the client's TERM, used if it is a plain terminal name
	Cols, Rows uint16
	Actor      string // who asked, for the audit entry
}

// maxShells is how many shells the console serves at once.
const maxShells = 8

// termRe is a TERM value worth passing on: a terminfo name. Anything else is the
// default, because the value comes from the client's environment.
var termRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,39}$`)

// Shell opens a login shell in the open console, as the console's user, in the
// directory of the workspace asked for, with the client's terminal name and size
// and the egress proxy's variables, and records it as an audit entry. The console
// must be open (Open), and the runtime must have terminals. The shell ends when
// the returned terminal is closed or the command ends; the caller pumps it.
func (c *Consoles) Shell(ctx context.Context, req ShellRequest) (runtime.Terminal, error) {
	ta, ok := c.svc.rt.(runtime.TerminalAdapter)
	if !ok {
		return nil, domain.NewConflict(domain.RuleEnvRunning, "this runtime has no terminals, so there is no console shell")
	}
	cur, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.State != domain.EnvRunning {
		return nil, domain.NewConflict(domain.RuleEnvRunning, "the console is not open: open it first")
	}
	dir := "/workspaces"
	if req.Workspace != "" {
		ws, err := c.svc.store.Workspace(ctx, req.Workspace)
		if err != nil {
			return nil, err
		}
		if c.cfg.Dir != nil {
			dir = c.cfg.Dir(ws)
		}
	}
	term := req.Term
	if !termRe.MatchString(term) {
		term = "xterm-256color"
	}
	env := append([]string{
		"HOME=/home/whr", "USER=whr", "LOGNAME=whr", "SHELL=/bin/zsh", "LANG=C.UTF-8", "TERM=" + term, "WHR_CONSOLE=1",
	}, c.svc.agentEnv(ctx, domain.ID(cur.ID))...)

	c.mu.Lock()
	if c.shells >= maxShells {
		c.mu.Unlock()
		return nil, domain.NewConflict(domain.RuleEnvRunning, "the console already serves %d shells: close one first", maxShells)
	}
	c.shells++
	c.mu.Unlock()
	tm, err := ta.Terminal(ctx, cur.ID, runtime.TerminalRequest{Cmd: []string{"/bin/zsh", "-l"}, Env: env, Dir: dir, Cols: req.Cols, Rows: req.Rows})
	if err != nil {
		c.mu.Lock()
		c.shells--
		c.mu.Unlock()
		return nil, err
	}
	var rw []string
	if st, err := c.Status(ctx); err == nil && st != nil {
		rw = st.ReadWrite
	}
	if saved, err := c.svc.store.Append(ctx, domain.NewConsoleEvent(domain.ConsoleOpened{Actor: req.Actor, Workspace: req.Workspace, ReadWrite: rw}, c.svc.clock.Now())); err != nil {
		c.svc.report(fmt.Errorf("audit the console shell: %w", err))
	} else {
		c.svc.publish(saved)
	}
	ct := &countedTerminal{Terminal: tm}
	ct.release = func() {
		c.mu.Lock()
		c.shells--
		delete(c.open, ct)
		c.mu.Unlock()
	}
	c.mu.Lock()
	if c.open == nil {
		c.open = map[*countedTerminal]struct{}{}
	}
	c.open[ct] = struct{}{}
	c.mu.Unlock()
	return ct, nil
}

// CloseShells hangs up every shell that is open: the supervisor is stopping, and
// a shell must not outlive the stream that carries it.
func (c *Consoles) CloseShells() {
	c.mu.Lock()
	shells := make([]*countedTerminal, 0, len(c.open))
	for t := range c.open {
		shells = append(shells, t)
	}
	c.mu.Unlock()
	// In parallel: each close can wait for the guest to end its session (the kill
	// grace), and a supervisor stopping must not wait for them one after another.
	var wg sync.WaitGroup
	for _, t := range shells {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = t.Close()
		}()
	}
	wg.Wait()
}

// countedTerminal gives a shell back to the console's count once, when it is closed.
type countedTerminal struct {
	runtime.Terminal
	release func()
	once    sync.Once
}

func (t *countedTerminal) Close() error {
	err := t.Terminal.Close()
	t.once.Do(t.release)
	return err
}
