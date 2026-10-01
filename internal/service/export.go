package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Errors of exporting an agent's branch.
var (
	// ErrNothingToExport means the branch has no commits beyond its base.
	ErrNothingToExport = errors.New("the agent's branch has no commits beyond its base")
	// ErrRebaseBlocked means the branch did not rebase before the export. A
	// question (design §4.2) was raised for the task's stopped run.
	ErrRebaseBlocked = errors.New("the agent's branch does not rebase: a question was raised")
	// ErrNoWorkspaces means the publisher has no workspace operations, which an
	// export from an environment needs.
	ErrNoWorkspaces = errors.New("the publisher has no workspace operations")
)

const (
	// DefaultMaxBundle is the most bytes of a bundle the host takes in.
	DefaultMaxBundle = 512 << 20
	// DefaultExportTimeout bounds the guest's `git bundle create`.
	DefaultExportTimeout = 5 * time.Minute
)

var commitRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// exportBranch brings an agent's branch out of its environment into the
// supervisor's repository (design D42, §4.5): optionally rebased onto the
// integration branch first, then `git bundle create -` runs in the guest, under
// the launcher's reach and a timeout, and its output is imported as a stream
// with hostgit. The host never runs git in the workspace, and the environment
// does not have to stop. The target and its history are in the repository
// already, because the bundle's prerequisite is the branch's merge base with
// the integration branch.
//
// A rebase that conflicts raises a blocking question for the task's stopped
// run and returns ErrRebaseBlocked.
func (p *Publisher) exportBranch(ctx context.Context, req Request, run domain.ID, rebase bool) error {
	w := p.cfg.Workspaces
	if w == nil {
		return ErrNoWorkspaces
	}
	if req.Agent == "" {
		return &domain.InvalidError{Msg: "a request needs an agent: the branch leaves its environment as a bundle"}
	}
	a, err := p.svc.store.Agent(ctx, req.Agent)
	if err != nil {
		return err
	}
	if req.Branch != a.Branch {
		return fmt.Errorf("branch %q is not agent %s's branch %q", req.Branch, a.Role, a.Branch)
	}
	ws, err := p.svc.store.Workspace(ctx, string(a.WorkspaceID))
	if err != nil {
		return err
	}
	if ws.EnvID == "" {
		return domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	info, err := p.svc.rt.Inspect(ctx, string(ws.EnvID))
	if err != nil {
		return err
	}
	if info.State != domain.EnvRunning { // exec needs it up; a stopped one is started
		if err := p.svc.rt.Start(ctx, string(ws.EnvID)); err != nil {
			return fmt.Errorf("start environment %s: %w", ws.EnvID, err)
		}
		if err := p.svc.waitReady(ctx, ws.EnvID); err != nil {
			return err
		}
	}

	if rebase {
		if err := w.Rebase(ctx, a.ID); err != nil {
			var re *RebaseError
			if errors.As(err, &re) {
				if rerr := p.raiseRebaseConflict(ctx, req.Task, run, ws.Integration, re.Paths); rerr != nil {
					return errors.Join(err, rerr)
				}
				return fmt.Errorf("%w: %w", ErrRebaseBlocked, err)
			}
			return err
		}
	}

	out, code, err := p.svc.exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "merge-base", ws.Integration, a.Branch}, Env: gitEnv(),
	})
	if err != nil {
		return err
	}
	base := strings.TrimSpace(out)
	if code != 0 || !commitRe.MatchString(base) {
		return fmt.Errorf("the merge base of %s and %s could not be found (exit %d): %s", ws.Integration, a.Branch, code, oneLine(out))
	}
	return p.streamBundle(ctx, ws, a, base)
}

// streamBundle runs `git bundle create -` in the guest and imports its output.
func (p *Publisher) streamBundle(ctx context.Context, ws domain.Workspace, a domain.Agent, base string) error {
	timeout := p.cfg.ExportTimeout
	if timeout <= 0 {
		timeout = DefaultExportTimeout
	}
	limit := p.cfg.MaxBundle
	if limit <= 0 {
		limit = DefaultMaxBundle
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	st, err := p.svc.rt.Exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "bundle", "create", "-", base + ".." + a.Branch}, Env: gitEnv(),
	})
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		for c := range st.Chunks() {
			switch c.Stream {
			case runtime.Stdout:
				_, _ = pw.Write(c.Data) // a reader that has gone leaves the loop draining
			case runtime.Stderr:
				if stderr.Len() < 4096 {
					stderr.Write(c.Data)
				}
			}
		}
		code, werr := st.Wait()
		var ferr error
		switch {
		case werr != nil:
			ferr = fmt.Errorf("git bundle create: %w", werr)
		case code != 0:
			ferr = fmt.Errorf("git bundle create exited %d: %s", code, oneLine(stderr.String()))
		}
		pw.CloseWithError(ferr) // nil is a plain EOF
		done <- ferr
	}()

	_, ierr := p.cfg.Repo.ImportBundle(ctx, a.Branch, pr, limit)
	_ = pr.Close() // unblocks the writer if the import stopped early
	cancel()       // and ends the guest command
	gerr := <-done
	switch {
	case ierr == nil:
		return nil
	case strings.Contains(stderr.String(), "empty bundle"):
		return ErrNothingToExport
	case gerr != nil && !errors.Is(ierr, gerr):
		return errors.Join(ierr, gerr)
	}
	return ierr
}

// raiseRebaseConflict asks the human what to do about a branch that does not
// rebase, as a blocking question for the task's stopped run (design §4.2).
func (p *Publisher) raiseRebaseConflict(ctx context.Context, task, run domain.ID, target string, paths []string) error {
	return p.svc.update(ctx, task, func(a *domain.TaskAggregate) error {
		_, err := a.RaiseRebaseConflict(run, p.svc.cfg.NewID(), target, paths, p.svc.clock.Now())
		return err
	})
}
