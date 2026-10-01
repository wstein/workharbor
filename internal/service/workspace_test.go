package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/agenttest"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge/forgetest"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/runtime/runtimetest"
	"github.com/wstein/workharbor/internal/store"
)

// wsRig is a service with workspace operations on the fakes, a real hostgit
// and a real forge repository on disk.
type wsRig struct {
	t      *testing.T
	svc    *Service
	ws     *Workspaces
	store  *store.Store
	rt     runtimetest.Harness
	fake   *runtimetest.Fake
	agent  *agenttest.Fake
	root   string // the workspace root
	forge  string // the source repository
	ids    int
	failAg bool
	issues *forgetest.Fake
}

func newWsRig(t *testing.T) *wsRig { return newWsRigBlocking(t, true) }

// newWsRigBlocking makes the rig; with block false the fake agent's sessions
// finish at once instead of running until they are stopped.
func newWsRigBlocking(t *testing.T, block bool) *wsRig {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	r := &wsRig{t: t}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.rt = runtimetest.NewFakeHarness(t)
	r.fake = r.rt.Adapter.(*runtimetest.Fake)
	// The runtime only mounts folders it allows: the harness allows one below its home.
	r.root = filepath.Join(r.rt.AllowedMount, "workspaces")
	if err := os.MkdirAll(r.root, 0o750); err != nil {
		t.Fatal(err)
	}
	// The forge lives outside the workspace root, as a human's repository does.
	outside, err := os.MkdirTemp("", "ws-forge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	r.forge = filepath.Join(outside, "forge")
	if err := os.MkdirAll(r.forge, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=h", "-c", "user.email=h@h", "commit", "-q", "--allow-empty", "-m", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", r.forge}, args...)...) //nolint:gosec,noctx // test setup
		cmd.Env = []string{"HOME=" + outside, "GIT_CONFIG_NOSYSTEM=1", "PATH=" + os.Getenv("PATH")}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	clock := &fakeClock{now: t0}
	r.store, err = store.Open(bg, filepath.Join(dir, "workharbor.db"), store.WithClock(func() time.Time { return clock.now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.store.Close() })
	r.agent = agenttest.NewFake(agenttest.FullCaps())
	if block {
		r.agent.Block()
	}
	r.svc = New(r.store, r.rt.Adapter, r.agent, clock, Config{
		Owner: r.rt.Owner, ReadyCmd: []string{"echo", "ready"},
		NewID: func() domain.ID { return r.id("d") },
		Spec: func(domain.Task, domain.Run) agent.StartSpec {
			s := spec()
			if r.failAg {
				s.Auth = "no-such-auth"
			}
			return s
		},
	})
	t.Cleanup(r.svc.Shutdown)

	g, err := hostgit.New()
	if err != nil {
		t.Skipf("git is not available: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	r.issues = forgetest.NewFake()
	r.ws = NewWorkspaces(r.svc, WorkspaceConfig{
		Issues:  r.issues,
		Config:  &config.Config{Roots: config.Roots{Workspaces: []string{r.root}}},
		Git:     g,
		Spec:    func(domain.Workspace) runtime.Spec { return r.rt.NewSpec() },
		Prepare: r.rt.Prepare,
		NewID:   func() domain.ID { return r.id("x") },
	})
	return r
}

func (r *wsRig) id(prefix string) domain.ID {
	r.ids++
	return domain.ID(fmt.Sprintf("%s-%d", prefix, r.ids))
}

func (r *wsRig) folder(name string) string {
	r.t.Helper()
	p := filepath.Join(r.root, name)
	if err := os.MkdirAll(p, 0o750); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// create makes a workspace whose first agent has the role docs.
func (r *wsRig) create(name string) (domain.Workspace, domain.Agent) {
	r.t.Helper()
	w, a, err := r.ws.Create(bg, CreateRequest{
		Name: name, Path: r.folder(name), Repo: "wstein/workharbor", Integration: "main", Source: r.forge, Role: "docs", Instructions: "docs only",
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return w, a
}

func (r *wsRig) logs(env domain.ID) string {
	r.t.Helper()
	b, err := r.rt.Adapter.Logs(bg, string(env))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func TestCreateMakesTheWorkspaceItsEnvironmentAndFirstAgent(t *testing.T) {
	r := newWsRig(t)
	w, a := r.create("docs-ws")

	if _, err := os.Stat(filepath.Join(w.Path, CloneDir, ".git")); err != nil {
		t.Errorf("the agent clone was not seeded: %v", err)
	}
	if got, err := r.store.Workspace(bg, "docs-ws"); err != nil || got.EnvID != w.EnvID || w.EnvID == "" {
		t.Errorf("stored workspace %+v, %v", got, err)
	}
	if got, err := r.store.AgentByRole(bg, w.ID, "docs"); err != nil || got.ID != a.ID || got.Branch != "agent/docs" {
		t.Errorf("stored agent %+v, %v", got, err)
	}
	info, err := r.rt.Adapter.Inspect(bg, string(w.EnvID))
	if err != nil || info.State != domain.EnvRunning {
		t.Errorf("environment: %+v, %v", info, err)
	}
	mounted := false
	for _, m := range info.Mounts {
		if m.Target == WorkspaceMount && m.Kind == runtime.MountBind && !m.ReadOnly {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("the workspace folder is not mounted read-write at %s: %+v", WorkspaceMount, info.Mounts)
	}
	// The worktree is made inside the environment.
	if want := "git -C /ws/repo worktree add -b agent/docs /ws/wt/docs main"; !strings.Contains(r.logs(w.EnvID), want) {
		t.Errorf("the environment did not run %q; its log:\n%s", want, r.logs(w.EnvID))
	}
	evs, _ := r.store.EventsSince(bg, domain.WorkspaceStream(w.ID), 0, 10)
	if len(evs) != 2 || evs[0].Kind != domain.EventWorkspaceAdded || evs[1].Kind != domain.EventAgentAdded {
		t.Errorf("events: %+v", evs)
	}
}

func TestCreateRefusesABadFolderAndLeavesNothing(t *testing.T) {
	r := newWsRig(t)
	outside := t.TempDir()
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "w", Path: outside, Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"})
	var inv *domain.InvalidError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), "not below a workspace root") {
		t.Errorf("err = %v", err)
	}
	for name, req := range map[string]CreateRequest{
		"bad role": {Name: "w", Path: r.folder("w1"), Repo: "a/b", Integration: "main", Source: r.forge, Role: "../x"},
		"bad name": {Name: "W", Path: r.folder("w2"), Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"},
	} {
		if _, _, err := r.ws.Create(bg, req); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if list, _ := r.store.Workspaces(bg); len(list) != 0 {
		t.Errorf("workspaces left behind: %+v", list)
	}
	if entries, _ := os.ReadDir(filepath.Join(r.root, "w1")); len(entries) != 0 {
		t.Errorf("a refused request seeded a clone: %v", entries)
	}
}

func TestCreateTakesBackWhatItMadeWhenTheWorktreeFails(t *testing.T) {
	r := newWsRig(t)
	r.fake.GitExit = 128
	folder := r.folder("fail")
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "fail", Path: folder, Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"})
	if err == nil || !strings.Contains(err.Error(), "git exited 128") {
		t.Fatalf("err = %v", err)
	}
	if list, _ := r.store.Workspaces(bg); len(list) != 0 {
		t.Errorf("workspace left behind: %+v", list)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Errorf("the clone was left in the folder: %v", entries)
	}
	inv, err := r.rt.Adapter.Inventory(bg)
	if err != nil || len(inv.Networks)+len(inv.Sidecars) != 0 {
		t.Errorf("runtime resources left behind: %+v, %v", inv, err)
	}
	if envs, _ := r.rt.Adapter.List(bg, r.rt.Owner); len(envs) != 0 {
		t.Errorf("environments left behind: %+v", envs)
	}
	// The folder is free to try again.
	r.fake.GitExit = 0
	if _, _, err := r.ws.Create(bg, CreateRequest{Name: "fail", Path: folder, Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"}); err != nil {
		t.Errorf("a second try: %v", err)
	}
}

func TestAddAgentMakesAWorktreeAndRefusesADuplicate(t *testing.T) {
	r := newWsRig(t)
	w, _ := r.create("multi")
	a, err := r.ws.AddAgent(bg, "multi", "runtime", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Worktree != "/ws/wt/runtime" || !strings.Contains(r.logs(w.EnvID), "worktree add -b agent/runtime /ws/wt/runtime main") {
		t.Errorf("agent %+v; log:\n%s", a, r.logs(w.EnvID))
	}
	if _, err := r.ws.AddAgent(bg, "multi", "runtime", "", ""); err == nil {
		t.Error("a duplicate role was accepted")
	}
	r.fake.GitExit = 1
	if _, err := r.ws.AddAgent(bg, "multi", "review", "", ""); err == nil {
		t.Error("a failed worktree was accepted")
	}
	if _, err := r.store.AgentByRole(bg, w.ID, "review"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an agent without a worktree was recorded: %v", err)
	}
	if _, err := r.ws.AddAgent(bg, "nope", "x", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: %v", err)
	}
}

func TestStartTaskRunsTheAgentInItsWorktree(t *testing.T) {
	r := newWsRig(t)
	_, a := r.create("run")
	task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7", Prompt: "write the manual"})
	if err != nil {
		t.Fatal(err)
	}
	agg, err := r.store.LoadTask(bg, task)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := agg.Run(run)
	if agg.Task().AgentID != a.ID || got.AgentID != a.ID || got.State != domain.RunRunning || agg.Task().State != domain.TaskRunning {
		t.Errorf("task %+v run %+v", agg.Task(), got)
	}
	if !r.svc.attached(run) {
		t.Error("the session was not attached")
	}
}

// One live run per environment: a second agent is refused with a clear error
// until several agents at once are built (#94).
func TestASecondAgentInTheSameEnvironmentIsRefused(t *testing.T) {
	r := newWsRig(t)
	_, first := r.create("two")
	second, err := r.ws.AddAgent(bg, "two", "runtime", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: first.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	_, _, err = r.ws.StartTask(bg, StartRequest{AgentID: second.ID, Issue: "#2"})
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != domain.RuleEnvBusy || !strings.Contains(err.Error(), "several agents at once are not supported") {
		t.Fatalf("err = %v", err)
	}
	if ids, _ := r.store.ActiveTaskIDs(bg); len(ids) != 1 {
		t.Errorf("the refused start left a task: %v", ids)
	}
}

func TestTwoStartsAtOnceOnlyOneWins(t *testing.T) {
	r := newWsRig(t)
	_, first := r.create("race")
	second, err := r.ws.AddAgent(bg, "race", "runtime", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, a := range []domain.Agent{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: fmt.Sprintf("#%d", i)})
		}()
	}
	wg.Wait()
	if ok := btoi(errs[0] == nil) + btoi(errs[1] == nil); ok != 1 {
		t.Errorf("%d starts succeeded, want exactly one: %v", ok, errs)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// A start whose agent cannot be launched fails the run into a Decision and
// frees the environment for the next try.
func TestAFailedAgentStartOpensADecisionAndFreesTheEnvironment(t *testing.T) {
	r := newWsRig(t)
	w, a := r.create("failstart")
	r.failAg = true
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err == nil {
		t.Fatal("an agent that cannot start was reported as started")
	}
	live, err := r.store.LiveRuns(bg, w.EnvID)
	if err != nil || len(live) != 0 {
		t.Errorf("live runs after a failed start: %+v, %v", live, err)
	}
	r.failAg = false
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#2"}); err != nil {
		t.Errorf("the next start: %v", err)
	}
}

func TestRebaseRunsInTheEnvironmentAndReportsAConflict(t *testing.T) {
	r := newWsRig(t)
	w, a := r.create("rebase")
	if err := r.ws.Rebase(bg, a.ID); err != nil {
		t.Fatal(err)
	}
	if want := "git -C /ws/wt/docs rebase main"; !strings.Contains(r.logs(w.EnvID), want) {
		t.Errorf("no %q in the log:\n%s", want, r.logs(w.EnvID))
	}

	r.fake.GitExit = 1 // the rebase and the abort both exit 1 in the fake
	err := r.ws.Rebase(bg, a.ID)
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != domain.RuleRebase {
		t.Fatalf("err = %v, want rebase-conflict", err)
	}
	if !strings.Contains(err.Error(), "rebase --abort") {
		t.Errorf("a failed abort must be reported: %v", err)
	}
	r.fake.GitExit = 0
	if !strings.Contains(r.logs(w.EnvID), "git -C /ws/wt/docs rebase --abort") {
		t.Errorf("a conflict must abort the rebase:\n%s", r.logs(w.EnvID))
	}

	// Not under a running agent.
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ws.Rebase(bg, a.ID); !errors.As(err, &c) || c.Rule != domain.RuleAgentActive {
		t.Errorf("a rebase under a running agent: %v", err)
	}
	if err := r.ws.Rebase(bg, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown agent: %v", err)
	}
}
