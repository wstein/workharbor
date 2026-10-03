package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/agenttest"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge/forgetest"
	"github.com/wstein/workharbor/internal/gittest"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/policy"
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
	clock  *fakeClock
	bgMu   sync.Mutex
	bgErrs []error // what the service reported through OnError, from goroutines of its own
	egress bool    // give the environments an egress sidecar
	home   bool    // give the environments an agent home volume
}

// reported returns what the service reported through OnError so far. The
// service reports from goroutines of its own, so it is read under a lock.
func (r *wsRig) reported() []error {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	return append([]error(nil), r.bgErrs...)
}

// forget drops what was reported so far.
func (r *wsRig) forget() {
	r.bgMu.Lock()
	r.bgErrs = nil
	r.bgMu.Unlock()
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
		cmd := gittest.Git(bg, outside, r.forge, nil, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	clock := &fakeClock{now: t0}
	r.clock = clock
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
		OnError: func(err error) { r.bgMu.Lock(); r.bgErrs = append(r.bgErrs, err); r.bgMu.Unlock() },
		NewID:   func() domain.ID { return r.id("d") },
		Spec: func(domain.Task, domain.Run) agent.StartSpec {
			s := spec()
			s.Env = []string{"HOME=/home/agent"}
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
		Issues: r.issues,
		Config: &config.Config{Roots: config.Roots{Workspaces: []string{r.root}}},
		Git:    g,
		Spec: func(w domain.Workspace) runtime.Spec {
			spec := r.rt.NewSpec()
			if r.home {
				spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountVolume, Source: "whtmp-conformance-" + string(w.ID), Target: "/home/agent"})
			}
			if r.egress {
				spec.Egress = &runtime.Egress{Image: spec.Image, Proxy: r.rt.ProxyBinary, Allow: []string{"api.anthropic.com"}}
			}
			return spec
		},
		Prepare: r.rt.Prepare,
		NewID:   func() domain.ID { return r.id("x") },
		// The rig's repositories publish into main (issue #208).
		Branch: func(string) string { return "main" },
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	r := newWsRig(t)
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) > 3 && cmd[0] == "git" && cmd[3] == "worktree" {
			return nil, "fatal: invalid reference", 128, true
		}
		return nil, "", 0, false
	}
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
	r.fake.OnExec = nil
	if _, _, err := r.ws.Create(bg, CreateRequest{Name: "fail", Path: folder, Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"}); err != nil {
		t.Errorf("a second try: %v", err)
	}
}

func TestAddAgentMakesAWorktreeAndRefusesADuplicate(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// The agent's session outlives the request that started it: the API call
// returns at once and the agent runs for hours (found by the serve integration
// run, where the session died with its request).
func TestTheSessionOutlivesTheContextThatStartedIt(t *testing.T) {
	t.Parallel()
	r := newWsRig(t) // blocking sessions
	_, a := r.create("outlive")
	ctx, cancel := context.WithCancel(context.Background())
	task, run, err := r.ws.StartTask(ctx, StartRequest{AgentID: a.ID, Issue: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	cancel() // the request is over
	time.Sleep(200 * time.Millisecond)
	if !r.svc.attached(run) {
		t.Fatal("the session ended with the request that started it")
	}
	v, _ := r.svc.Show(bg, task)
	if v.Runs[0].State != domain.RunRunning {
		t.Errorf("run = %s, want running", v.Runs[0].State)
	}
	// An answer that resumes it does the same.
	if _, err := r.svc.Say(bg, task, "still there?"); err != nil {
		t.Errorf("the session cannot be spoken to: %v", err)
	}
}

// The agent gets the egress proxy's address and a home, and no secret.
func TestTheAgentIsStartedWithTheProxyAndItsHome(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.egress = true
	_, a := r.create("proxyenv")
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"}); err != nil {
		t.Fatal(err)
	}
	spec := r.agent.Specs[0]
	var proxy string
	for _, e := range spec.Env {
		if v, ok := strings.CutPrefix(e, "HTTPS_PROXY="); ok {
			proxy = v
		}
	}
	if !strings.HasPrefix(proxy, "http://") || !strings.HasSuffix(proxy, ":3128") {
		t.Errorf("env = %v, want HTTPS_PROXY=http://<sidecar>:3128", spec.Env)
	}
	have := map[string]bool{}
	for _, e := range spec.Env {
		have[strings.SplitN(e, "=", 2)[0]] = true
	}
	for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "HOME"} {
		if !have[k] {
			t.Errorf("no %s in %v", k, spec.Env)
		}
	}
}

// A Create that fails after the environment was made takes back its home volume
// too: a volume outlives its environment, so deleting the environment is not
// enough (found by the serve integration run).
func TestAFailedCreateDoesNotLeakTheHomeVolume(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.home = true
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) { // the worktree fails after the environment and its volume exist
		if len(cmd) > 3 && cmd[0] == "git" && cmd[3] == "worktree" {
			return nil, "fatal: invalid reference", 128, true
		}
		return nil, "", 0, false
	}
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "leak", Path: r.folder("leak"), Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"})
	if err == nil {
		t.Fatal("the worktree should fail")
	}
	inv, err := r.rt.Adapter.Inventory(bg)
	if err != nil || len(inv.Volumes) != 0 || len(inv.Networks) != 0 || len(inv.Sidecars) != 0 {
		t.Errorf("left behind: %+v, %v", inv, err)
	}
}

