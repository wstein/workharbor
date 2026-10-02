package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// RebuildResult says what a rebuild replaced.
type RebuildResult struct {
	OldEnv, NewEnv       string
	OldImage, NewImage   string
	OldDigest, NewDigest string
}

// Rebuild replaces a workspace's environment with one made from the image its
// repository's default branch resolves to now (issue #128): an allowed feature
// source, a new devcontainer commit or a bumped base image reaches a long-lived
// workspace only this way. The home and build volumes, the workspace folder and
// so every worktree and branch stay; the container, its network and its egress
// sidecar are new, with the allowed hosts as at provision.
//
// It is refused while any run of the workspace is unfinished (an interrupted
// one is, too: the reconciler would resume it in the old environment), and never started by
// itself. The new image is built and the new spec checked first, so a repository
// whose image cannot be built costs nothing. Then the old environment is stopped,
// because a volume is held by one running environment at a time, the new one is
// provisioned beside it on a network of its own name, started and checked, and the
// record is switched in one change that also writes the audit entry. The old
// environment, its network and sidecar are removed only after that; if the new one
// cannot be brought up or the record cannot be switched, the new one is taken back
// and the old one is started again.
func (w *Workspaces) Rebuild(ctx context.Context, workspace, actor string) (RebuildResult, error) {
	ws, err := w.svc.store.Workspace(ctx, workspace)
	if err != nil {
		return RebuildResult{}, err
	}
	if ws.EnvID == "" {
		return RebuildResult{}, domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment to rebuild", ws.Name)
	}
	ws, err = w.beginRebuild(ctx, ws)
	if err != nil {
		return RebuildResult{}, err
	}
	defer w.endRebuild(ws)

	old := string(ws.EnvID)
	res := RebuildResult{OldEnv: old}
	oldInfo, oldErr := w.svc.rt.Inspect(ctx, old)
	switch {
	case oldErr == nil:
		res.OldImage, res.OldDigest = oldInfo.Image, oldInfo.ImageDigest
	case !errors.Is(oldErr, runtime.ErrNotFound):
		return RebuildResult{}, fmt.Errorf("inspect environment %s: %w", old, oldErr)
	}

	prep, err := w.prepareSpec(ctx, ws, "-"+networkSuffix(w.cfg.NewID()))
	if err != nil {
		return RebuildResult{}, err
	}
	res.NewImage = prep.Spec().Image

	bg := context.WithoutCancel(ctx) // taking back what was done does not stop because the caller left
	wasRunning := oldErr == nil && oldInfo.State == domain.EnvRunning
	if oldErr == nil {
		if err := w.svc.rt.Stop(ctx, old); err != nil {
			return RebuildResult{}, fmt.Errorf("stop environment %s: %w", old, err)
		}
	}
	restore := func(cause error) (RebuildResult, error) {
		if oldErr == nil && wasRunning {
			if err := w.svc.rt.Start(bg, old); err != nil {
				return RebuildResult{}, fmt.Errorf("%w (and the old environment %s could not be started again: %w)", cause, old, err)
			}
		}
		return RebuildResult{}, fmt.Errorf("%w (the old environment %s is as it was)", cause, old)
	}

	newEnv, err := w.bringUp(ctx, prep, true)
	if err != nil {
		return restore(err)
	}
	res.NewEnv = newEnv
	if info, err := w.svc.rt.Inspect(ctx, newEnv); err == nil {
		res.NewDigest = info.ImageDigest
	}
	ev := domain.NewWorkspaceRebuiltEvent(domain.WorkspaceRebuilt{
		ID: ws.ID, Name: ws.Name, Actor: actor, OldEnv: domain.ID(old), NewEnv: domain.ID(newEnv),
		OldImage: res.OldImage, NewImage: res.NewImage, OldDigest: res.OldDigest, NewDigest: res.NewDigest,
	}, w.svc.clock.Now())
	if err := w.svc.store.SwapWorkspaceEnv(ctx, ws.ID, ws.EnvID, domain.ID(newEnv), ev); err != nil {
		_ = w.svc.rt.Stop(bg, newEnv)
		_ = w.svc.rt.Delete(bg, newEnv)
		return restore(fmt.Errorf("record the new environment: %w", err))
	}
	w.svc.publish([]domain.Event{ev})
	if oldErr == nil {
		// Only now: the old container, its network and its sidecar. A failure leaves
		// them behind, reported; the workspace already runs in the new one.
		if err := w.svc.rt.Delete(bg, old); err != nil {
			w.svc.report(fmt.Errorf("remove the old environment %s of workspace %s after the rebuild: %w", old, ws.Name, err))
		}
	}
	return res, nil
}

// beginRebuild refuses a workspace with an unfinished run (live or interrupted),
// naming it, and one on whose environment an operation holds a lease (it is
// starting or using it), and marks the workspace as being rebuilt, under the lock
// a run's start takes. The record is read again inside that lock and the fresh one
// returned: a caller that loaded it before another rebuild switched the
// environment must not rebuild, or swap from, the old one.
func (w *Workspaces) beginRebuild(ctx context.Context, ws domain.Workspace) (domain.Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.svc.rebuilding(ws.ID) {
		return ws, domain.NewConflict(domain.RuleEnvRunning, "workspace %s is already being rebuilt", ws.Name)
	}
	ws, err := w.svc.store.Workspace(ctx, string(ws.ID))
	if err != nil {
		return ws, err
	}
	if ws.EnvID == "" {
		return ws, domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment to rebuild", ws.Name)
	}
	open, err := w.svc.store.UnfinishedRuns(ctx, ws.EnvID)
	if err != nil {
		return ws, err
	}
	if len(open) > 0 {
		r := open[0]
		return ws, domain.NewConflict(domain.RuleAgentActive, "workspace %s has run %s (%s) of agent %s: finish, stop or fail it before a rebuild", ws.Name, r.ID, r.State, r.AgentID)
	}
	marked, leased := w.svc.markRebuilding(ws.ID)
	if leased {
		return ws, domain.NewConflict(domain.RuleEnvRunning, "workspace %s: an operation is starting or using the environment: try the rebuild again when it is done", ws.Name)
	}
	if !marked {
		return ws, domain.NewConflict(domain.RuleEnvRunning, "workspace %s is already being rebuilt", ws.Name)
	}
	return ws, nil
}

func (w *Workspaces) endRebuild(ws domain.Workspace) {
	w.svc.unmarkRebuilding(ws.ID)
}

// networkSuffix makes an ID into a few characters a network name may hold.
func networkSuffix(id domain.ID) string {
	var b strings.Builder
	for _, r := range strings.ToLower(string(id)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		s = "r"
	}
	if len(s) > 12 {
		s = s[len(s)-12:]
	}
	return s
}
