package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/policy"
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
	// Issues loads an issue from the forge. Optional: Run needs it.
	Issues IssueSource
	// Topics and EditorDir make `whr open` work: Topics opens the supervisor's
	// own repository and the mirror of a forge repository, and EditorDir is the
	// directory the editor copies are made in, outside every workspace root.
	// Optional.
	Topics    TopicsFunc
	EditorDir string
	// Environment reads the repository's environment from its default branch, in
	// the supervisor's own copy, never from a checkout (D38): the hosts its
	// devcontainer.json requests and its lockfiles suggest are asked about at a
	// run's start. Optional: without it nothing is asked.
	Environment func(ctx context.Context, repo, branch string) (devcontainer.Environment, error)
	// Workflow returns the preset a repository runs under (D47); a task keeps the
	// one it started under. Nil means the default preset.
	Workflow func(repo string) string
	// Trust is the trust tier of issue #53: it says whether an issue may start
	// a run. Nil allows every issue, today. It is the place to add the tiers,
	// not a way around them.
	Trust func(forge.Issue) error
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
		// A volume outlives its environment (design §4.4), so the home volume
		// this call made is removed here, or a failed Create would leak it (found
		// by the serve integration run).
		res, _ := w.svc.rt.Resources(bg, env)
		_ = w.svc.rt.Stop(bg, env)
		_ = w.svc.rt.Delete(bg, env)
		for _, v := range res.Volumes {
			_ = w.svc.rt.RemoveVolume(bg, v)
		}
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
	if spec.Egress != nil {
		// A repository's allowed hosts are in the sidecar from the start, so no
		// other workspace of it is asked again.
		allowed, err := w.svc.EgressAllow(ctx, ws.Repo)
		if err != nil {
			return "", err
		}
		egress := *spec.Egress
		egress.Allow = unionHosts(egress.Allow, allowed)
		spec.Egress = &egress
	}
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
	if err := w.checkGit(ctx, env, spec.Image); err != nil {
		// Like Create's own undo: a volume outlives its environment (§4.4).
		bg := context.WithoutCancel(ctx)
		res, _ := w.svc.rt.Resources(bg, env)
		_ = w.svc.rt.Stop(bg, env)
		_ = w.svc.rt.Delete(bg, env)
		for _, v := range res.Volumes {
			_ = w.svc.rt.RemoveVolume(bg, v)
		}
		return "", err
	}
	return env, nil
}