func TestRemoveAgentAndWorkspace(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.home = true
	w, a := r.create("rm")
	second, err := r.ws.AddAgent(bg, "rm", "runtime", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = second
	// A workspace with agents is not removed.
	var c *domain.ConflictError
	if err := r.ws.Remove(bg, "rm"); !errors.As(err, &c) || c.Rule != "in-use" {
		t.Errorf("a workspace with agents = %v", err)
	}
	// An agent with an unfinished task is not removed; the other one is.
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ws.RemoveAgent(bg, "rm", "docs"); !errors.As(err, &c) || c.Rule != "in-use" {
		t.Errorf("an agent with a task = %v", err)
	}
	if err := r.ws.RemoveAgent(bg, "rm", "runtime"); err != nil {
		t.Fatal(err)
	}
	if err := r.ws.RemoveAgent(bg, "rm", "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown agent = %v", err)
	}
	must(t, r.svc.Cancel(bg, task))
	if err := r.ws.RemoveAgent(bg, "rm", "docs"); err != nil {
		t.Fatal(err)
	}
	// The clone is the human's: it stays. The environment, network and volume go.
	if err := r.ws.Remove(bg, "rm"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.Path, CloneDir, ".git")); err != nil {
		t.Errorf("the clone was removed: %v", err)
	}
	inv, _ := r.rt.Adapter.Inventory(bg)
	if len(inv.Volumes)+len(inv.Networks)+len(inv.Sidecars) != 0 {
		t.Errorf("left behind: %+v", inv)
	}
	if envs, _ := r.rt.Adapter.List(bg, r.rt.Owner); len(envs) != 0 {
		t.Errorf("environments left: %+v", envs)
	}
	if _, err := r.store.Workspace(bg, "rm"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the record is still there: %v", err)
	}
	if err := r.ws.Remove(bg, "rm"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removing twice = %v", err)
	}
}

func TestTasksAndShowCarryTheAgentAsWorkspaceSlashRole(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	_, a := r.create("named")
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := r.svc.List(bg, false)
	v, _ := r.svc.Show(bg, task)
	if len(list) != 1 || list[0].Agent != "named/docs" || v.Agent != "named/docs" {
		t.Errorf("list %+v, show agent %q", list, v.Agent)
	}
}

// An image without git is refused when the workspace is created, with a message
// that says git is missing, not later in `git worktree add` (design D44).
func TestCreateRefusesAnImageWithoutGit(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.home = true // the refusal also takes back the home volume
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 2 && cmd[0] == "git" && cmd[1] == "--version" {
			return nil, `exec: "git": executable file not found in $PATH`, 1, true
		}
		return nil, "", 0, false
	}
	folder := r.folder("nogit")
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "nogit", Path: folder, Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"})
	var bad *domain.InvalidError
	if !errors.As(err, &bad) {
		t.Fatalf("err = %v, want an invalid request", err)
	}
	for _, want := range []string{"has no git", "executable file not found", "environment.image", "D44"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message lacks %q: %v", want, err)
		}
	}
	if list, _ := r.store.Workspaces(bg); len(list) != 0 {
		t.Errorf("workspace left behind: %+v", list)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Errorf("the clone was left in the folder: %v", entries)
	}
	if inv, err := r.rt.Adapter.Inventory(bg); err != nil || len(inv.Networks)+len(inv.Sidecars)+len(inv.Volumes) != 0 {
		t.Errorf("runtime resources left behind: %+v, %v", inv, err)
	}
}

