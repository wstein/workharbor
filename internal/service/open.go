package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/hostgit"
)

// ErrNoEditorCopies is returned when the supervisor was not set up to make
// editor copies (no place for them, or no way to open the repository's mirror).
var ErrNoEditorCopies = errors.New("editor copies are not configured on this supervisor")

// OpenCopy is `whr open` (design §4.5, issue #59): it exports the agent's
// branch out of its environment, which keeps running, into the supervisor's own
// repository and clones it into a copy the developer's editor can open. The copy
// is never the agent's checkout, has no hooks and none of the agent's config,
// and a later call refreshes it by fast-forward without overwriting edits. An
// empty role means the workspace's only agent.
func (w *Workspaces) OpenCopy(ctx context.Context, workspace, role string) (EditorCopy, error) {
	if w.cfg.Topics == nil || w.cfg.EditorDir == "" {
		return EditorCopy{}, ErrNoEditorCopies
	}
	ws, err := w.svc.store.Workspace(ctx, workspace)
	if err != nil {
		return EditorCopy{}, err
	}
	a, err := w.agentOf(ctx, ws, role)
	if err != nil {
		return EditorCopy{}, err
	}
	repo, cache, err := w.cfg.Topics(ctx, ws.Repo)
	if err != nil {
		return EditorCopy{}, fmt.Errorf("open the repository %s: %w", ws.Repo, err)
	}
	pub := NewPublisher(w.svc, PublishConfig{Repo: repo, Cache: cache, Workspaces: w})
	// Names and roles are lowercase letters, digits and "-", so "." cannot occur in
	// either: workspace a-b with role c and workspace a with role b-c never share a copy.
	dest := filepath.Join(w.cfg.EditorDir, ws.Name+"."+a.Role)
	return pub.OpenCopy(ctx, Request{Agent: a.ID, Branch: a.Branch, Target: ws.Integration}, dest)
}

// agentOf finds an agent of a workspace by role; an empty role is allowed when
// the workspace has exactly one.
func (w *Workspaces) agentOf(ctx context.Context, ws domain.Workspace, role string) (domain.Agent, error) {
	if role != "" {
		return w.svc.store.AgentByRole(ctx, ws.ID, role)
	}
	agents, err := w.svc.store.Agents(ctx, ws.ID)
	if err != nil {
		return domain.Agent{}, err
	}
	switch len(agents) {
	case 1:
		return agents[0], nil
	case 0:
		return domain.Agent{}, &domain.NotFoundError{Kind: "agent", ID: ws.Name}
	}
	roles := make([]string, len(agents))
	for i, a := range agents {
		roles[i] = a.Role
	}
	return domain.Agent{}, &domain.InvalidError{Msg: fmt.Sprintf("workspace %s has several agents (%s): name one", ws.Name, strings.Join(roles, ", "))}
}

// TopicsFunc opens the supervisor's own repository and the mirror of a forge
// repository (design D42, §4.5), where an agent's branch is imported.
type TopicsFunc func(ctx context.Context, repo string) (*hostgit.Repo, *hostgit.Cache, error)
