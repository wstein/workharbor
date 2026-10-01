package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/store"
)

// WorkspaceMount is where a workspace folder is mounted in its environment.
const WorkspaceMount = "/ws"

// CloneDir is the agent clone's directory inside a workspace folder.
const CloneDir = "repo"

// WorkspaceConfig is what the workspace operations need besides the service.
type WorkspaceConfig struct {
	// Config says where workspaces may live (CheckWorkspacePath).
	Config *config.Config
	// Git seeds the agent clone. It is the only time the host runs git in it.
	Git *hostgit.Git
	// Spec returns the environment's spec for a workspace: image, limits, user,
	// network and egress. The service adds the workspace folder as the bind
	// mount at WorkspaceMount; nothing else comes from the workspace.
	Spec func(w domain.Workspace) runtime.Spec
	// Prepare is runtime.Prepare with this host's filesystem, roots and
	// volume ownership.
	Prepare func(runtime.Spec) (runtime.PreparedSpec, error)
	// NewID returns a fresh ID.
	NewID func() domain.ID
}

// Workspaces creates workspaces and their agents, and starts tasks on agents
// (design D42, issue #90).
type Workspaces struct {
	svc *Service
	cfg WorkspaceConfig
}

// NewWorkspaces returns the workspace operations of a service.
func NewWorkspaces(s *Service, cfg WorkspaceConfig) *Workspaces { return &Workspaces{svc: s, cfg: cfg} }

// CreateRequest describes a workspace and its first agent.
type CreateRequest struct {
	Name        string
	Path        string // an empty folder below a workspace root
	Repo        string // owner/name
	Integration string // main or develop
	// Source is where the agent clone is seeded from: an absolute path of the
	// human's repository or an https URL. Empty means the forge's URL of Repo.
	// The source's .git is read by `git clone` and never mounted.
	Source       string
	Role         string // the first agent
	Instructions string
	Profile      string
}

// Create makes a workspace: it checks the folder, seeds the agent clone, records
// the workspace, provisions and starts its environment and adds the first
// agent with its worktree, which git makes inside the environment, never on the
// host. If a step fails what was made is taken back.
func (w *Workspaces) Create(ctx context.Context, req CreateRequest) (domain.Workspace, domain.Agent, error) {
	now := w.svc.clock.Now()
	path, err := w.cfg.Config.CheckWorkspacePath(req.Path)
	if err != nil {
		return domain.Workspace{}, domain.Agent{}, &domain.InvalidError{Msg: err.Error()}
	}
	ws, wev, err := domain.NewWorkspace(w.cfg.NewID(), req.Name, path, req.Repo, req.Integration, now)
	if err != nil {
		return domain.Workspace{}, domain.Agent{}, err
	}
	ag, aev, err := domain.NewAgent(w.cfg.NewID(), ws.ID, req.Role, req.Instructions, req.Profile, now)
	if err != nil {
		return domain.Workspace{}, domain.Agent{}, err
	}
	origin := "https://github.com/" + ws.Repo + ".git"
	source := req.Source
	if source == "" {
		source = origin
	}

	clone := filepath.Join(path, CloneDir)
	if err := w.cfg.Git.SeedAgentClone(ctx, clone, source, ws.Integration, origin); err != nil {
		return domain.Workspace{}, domain.Agent{}, err
	}
	var undo []func()
	fail := func(err error) (domain.Workspace, domain.Agent, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return domain.Workspace{}, domain.Agent{}, err
	}
	undo = append(undo, func() { _ = os.RemoveAll(clone) })

	if err := w.svc.store.AddWorkspace(ctx, ws, wev); err != nil {
		return fail(err)
	}
	undo = append(undo, func() {
		_ = w.svc.store.RemoveWorkspace(context.WithoutCancel(ctx), ws, domain.RemovedWorkspaceEvent(ws, w.svc.clock.Now()))
	})

	env, err := w.provision(ctx, ws)
	if err != nil {
		return fail(err)
	}
	undo = append(undo, func() {
		bg := context.WithoutCancel(ctx)
		_ = w.svc.rt.Stop(bg, env)
		_ = w.svc.rt.Delete(bg, env)
	})
	if err := w.svc.store.SetWorkspaceEnv(ctx, ws.ID, domain.ID(env)); err != nil {
		return fail(err)
	}
	ws.EnvID = domain.ID(env)

	if err := w.svc.store.AddAgent(ctx, ag, aev); err != nil {
		return fail(err)
	}
	if err := w.addWorktree(ctx, ws, ag); err != nil {
		_ = w.svc.store.RemoveAgent(context.WithoutCancel(ctx), ag, domain.RemovedAgentEvent(ag, w.svc.clock.Now()))
		return fail(err)
	}
	return ws, ag, nil
}