// checkGit refuses an environment whose image has no git (design D44). The
// agents' worktrees and the bundle export run git inside the environment, so an
// image without it would fail later in `git worktree add` with an exec error;
// this says what is missing, when the workspace is created. The image may be
// the workharbor base image, which has git, or a repository's own.
func (w *Workspaces) checkGit(ctx context.Context, env, image string) error {
	out, code, err := w.svc.exec(ctx, env, runtime.ExecRequest{Cmd: []string{"git", "--version"}, Env: gitEnv()})
	if err != nil {
		return fmt.Errorf("check git in environment %s: %w", env, err)
	}
	if code == 0 {
		return nil
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return &domain.InvalidError{Msg: fmt.Sprintf(
		"the environment image %q has no git (`git --version` exited %d: %q): agents need git for their worktrees and for the bundle export, so use an image that has it, or leave environment.image unset for the workharbor base image (D44)",
		image, code, strings.TrimSpace(out))}
}

// addWorktree makes the agent's worktree and branch inside the environment. The
// host never runs git in a workspace's clone after it was seeded (§4.5).
func (w *Workspaces) addWorktree(ctx context.Context, ws domain.Workspace, a domain.Agent) error {
	clone := WorkspaceMount + "/" + CloneDir
	out, code, err := w.svc.exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", clone, "worktree", "add", "-b", a.Branch, a.Worktree, ws.Integration},
		Env: gitEnv(),
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
	a, ws, err := w.agentAndWorkspace(ctx, req.AgentID)
	if err != nil {
		return "", "", err
	}
	if err := w.ensureEnvironment(ctx, ws); err != nil {
		return "", "", err
	}
	task, run := w.cfg.NewID(), w.cfg.NewID()
	agg := domain.NewTaskAggregate(domain.Task{
		ID: task, Repo: ws.Repo, Issue: req.Issue, State: domain.TaskQueued, AgentID: a.ID, Workflow: w.workflowOf(ws.Repo), CreatedAt: w.svc.clock.Now(),
	})
	agg.AddEnvironment(domain.Environment{ID: ws.EnvID, Backend: w.svc.rt.Name(), State: domain.EnvRunning})
	if err := w.launch(ctx, agg, ws, a, run, req.Prompt); err != nil {
		return "", "", err
	}
	return task, run, nil
}

// agentAndWorkspace loads an agent and its workspace, which must have an
// environment.
func (w *Workspaces) agentAndWorkspace(ctx context.Context, agentID domain.ID) (domain.Agent, domain.Workspace, error) {
	a, err := w.svc.store.Agent(ctx, agentID)
	if err != nil {
		return domain.Agent{}, domain.Workspace{}, err
	}
	ws, err := w.svc.store.Workspace(ctx, string(a.WorkspaceID))
	if err != nil {
		return domain.Agent{}, domain.Workspace{}, err
	}
	if ws.EnvID == "" {
		return domain.Agent{}, domain.Workspace{}, domain.NewConflict(domain.RuleEnvRunning, "workspace %s has no environment", ws.Name)
	}
	return a, ws, nil
}

// ensureEnvironment starts the workspace's environment if it is not running and
// waits until exec answers.
func (w *Workspaces) ensureEnvironment(ctx context.Context, ws domain.Workspace) error {
	info, err := w.svc.rt.Inspect(ctx, string(ws.EnvID))
	if err != nil {
		return err
	}
	if info.State != domain.EnvRunning {
		if err := w.svc.rt.Start(ctx, string(ws.EnvID)); err != nil {
			return fmt.Errorf("start environment %s: %w", ws.EnvID, err)
		}
	}
	return w.svc.waitReady(ctx, ws.EnvID)
}

// launch starts a run on a task aggregate that has none live: the run is added,
// saved with the one-run check in one transaction, and the agent is started in
// its worktree and attached. If the agent cannot be started the run fails into
// a retry-or-cancel Decision, so the environment is free again.
func (w *Workspaces) launch(ctx context.Context, agg *domain.TaskAggregate, ws domain.Workspace, a domain.Agent, run domain.ID, prompt string) error {
	r := domain.Run{ID: run, WorkspaceID: ws.ID, AgentID: a.ID, EnvID: ws.EnvID}
	if err := agg.StartRun(r); err != nil {
		return err
	}
	task := agg.Task().ID
	var saved []domain.Event
	sl := w.svc.begin(run) // taken before the run is visible, so the reconciler does not take it for lost
	err := w.svc.store.Update(ctx, func(tx *store.Tx) error {
		live, err := tx.LiveRuns(ctx, ws.EnvID)
		if err != nil {
			return err
		}
		if err := domain.CheckEnvironmentFree(ws.EnvID, live); err != nil {
			return err
		}
		saved, err = tx.SaveTask(ctx, agg)
		return err
	})
	if err != nil {
		w.svc.end(run, sl)
		return err
	}
	w.svc.publish(saved)

	start := func(ctx context.Context) error { return w.startAgent(ctx, task, run, ws, a, prompt, sl) }
	waiting, err := w.gateEgress(ctx, task, run, ws, start, sl)
	if err != nil {
		w.svc.end(run, sl)
		var rep Report
		return errors.Join(err, w.svc.failRun(context.WithoutCancel(ctx), task, run, &rep))
	}
	if waiting {
		return nil // the run stays starting; the answers start the agent
	}
	return start(ctx)
}

// gateEgress asks the human about the hosts the repository requests or suggests
// and has not answered yet (design §4.2): one blocking approval per host, raised
// for the run, which stays starting. It returns true when it did, and the agent
// starts when the last request is answered (continueEgress). A repository that
// cannot be read right now does not stop the run: it starts with the hosts
// already allowed, and the failure is reported, since no host is ever allowed
// by what could not be read.
func (w *Workspaces) gateEgress(ctx context.Context, task, run domain.ID, ws domain.Workspace, start func(context.Context) error, sl *slot) (bool, error) {
	if w.cfg.Environment == nil {
		return false, nil
	}
	env, err := w.cfg.Environment(ctx, ws.Repo, ws.Integration)
	if err != nil {
		w.svc.report(fmt.Errorf("egress requests of %s: %w", ws.Repo, err))
		return false, nil
	}
	pending, err := w.svc.PendingEgress(ctx, ws.Repo, env)
	if err != nil {
		return false, err
	}
	if len(pending) == 0 {
		return false, nil
	}
	w.svc.holdForEgress(run, &egressWait{task: task, finish: start, sl: sl}) // before the requests are visible, so an answer finds it
	if _, err := w.svc.RequestEgress(ctx, task, run, pending); err != nil {
		w.svc.takeEgressWait(run)
		return false, err
	}
	return true, nil
}

// applyEgress makes the environment's proxy sidecar allow what the supervisor's
// configuration allows and the human allowed for the repository, if it does not
// already (design §4.2). It runs only before an agent process starts, because the
// sidecar's address changes. A runtime that cannot change the allowlist leaves it.
func (w *Workspaces) applyEgress(ctx context.Context, ws domain.Workspace) error {
	up, ok := w.svc.rt.(runtime.EgressUpdater)
	if !ok {
		return nil
	}
	spec := w.cfg.Spec(ws)
	if spec.Egress == nil {
		return nil
	}
	allowed, err := w.svc.EgressAllow(ctx, ws.Repo)
	if err != nil {
		return err
	}
	want := unionHosts(spec.Egress.Allow, allowed)
	info, err := w.svc.rt.Inspect(ctx, string(ws.EnvID))
	if err != nil {
		return err
	}
	if slices.Equal(info.EgressAllow, want) {
		return nil
	}
	egress := *spec.Egress
	egress.Allow = want
	spec.Egress = &egress
	// The spec names the environment's own network, as the runtime says it is.
	res, err := w.svc.rt.Resources(ctx, string(ws.EnvID))
	if err != nil {
		return err
	}
	spec.Network.Name = res.Network
	spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountBind, Source: ws.Path, Target: WorkspaceMount})
	prep, err := w.cfg.Prepare(spec)
	if err != nil {
		return err
	}
	return up.UpdateEgress(ctx, string(ws.EnvID), prep)
}

