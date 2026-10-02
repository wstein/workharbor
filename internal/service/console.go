package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
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
}

// Consoles opens and closes the console environment of design D43: an
// environment without an agent, for the human's shell work across workspaces.
type Consoles struct {
	svc *Service
	cfg ConsoleConfig
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