// A task keeps the preset its repository had when it started: a later change of
// the configuration, even to a looser preset, does not apply to it (D47).
func TestATaskKeepsThePresetItStartedUnder(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	current := "published"
	r.ws.cfg.Workflow = func(string) string { return current }
	_, a := r.create("keep")
	task, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	current = "prototype" // the configuration loosens
	agg, _ := r.store.LoadTask(bg, task)
	if agg.Task().Workflow != "published" {
		t.Errorf("the task's preset is %q, want the published it started under", agg.Task().Workflow)
	}
	if got := effectivePreset(agg.Task().Workflow, policy.Prototype); got != policy.Published {
		t.Errorf("a published task under a prototype configuration publishes as %s", got)
	}
	// a repository with nothing configured gets the default
	r.ws.cfg.Workflow = nil
	if got := r.ws.workflowOf("a/b"); got != "integration" {
		t.Errorf("default %q", got)
	}
}

func TestTheEffectivePresetIsTheStricterOfTheTaskAndTheRepository(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		task string
		repo policy.Preset
		want policy.Preset
	}{
		{"published", policy.Prototype, policy.Published},
		{"prototype", policy.Published, policy.Published},
		{"integration", policy.Prototype, policy.Integration},
		{"", policy.Prototype, policy.Prototype},
		{"", "", policy.Integration},
		{"nonsense", policy.Published, policy.Published},
	} {
		if got := effectivePreset(c.task, c.repo); got != c.want {
			t.Errorf("task %q repo %q = %s, want %s", c.task, c.repo, got, c.want)
		}
	}
}

// Where a tool reads its output location from the environment, the environment
// puts it on the build volume, in the agent's own directory, outside the
// bind-mounted checkout (D39).
func TestAnAgentsToolsWriteTheirOutputToTheBuildVolumeNotTheCheckout(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.BuildDir = "/var/whr/build"
	w, a := r.create("run")
	if want := "mkdir -p /var/whr/build/docs"; !strings.Contains(r.logs(w.EnvID), want) {
		t.Errorf("the agent's build directory was not made (%q); the log:\n%s", want, r.logs(w.EnvID))
	}
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Fatal(err)
	}
	if len(r.agent.Specs) != 1 {
		t.Fatalf("%d agents started", len(r.agent.Specs))
	}
	env := r.agent.Specs[0].Env
	for _, want := range []string{"CARGO_TARGET_DIR=/var/whr/build/docs/cargo-target", "UV_PROJECT_ENVIRONMENT=/var/whr/build/docs/venv", "HOME=/home/agent"} {
		if !slices.Contains(env, want) {
			t.Errorf("the agent's environment lacks %s: %v", want, env)
		}
	}
	for _, e := range env {
		if _, v, _ := strings.Cut(e, "="); strings.HasPrefix(v, WorkspaceMount) {
			t.Errorf("%s points into the checkout, which is the bind mount", e)
		}
	}
}

func TestWithoutABuildVolumeNothingIsRelocated(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	w, a := r.create("run")
	if strings.Contains(r.logs(w.EnvID), "mkdir") {
		t.Errorf("a build directory was made without a build volume:\n%s", r.logs(w.EnvID))
	}
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Fatal(err)
	}
	for _, e := range r.agent.Specs[0].Env {
		if strings.HasPrefix(e, "CARGO_TARGET_DIR=") || strings.HasPrefix(e, "UV_PROJECT_ENVIRONMENT=") {
			t.Errorf("%s was set without a build volume: the path would not exist", e)
		}
	}
}

func repoSizeEvents(t *testing.T, r *wsRig, task domain.ID) []domain.RepoSize {
	t.Helper()
	evs, err := r.store.EventsSince(bg, task, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.RepoSize
	for _, e := range evs {
		if e.Kind != domain.EventRepoSize {
			continue
		}
		if e.Tier != domain.TierAudit {
			t.Errorf("the size entry is in tier %s, want audit", e.Tier)
		}
		var rs domain.RepoSize
		if err := json.Unmarshal(e.Payload, &rs); err != nil {
			t.Fatal(err)
		}
		out = append(out, rs)
	}
	return out
}

// A run's start counts the files its checkout tracks and records it in the task's
// events; above 50 000 it warns (D39).
func TestARunRecordsHowManyFilesItsCheckoutTracksAndWarnsWhenItIsVeryLarge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		files int
		warn  bool
	}{{120, false}, {domain.RepoSizeWarnFiles, false}, {domain.RepoSizeWarnFiles + 1, true}, {153_000, true}} {
		r := newWsRig(t)
		r.fake.TrackedFiles = tc.files
		_, a := r.create("run")
		task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
		if err != nil {
			t.Fatal(err)
		}
		got := repoSizeEvents(t, r, task)
		if len(got) != 1 || got[0].TrackedFiles != tc.files || got[0].Warn != tc.warn || got[0].RunID != run {
			t.Fatalf("%d files: events %+v", tc.files, got)
		}
		if tc.warn && !strings.Contains(got[0].Message, "slow") {
			t.Errorf("the warning says nothing about what to expect: %q", got[0].Message)
		}
		if !tc.warn && got[0].Message != "" {
			t.Errorf("a warning on %d files: %q", tc.files, got[0].Message)
		}
	}
}