// unionHosts returns the sorted union of two host lists without duplicates.
func unionHosts(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range append(append([]string(nil), a...), b...) {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

// startAgent starts the agent of a run that is starting, after the allowlist was
// brought up to date. It is the second half of launch: the run is saved, and the
// egress requests, if any, are answered.
func (w *Workspaces) startAgent(ctx context.Context, task, run domain.ID, ws domain.Workspace, a domain.Agent, prompt string, sl *slot) error {
	agg, err := w.svc.store.LoadTask(ctx, task)
	if err != nil {
		w.svc.end(run, sl)
		return err
	}
	r, ok := agg.Run(run)
	if !ok || r.State != domain.RunStarting {
		w.svc.end(run, sl) // cancelled while it waited
		return nil
	}
	if err := w.applyEgress(ctx, ws); err != nil {
		w.svc.end(run, sl)
		var rep Report
		return errors.Join(fmt.Errorf("apply the egress allowlist: %w", err), w.svc.failRun(context.WithoutCancel(ctx), task, run, &rep))
	}
	spec := w.svc.cfg.Spec(agg.Task(), r)
	w.svc.fillApprover(&spec, task, run)
	spec.EnvID, spec.Workdir = string(ws.EnvID), a.Worktree
	if prompt != "" {
		spec.Prompt = prompt
	}
	spec.Env = append(spec.Env, w.svc.agentEnv(ctx, ws.EnvID)...)
	// The session outlives the request that starts it (the API request returns
	// at once; the agent runs for hours). Shutdown stops it.
	sess, err := w.svc.ag.Start(context.WithoutCancel(ctx), spec)
	if err != nil {
		w.svc.end(run, sl)
		var rep Report
		if ferr := w.svc.failRun(context.WithoutCancel(ctx), task, run, &rep); ferr != nil {
			return errors.Join(err, ferr)
		}
		return err
	}
	if err := w.svc.update(ctx, task, func(t *domain.TaskAggregate) error { return t.MarkRunning(run) }); err != nil {
		_ = sess.Stop(ctx)
		w.svc.end(run, sl)
		return err
	}
	w.svc.attach(task, run, sl, sess)
	return nil
}

// NewRun starts a new run on an existing task (design §5.3): rework after
// changes were requested, and after the answers of the failed-run and
// rebase-conflict questions. It checks the one-run rule like StartTask. The
// agent starts with the briefing of D27 for a new run; notes are untrusted data
// the caller wants in it, such as the paths of a rebase conflict.
func (w *Workspaces) NewRun(ctx context.Context, task domain.ID, prompt, notes string) (domain.ID, error) {
	agg, err := w.svc.store.LoadTask(ctx, task)
	if err != nil {
		return "", err
	}
	if agg.Task().AgentID == "" {
		return "", domain.NewConflict(domain.RuleRunAgent, "task %s has no agent: it was made before agents existed", task)
	}
	a, ws, err := w.agentAndWorkspace(ctx, agg.Task().AgentID)
	if err != nil {
		return "", err
	}
	if err := w.ensureEnvironment(ctx, ws); err != nil {
		return "", err
	}
	if env, ok := agg.Environment(ws.EnvID); !ok {
		agg.AddEnvironment(domain.Environment{ID: ws.EnvID, Backend: w.svc.rt.Name(), State: domain.EnvRunning})
	} else if env.State != domain.EnvRunning {
		if err := agg.ObserveEnv(ws.EnvID, domain.EnvRunning); err != nil {
			return "", err
		}
	}
	run := w.cfg.NewID()
	next := domain.Run{ID: run, AgentID: a.ID, WorkspaceID: ws.ID}
	if err := w.launch(ctx, agg, ws, a, run, NewRunBriefing(agg.Task(), next, prompt, notes)); err != nil {
		return "", err
	}
	return run, nil
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

// RebaseError is a rebase that conflicts: the conflict rule, git's report and
// the conflicting paths, which are the agent's data and untrusted.
type RebaseError struct {
	conflict *domain.ConflictError
	Paths    []string
}

func (e *RebaseError) Error() string { return e.conflict.Error() }

// Unwrap lets errors.As find the domain conflict (rule rebase-conflict).
func (e *RebaseError) Unwrap() error { return e.conflict }

// Rebase rebases an agent's branch onto the workspace's integration branch,
// inside the environment (design §4.5). It is refused while the agent has a
// starting or running run, which is editing the worktree. On a conflict the
// conflicting paths are read, the rebase is aborted, so the worktree is as it
// was, and a *RebaseError is returned: the export turns it into a Decision on
// the task's stopped run (design §4.2); a rebase asked for directly raises
// none.
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
	env := gitEnv("GIT_COMMITTER_NAME=workharbor agent "+a.Role, "GIT_COMMITTER_EMAIL=agent@workharbor.invalid") // a rebase writes commits; the authors are kept
	out, code, err := w.svc.exec(ctx, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "rebase", ws.Integration}, Env: env,
	})
	if err != nil {
		return fmt.Errorf("rebase agent %s: %w", a.Role, err)
	}
	if code == 0 {
		return nil
	}
	bg := context.WithoutCancel(ctx)
	var paths []string
	if list, lcode, lerr := w.svc.exec(bg, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "diff", "--name-only", "--diff-filter=U"}, Env: env,
	}); lerr == nil && lcode == 0 {
		for _, p := range strings.Split(strings.TrimSpace(list), "\n") {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	// Leave the worktree as it was; a failed abort is reported with the conflict.
	_, acode, aerr := w.svc.exec(bg, string(ws.EnvID), runtime.ExecRequest{
		Cmd: []string{"git", "-C", a.Worktree, "rebase", "--abort"}, Env: env,
	})
	msg := fmt.Sprintf("agent/%s does not rebase onto %s: %s", a.Role, ws.Integration, strings.TrimSpace(out))
	if aerr != nil || acode != 0 {
		msg += fmt.Sprintf(" (and `rebase --abort` failed: exit %d, %v)", acode, aerr)
	}
	return &RebaseError{conflict: domain.NewConflict(domain.RuleRebase, "%s", msg), Paths: paths}
}

// gitEnv is the environment of a git command in the guest: the clone belongs to
// the host user, the guest runs as another, so safe.directory is set for it.
func gitEnv(extra ...string) []string {
	return append([]string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0=*"}, extra...)
}

// RemoveAgent removes an agent record. It is refused (store.RuleInUse) while a
// task that is not finished is assigned to it. The worktree and the branch stay
// in the clone, so no unpushed work is lost; the human removes the workspace
// folder when they are done with it.
func (w *Workspaces) RemoveAgent(ctx context.Context, workspace, role string) error {
	ws, err := w.svc.store.Workspace(ctx, workspace)
	if err != nil {
		return err
	}
	a, err := w.svc.store.AgentByRole(ctx, ws.ID, role)
	if err != nil {
		return err
	}
	return w.svc.store.RemoveAgent(ctx, a, domain.RemovedAgentEvent(a, w.svc.clock.Now()))
}

// Remove removes a workspace: its environment, with its network, sidecar and
// home volume, and its record. It is refused while the workspace has agents
// (remove them first). The folder and the clone in it are the human's and stay.
func (w *Workspaces) Remove(ctx context.Context, workspace string) error {
	ws, err := w.svc.store.Workspace(ctx, workspace)
	if err != nil {
		return err
	}
	agents, err := w.svc.store.Agents(ctx, ws.ID)
	if err != nil {
		return err
	}
	if len(agents) > 0 {
		return domain.NewConflict(store.RuleInUse, "workspace %s still has %d agent(s): remove them first", ws.Name, len(agents))
	}
	if ws.EnvID != "" {
		env := string(ws.EnvID)
		res, _ := w.svc.rt.Resources(ctx, env)
		if err := w.svc.rt.Stop(ctx, env); err != nil && !errors.Is(err, runtime.ErrNotFound) {
			return fmt.Errorf("stop environment %s: %w", env, err)
		}
		if err := w.svc.rt.Delete(ctx, env); err != nil {
			return fmt.Errorf("delete environment %s: %w", env, err)
		}
		for _, v := range res.Volumes {
			if err := w.svc.rt.RemoveVolume(ctx, v); err != nil {
				return fmt.Errorf("remove volume %s: %w", v, err)
			}
		}
	}
	return w.svc.store.RemoveWorkspace(ctx, ws, domain.RemovedWorkspaceEvent(ws, w.svc.clock.Now()))
}

// workflowOf is the preset a task started now would keep.
func (w *Workspaces) workflowOf(repo string) string {
	if w.cfg.Workflow == nil {
		return string(policy.DefaultPreset)
	}
	if p := w.cfg.Workflow(repo); p != "" {
		return p
	}
	return string(policy.DefaultPreset)
}