// provision makes the workspace's environment, with the folder mounted
// read-write, and starts it.
func (w *Workspaces) provision(ctx context.Context, ws domain.Workspace) (string, error) {
	spec := w.cfg.Spec(ws)
	spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountBind, Source: ws.Path, Target: WorkspaceMount})
	prep, err := w.cfg.Prepare(spec)
	if err != nil {
		return "", err
	}
	env, err := w.svc.rt.Provision(ctx, prep)
	if err != nil {
		return "", err
	}
	if err := w.svc.rt.Start(ctx, env); err != nil {
		bg := context.WithoutCancel(ctx)
		_ = w.svc.rt.Delete(bg, env)
		return "", fmt.Errorf("start environment %s: %w", env, err)
	}
	if err := w.svc.waitReady(ctx, domain.ID(env)); err != nil {
		bg := context.WithoutCancel(ctx)
		_ = w.svc.rt.Stop(bg, env)
		_ = w.svc.rt.Delete(bg, env)
		return "", err
	}
	return env, nil
}

// addWorktree makes the agent's worktree and branch inside the environment. The
// host never runs git in a workspace's clone after it was seeded (§4.5).
func (w *Workspaces) addWorktree(ctx context.Context, ws domain.Workspace, a domain.Agent) error {
	clone := WorkspaceMount + "/" + CloneDir
	// safe.directory: the clone belongs to the host user, the guest runs as another.
	env := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0=*"}
	out, code, err := w.svc.exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", clone, "worktree", "add", "-b", a.Branch, a.Worktree, ws.Integration},
		Env: env,
	})
	if err != nil {
		return fmt.Errorf("add worktree for agent %s: %w", a.Role, err)
	}
	if code != 0 {
		return fmt.Errorf("add worktree for agent %s: git exited %d: %s", a.Role, code, strings.TrimSpace(out))
	}
	return nil
}