// A count that cannot be made never fails the start: it is reported.
func TestACountThatFailsIsReportedAndTheRunStartsAnyway(t *testing.T) {
	t.Parallel()
	for name, answer := range map[string]struct {
		out  string
		code int
	}{"exit": {"", 3}, "not a number": {"lots\n", 0}} {
		r := newWsRig(t)
		r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
			if len(cmd) > 2 && cmd[0] == "sh" && strings.Contains(cmd[2], "ls-files") {
				return []byte(answer.out), "boom", answer.code, true
			}
			return nil, "", 0, false
		}
		_, a := r.create("run")
		task, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !r.svc.attached(run) {
			t.Errorf("%s: the run did not start", name)
		}
		if got := repoSizeEvents(t, r, task); len(got) != 0 {
			t.Errorf("%s: an event for a count that failed: %+v", name, got)
		}
		var found bool
		for _, e := range r.reported() {
			found = found || strings.Contains(e.Error(), "count the tracked files")
		}
		if !found {
			t.Errorf("%s: nothing was reported: %v", name, r.reported())
		}
	}
}

func TestARepoSizeNeedsATaskARunAndACount(t *testing.T) {
	t.Parallel()
	for _, bad := range []struct {
		task, run domain.ID
		files     int
	}{{"", "r", 1}, {"t", "", 1}, {"t", "r", -1}} {
		if _, err := domain.NewRepoSizeEvent(bad.task, bad.run, bad.files, t0); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}

// The supervisor's git commands in an agent's checkout run with hooks and fsmonitor
// off: a .git an agent wrote must not run a command of its choice, as it must not
// on the host. Plain git runs the planted fsmonitor on `git ls-files` (measured,
// git 2.54), so the test is a real one.
func TestTheGuestsGitEnvironmentStopsAPlantedFsmonitorAndHook(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	home := t.TempDir()
	repo := filepath.Join(t.TempDir(), "r")
	marks := t.TempDir()
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := gittest.Git(bg, home, repo, nil, args...)
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	run(gittest.Identity, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(gittest.Identity, "add", "a")
	run(gittest.Identity, "commit", "-q", "-m", "one")
	run(nil, "config", "core.fsmonitor", "sh -c 'touch "+filepath.Join(marks, "fsmonitor")+"; true'")
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+filepath.Join(marks, "hook")+"\n"), 0o700); err != nil { //nolint:gosec // the planted hook this test is about
		t.Fatal(err)
	}
	ran := func() string {
		ents, _ := os.ReadDir(marks)
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
			_ = os.Remove(filepath.Join(marks, e.Name()))
		}
		return strings.Join(names, " ")
	}
	exercise := func(env []string) string {
		ran()
		run(env, "ls-files", "-z")
		run(env, "status", "--short")
		run(env, "checkout", "-q", "main")
		return ran()
	}
	if plain := exercise(nil); !strings.Contains(plain, "fsmonitor") || !strings.Contains(plain, "hook") {
		t.Fatalf("plain git ran %q: the plant does not work, so this test proves nothing", plain)
	}
	if got := exercise(gitEnv()); got != "" {
		t.Errorf("git with gitEnv() ran %q", got)
	}
}

// A role that is added again later does not inherit the old one's output.
func TestRemovingAnAgentClearsItsDirectoryOnTheBuildVolume(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.BuildDir = "/var/whr/build"
	w, _ := r.create("run")
	second, err := r.ws.AddAgent(bg, w.Name, "review", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ws.RemoveAgent(bg, w.Name, second.Role); err != nil {
		t.Fatal(err)
	}
	if want := "rm -rf -- /var/whr/build/review"; !strings.Contains(r.logs(w.EnvID), want) {
		t.Errorf("the agent's build directory was not cleared (%q); the log:\n%s", want, r.logs(w.EnvID))
	}
	if strings.Contains(r.logs(w.EnvID), "rm -rf -- /var/whr/build/docs") {
		t.Error("another agent's directory was cleared")
	}
	// A failure to clear is reported and does not undo the removal.
	again, err := r.ws.AddAgent(bg, w.Name, "review", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) > 0 && cmd[0] == "rm" {
			return nil, "", 1, true
		}
		return nil, "", 0, false
	}
	if err := r.ws.RemoveAgent(bg, w.Name, again.Role); err != nil {
		t.Fatalf("a failed clear undid the removal: %v", err)
	}
	var reported bool
	for _, e := range r.reported() {
		reported = reported || strings.Contains(e.Error(), "clear the build directory")
	}
	if !reported {
		t.Errorf("the failure was not reported: %v", r.reported())
	}
}

