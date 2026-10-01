package store

import (
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

var wsNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func addWorkspace(t *testing.T, s *Store, id domain.ID, name, path string) domain.Workspace {
	t.Helper()
	w, ev, err := domain.NewWorkspace(id, name, path, "wstein/workharbor", "main", wsNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddWorkspace(bg, w, ev); err != nil {
		t.Fatal(err)
	}
	return w
}

func addAgent(t *testing.T, s *Store, id, ws domain.ID, role string) domain.Agent {
	t.Helper()
	a, ev, err := domain.NewAgent(id, ws, role, "be careful", "", wsNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAgent(bg, a, ev); err != nil {
		t.Fatal(err)
	}
	return a
}

func wantRule(t *testing.T, err error, rule domain.Rule) {
	t.Helper()
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != rule {
		t.Errorf("err = %v, want conflict %s", err, rule)
	}
}

func TestWorkspaceAndAgentRegistry(t *testing.T) {
	s := openTemp(t)
	w := addWorkspace(t, s, "w1", "main-ws", "/ws/main")
	addAgent(t, s, "a1", "w1", "docs")
	addAgent(t, s, "a2", "w1", "runtime")

	got, err := s.Workspace(bg, "main-ws")
	if err != nil || got != w {
		t.Fatalf("by name: %+v %v, want %+v", got, err, w)
	}
	if byID, err := s.Workspace(bg, "w1"); err != nil || byID != w {
		t.Errorf("by id: %+v %v", byID, err)
	}
	if _, err := s.Workspace(bg, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: %v", err)
	}
	agents, err := s.Agents(bg, "w1")
	if err != nil || len(agents) != 2 || agents[0].Role != "docs" || agents[1].Branch != "agent/runtime" || agents[0].Worktree != "/ws/wt/docs" {
		t.Errorf("agents = %+v, %v", agents, err)
	}
	if a, err := s.AgentByRole(bg, "w1", "docs"); err != nil || a.ID != "a1" || a.Instructions != "be careful" {
		t.Errorf("by role: %+v %v", a, err)
	}

	if err := s.SetWorkspaceEnv(bg, "w1", "e1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentSession(bg, "a1", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if w2, _ := s.Workspace(bg, "w1"); w2.EnvID != "e1" {
		t.Errorf("env = %q", w2.EnvID)
	}
	if a, _ := s.Agent(bg, "a1"); a.SessionID != "sess-1" {
		t.Errorf("session = %q", a.SessionID)
	}

	// The creation events are in the workspace's stream, as audit.
	evs, err := s.EventsSince(bg, domain.WorkspaceStream("w1"), 0, 10)
	if err != nil || len(evs) != 3 || evs[0].Kind != domain.EventWorkspaceAdded || evs[1].Kind != domain.EventAgentAdded || evs[0].Tier != domain.TierAudit {
		t.Errorf("events = %+v, %v", evs, err)
	}
}

func TestRegistryRefusesDuplicatesAndInUse(t *testing.T) {
	s := openTemp(t)
	addWorkspace(t, s, "w1", "main-ws", "/ws/main")
	addAgent(t, s, "a1", "w1", "docs")

	w, ev, _ := domain.NewWorkspace("w2", "main-ws", "/ws/other", "a/b", "main", wsNow)
	wantRule(t, s.AddWorkspace(bg, w, ev), RuleExists)
	w, ev, _ = domain.NewWorkspace("w3", "other", "/ws/main", "a/b", "main", wsNow)
	wantRule(t, s.AddWorkspace(bg, w, ev), RuleExists)

	a, ev, _ := domain.NewAgent("a9", "w1", "docs", "", "", wsNow)
	wantRule(t, s.AddAgent(bg, a, ev), RuleExists)
	a, ev, _ = domain.NewAgent("a9", "nope", "docs", "", "", wsNow)
	if err := s.AddAgent(bg, a, ev); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("agent in an unknown workspace: %v", err)
	}

	ws, _ := s.Workspace(bg, "w1")
	wantRule(t, s.RemoveWorkspace(bg, ws, domain.RemovedWorkspaceEvent(ws, wsNow)), RuleInUse)

	// An agent with an unfinished task stays; once the task is finished it goes.
	agg := domain.NewTaskAggregate(domain.Task{ID: "t1", Repo: "a/b", Issue: "1", State: domain.TaskQueued, AgentID: "a1", CreatedAt: wsNow})
	if _, err := s.SaveTask(bg, agg); err != nil {
		t.Fatal(err)
	}
	ag, _ := s.Agent(bg, "a1")
	wantRule(t, s.RemoveAgent(bg, ag, domain.RemovedAgentEvent(ag, wsNow)), RuleInUse)
	if err := agg.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, agg); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAgent(bg, ag, domain.RemovedAgentEvent(ag, wsNow)); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveWorkspace(bg, ws, domain.RemovedWorkspaceEvent(ws, wsNow)); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Workspaces(bg); len(list) != 0 {
		t.Errorf("workspaces after removal: %+v", list)
	}
}

func TestTasksAndRunsKeepTheirAgent(t *testing.T) {
	s := openTemp(t)
	agg := domain.NewTaskAggregate(domain.Task{ID: "t1", Repo: "a/b", Issue: "1", State: domain.TaskQueued, AgentID: "a1", CreatedAt: wsNow})
	agg.AddEnvironment(domain.Environment{ID: "e1", Backend: "fake", State: domain.EnvRunning})
	if err := agg.StartRun(domain.Run{ID: "r1", WorkspaceID: "w1", AgentID: "a1", EnvID: "e1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTask(bg, agg); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadTask(bg, "t1")
	if err != nil {
		t.Fatal(err)
	}
	run, _ := got.LiveRun()
	if got.Task().AgentID != "a1" || run.AgentID != "a1" {
		t.Errorf("agent after a reload: task %q run %q", got.Task().AgentID, run.AgentID)
	}
	live, err := s.LiveRuns(bg, "e1")
	if err != nil || len(live) != 1 || live[0].AgentID != "a1" || live[0].TaskID != "t1" {
		t.Errorf("live runs = %+v, %v", live, err)
	}
	if other, _ := s.LiveRuns(bg, "e2"); len(other) != 0 {
		t.Errorf("another environment: %+v", other)
	}
	if err := domain.CheckEnvironmentFree("e1", live); err == nil {
		t.Error("the environment must be busy")
	}
}

// A task row made before agents existed has no agent_id and loads with none.
func TestExistingTasksLoad(t *testing.T) {
	s := openTemp(t)
	if _, err := s.db.ExecContext(bg, `INSERT INTO tasks (id, version, repo, issue, state, created_at) VALUES ('old', 1, 'a/b', '7', 'queued', 0)`); err != nil {
		t.Fatal(err)
	}
	agg, err := s.LoadTask(bg, "old")
	if err != nil || agg.Task().AgentID != "" || agg.Task().Issue != "7" {
		t.Errorf("old task: %+v, %v", agg, err)
	}
}

// A workspace's environment serves its tasks one after the other, so one
// environment ID is in several tasks.
func TestOneEnvironmentServesSeveralTasks(t *testing.T) {
	s := openTemp(t)
	for _, id := range []domain.ID{"t1", "t2"} {
		agg := domain.NewTaskAggregate(domain.Task{ID: id, Repo: "a/b", Issue: "1", State: domain.TaskQueued, CreatedAt: wsNow})
		agg.AddEnvironment(domain.Environment{ID: "shared", Backend: "fake", State: domain.EnvRunning})
		if _, err := s.SaveTask(bg, agg); err != nil {
			t.Fatalf("task %s: %v", id, err)
		}
	}
	for _, id := range []domain.ID{"t1", "t2"} {
		agg, err := s.LoadTask(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := agg.Environment("shared"); !ok {
			t.Errorf("task %s lost its environment", id)
		}
	}
}

func TestTasksListsNewestFirstAndFiltersTheFinished(t *testing.T) {
	s := openTemp(t)
	for i, st := range []domain.TaskState{domain.TaskQueued, domain.TaskCompleted, domain.TaskRunning} {
		id := domain.ID([]string{"t1", "t2", "t3"}[i])
		agg := domain.NewTaskAggregate(domain.Task{ID: id, Repo: "a/b", Issue: "#" + string(id), State: st, AgentID: "a1", CreatedAt: wsNow.Add(time.Duration(i) * time.Minute)})
		if _, err := s.SaveTask(bg, agg); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.Tasks(bg, false)
	if err != nil || len(all) != 3 || all[0].ID != "t3" || all[2].ID != "t1" || all[0].AgentID != "a1" {
		t.Errorf("all = %+v, %v", all, err)
	}
	active, err := s.Tasks(bg, true)
	if err != nil || len(active) != 2 || active[0].ID != "t3" || active[1].ID != "t1" {
		t.Errorf("active = %+v, %v", active, err)
	}
}