// AddAgent adds a named agent to a workspace: a record, and its worktree and
// branch inside the environment, which must be running.
func (w *Workspaces) AddAgent(ctx context.Context, workspace, role, instructions, profile string) (domain.Agent, error) {
	ws, err := w.svc.store.Workspace(ctx, workspace)
	if err != nil {
		return domain.Agent{}, err
	}
	if ws.EnvID == "" {
		return domain.Agent{}, domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	a, ev, err := domain.NewAgent(w.cfg.NewID(), ws.ID, role, instructions, profile, w.svc.clock.Now())
	if err != nil {
		return domain.Agent{}, err
	}
	if err := w.svc.store.AddAgent(ctx, a, ev); err != nil {
		return domain.Agent{}, err
	}
	if err := w.addWorktree(ctx, ws, a); err != nil {
		_ = w.svc.store.RemoveAgent(context.WithoutCancel(ctx), a, domain.RemovedAgentEvent(a, w.svc.clock.Now()))
		return domain.Agent{}, err
	}
	return a, nil
}

// StartRequest starts a task on an agent.
type StartRequest struct {
	AgentID domain.ID
	Issue   string // the issue the task works on
	Prompt  string // the first message to the agent
}

// StartTask creates a task assigned to an agent and starts a run of it in the
// workspace's environment. The environment holds one live run at most
// (design §4.3): the check and the save are one transaction, so two starts
// cannot both pass it. The agent starts in its worktree, under the launcher
// that can cancel its process group (D25), with the session recorded by the
// run. A start that stops half-way leaves a starting run with no session, which
// the reconciler fails into a retry-or-cancel Decision.
func (w *Workspaces) StartTask(ctx context.Context, req StartRequest) (domain.ID, domain.ID, error) {
	a, err := w.svc.store.Agent(ctx, req.AgentID)
	if err != nil {
		return "", "", err
	}
	ws, err := w.svc.store.Workspace(ctx, string(a.WorkspaceID))
	if err != nil {
		return "", "", err
	}
	if ws.EnvID == "" {
		return "", "", domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	info, err := w.svc.rt.Inspect(ctx, string(ws.EnvID))
	if err != nil {
		return "", "", err
	}
	if info.State != domain.EnvRunning {
		if err := w.svc.rt.Start(ctx, string(ws.EnvID)); err != nil {
			return "", "", fmt.Errorf("start environment %s: %w", ws.EnvID, err)
		}
	}
	if err := w.svc.waitReady(ctx, ws.EnvID); err != nil {
		return "", "", err
	}

	task, run := w.cfg.NewID(), w.cfg.NewID()
	agg := domain.NewTaskAggregate(domain.Task{
		ID: task, Repo: ws.Repo, Issue: req.Issue, State: domain.TaskQueued, AgentID: a.ID, CreatedAt: w.svc.clock.Now(),
	})
	agg.AddEnvironment(domain.Environment{ID: ws.EnvID, Backend: w.svc.rt.Name(), State: domain.EnvRunning})
	if err := agg.StartRun(domain.Run{ID: run, WorkspaceID: ws.ID, AgentID: a.ID, EnvID: ws.EnvID}); err != nil {
		return "", "", err
	}
	sl := w.svc.begin(run) // taken before the run is visible, so the reconciler does not take it for lost
	err = w.svc.store.Update(ctx, func(tx *store.Tx) error {
		live, err := tx.LiveRuns(ctx, ws.EnvID)
		if err != nil {
			return err
		}
		if err := domain.CheckEnvironmentFree(ws.EnvID, live); err != nil {
			return err
		}
		if _, err := tx.SaveTask(ctx, agg); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		w.svc.end(run, sl)
		return "", "", err
	}

	spec := w.svc.cfg.Spec(agg.Task(), domain.Run{ID: run, WorkspaceID: ws.ID, AgentID: a.ID, EnvID: ws.EnvID})
	spec.EnvID, spec.Workdir = string(ws.EnvID), a.Worktree
	if req.Prompt != "" {
		spec.Prompt = req.Prompt
	}
	sess, err := w.svc.ag.Start(ctx, spec)
	if err != nil {
		w.svc.end(run, sl)
		var rep Report
		if ferr := w.svc.failRun(context.WithoutCancel(ctx), task, run, &rep); ferr != nil {
			return "", "", errors.Join(err, ferr)
		}
		return "", "", err
	}
	if err := w.svc.update(ctx, task, func(t *domain.TaskAggregate) error { return t.MarkRunning(run) }); err != nil {
		_ = sess.Stop(ctx)
		w.svc.end(run, sl)
		return "", "", err
	}
	w.svc.attach(task, run, sl, sess)
	return task, run, nil
}

// exec runs a command in an environment and returns its combined output and
// exit code.
func (s *Service) exec(ctx context.Context, env string, req runtime.ExecRequest) (string, int, error) {
	st, err := s.rt.Exec(ctx, env, req)
	if err != nil {
		return "", 0, err
	}
	stdout, stderr, code, err := runtime.Collect(st)
	return string(stdout) + string(stderr), code, err
}

// Rebase rebases an agent's branch onto the workspace's integration branch,
// inside the environment (design §4.5). It is refused while the agent has a
// starting or running run, which is editing the worktree. On a conflict the
// rebase is aborted, so the worktree is as it was, and the conflict is
// returned with git's report: the caller (the export, #91) turns it into a
// Decision on the task, which a conflict outside a live run has no run to
// raise from.
func (w *Workspaces) Rebase(ctx context.Context, agentID domain.ID) error {
	a, err := w.svc.store.Agent(ctx, agentID)
	if err != nil {
		return err
	}
	ws, err := w.svc.store.Workspace(ctx, string(a.WorkspaceID))
	if err != nil {
		return err
	}
	if ws.EnvID == "" {
		return domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	live, err := w.svc.store.LiveRuns(ctx, ws.EnvID)
	if err != nil {
		return err
	}
	for _, r := range live {
		if r.AgentID == a.ID && (r.State == domain.RunStarting || r.State == domain.RunRunning) {
			return domain.NewConflict(domain.RuleAgentActive, "agent %s has run %s (%s): pause or finish it before a rebase", a.Role, r.ID, r.State)
		}
	}
	env := []string{
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0=*",
		// a rebase writes commits, which need a committer; the authors are kept
		"GIT_COMMITTER_NAME=workharbor agent " + a.Role, "GIT_COMMITTER_EMAIL=agent@workharbor.invalid",
	}
	out, code, err := w.svc.exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "rebase", ws.Integration}, Env: env,
	})
	if err != nil {
		return fmt.Errorf("rebase agent %s: %w", a.Role, err)
	}
	if code == 0 {
		return nil
	}
	// Leave the worktree as it was; a failed abort is reported with the conflict.
	_, acode, aerr := w.svc.exec(context.WithoutCancel(ctx), string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "rebase", "--abort"}, Env: env,
	})
	msg := fmt.Sprintf("agent/%s does not rebase onto %s: %s", a.Role, ws.Integration, strings.TrimSpace(out))
	if aerr != nil || acode != 0 {
		msg += fmt.Sprintf(" (and `rebase --abort` failed: exit %d, %v)", acode, aerr)
	}
	return domain.NewConflict(domain.RuleRebase, "%s", msg)
}