func TestWithoutABuildVolumeRemovingAnAgentRunsNothing(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	w, _ := r.create("run")
	second, err := r.ws.AddAgent(bg, w.Name, "review", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ws.RemoveAgent(bg, w.Name, second.Role); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.logs(w.EnvID), "rm ") {
		t.Errorf("a removal ran without a build volume:\n%s", r.logs(w.EnvID))
	}
}

func TestCreateDefaultsTheBranchToThePublicationTarget(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		workflow, branch, want string
		fake                   string
	}{
		"integration without a branch": {workflow: "integration", want: "develop"},
		"integration with a branch":    {workflow: "integration", branch: "main", want: "main"},
		"published reads the default":  {workflow: "published", want: "main"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newWsRig(t)
			r.ws.cfg.Workflow = func(string) string { return tc.workflow }
			r.ws.cfg.Branch = func(string) string { return tc.branch }
			got, _, err := r.ws.integrationFor(bg, "a/b", "")
			if err != nil || got != tc.want {
				t.Errorf("integrationFor = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestCreateRefusesABranchThatIsNotThePublicationTarget(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.Workflow = func(string) string { return "integration" }
	r.ws.cfg.Branch = func(string) string { return "develop" }
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "w", Path: r.folder("w"), Repo: "a/b", Integration: "main", Source: r.forge, Role: "docs"})
	var inv *domain.InvalidError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), `"main"`) || !strings.Contains(err.Error(), `"develop"`) {
		t.Fatalf("err = %v, want a refusal naming main and develop", err)
	}
	if list, _ := r.store.Workspaces(bg); len(list) != 0 {
		t.Errorf("workspaces left behind: %+v", list)
	}
}

func TestIntegrationForFailsClosedWithoutATarget(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.Workflow = func(string) string { return "prototype" }
	r.ws.cfg.Branch = func(string) string { return "" }
	if got, _, err := r.ws.integrationFor(bg, "a/b", ""); err == nil {
		t.Errorf("a prototype without a branch got %q", got)
	}
	r.ws.cfg.Workflow = func(string) string { return "published" }
	r.ws.cfg.Issues = noDefaultBranch{}
	if got, _, err := r.ws.integrationFor(bg, "a/b", ""); err == nil {
		t.Errorf("a forge that cannot name the default branch got %q", got)
	}
	r.ws.cfg.Issues = failingDefault{}
	_, _, err := r.ws.integrationFor(bg, "a/b", "")
	if err == nil || !strings.Contains(err.Error(), "no route to host") {
		t.Errorf("a failed read: err = %v, want the cause", err)
	}
	r.ws.cfg.Issues = emptyDefault{}
	_, _, err = r.ws.integrationFor(bg, "a/b", "")
	if err == nil || strings.Contains(err.Error(), "<nil>") || !strings.Contains(err.Error(), "no default branch") {
		t.Errorf("an empty name: err = %v, want a message without <nil>", err)
	}
}

func TestCreateNamesWhereAnUnacceptableBranchCameFrom(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.Workflow = func(string) string { return "published" }
	r.issues.DefaultBranch = "agent/x"
	_, _, err := r.ws.Create(bg, CreateRequest{Name: "w", Path: r.folder("w"), Repo: "a/b", Source: r.forge, Role: "docs"})
	if err == nil || !strings.Contains(err.Error(), `"agent/x"`) || !strings.Contains(err.Error(), "default branch of a/b") {
		t.Fatalf("err = %v, want the forge named as the source", err)
	}
	r.ws.cfg.Workflow = func(string) string { return "prototype" }
	r.ws.cfg.Branch = func(string) string { return "agent/y" }
	_, _, err = r.ws.Create(bg, CreateRequest{Name: "w", Path: r.folder("w2"), Repo: "a/b", Source: r.forge, Role: "docs"})
	if err == nil || !strings.Contains(err.Error(), "integration_branch in the configuration") {
		t.Fatalf("err = %v, want the configuration named as the source", err)
	}
}

// emptyDefault is a forge that answers no error and no name.
type emptyDefault struct{ IssueSource }

func (emptyDefault) DefaultBranchName(context.Context, string) (string, error) { return "", nil }

// noDefaultBranch is an IssueSource that is not a forge.DefaultBrancher.
type noDefaultBranch struct{ IssueSource }
