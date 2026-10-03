package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewWorkspace(t *testing.T) {
	tests := []struct {
		name, wsName, path, repo, integ string
		rule                            Rule // wanted conflict, "" for an invalid-input error, "ok" for success
	}{
		{"valid", "main-ws", "/Users/h/ws/main", "wstein/workharbor", "main", "ok"},
		{"develop branch", "w", "/ws", "a/b", "develop", "ok"},
		{"upper case name", "Main", "/ws", "a/b", "main", RuleWorkspaceName},
		{"name with slash", "a/b", "/ws", "a/b", "main", RuleWorkspaceName},
		{"name starts with digit", "1a", "/ws", "a/b", "main", RuleWorkspaceName},
		{"name too long", strings.Repeat("a", 41), "/ws", "a/b", "main", RuleWorkspaceName},
		{"relative path", "w", "ws", "a/b", "main", ""},
		{"unclean path", "w", "/ws/../etc", "a/b", "main", ""},
		{"trailing slash", "w", "/ws/", "a/b", "main", ""},
		{"repo without owner", "w", "/ws", "workharbor", "main", ""},
		{"repo with a flag", "w", "/ws", "-x/y", "main", ""},
		{"agent namespace", "w", "/ws", "a/b", "agent/x", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, ev, err := NewWorkspace("w1", tc.wsName, tc.path, tc.repo, tc.integ, t0)
			if tc.rule == "ok" {
				if err != nil {
					t.Fatal(err)
				}
				if w.Name != tc.wsName || ev.Kind != EventWorkspaceAdded || ev.TaskID != "workspace:w1" || ev.Tier != TierAudit {
					t.Errorf("workspace %+v event %+v", w, ev)
				}
				return
			}
			if err == nil {
				t.Fatalf("accepted %+v", tc)
			}
			var c *ConflictError
			if tc.rule != "" && (!errors.As(err, &c) || c.Rule != tc.rule) {
				t.Errorf("err = %v, want conflict %s", err, tc.rule)
			}
		})
	}
}

func TestNewAgentDerivesBranchAndWorktreeFromTheRole(t *testing.T) {
	a, ev, err := NewAgent("a1", "w1", "docs", "write docs", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Branch != "agent/docs" || a.Worktree != "/ws/wt/docs" || ev.Kind != EventAgentAdded || ev.TaskID != "workspace:w1" {
		t.Errorf("agent %+v event %+v", a, ev)
	}
	for _, role := range []string{"", "Docs", "a b", "../x", "a/b", "-x", "a.b", strings.Repeat("a", 31), "docs\n"} {
		if _, _, err := NewAgent("a1", "w1", role, "", "", t0); err == nil {
			t.Errorf("role %q was accepted", role)
		}
	}
	if _, _, err := NewAgent("", "w1", "docs", "", "", t0); err == nil {
		t.Error("an agent without an ID was accepted")
	}
}

func TestCheckEnvironmentFree(t *testing.T) {
	runs := []Run{
		{ID: "r1", EnvID: "e1", AgentID: "a1", State: RunStopped},
		{ID: "r2", EnvID: "e1", AgentID: "a1", State: RunFailed},
		{ID: "r3", EnvID: "e2", AgentID: "a2", State: RunRunning},
	}
	if err := CheckEnvironmentFree("e1", runs); err != nil {
		t.Errorf("finished runs hold nothing: %v", err)
	}
	for _, state := range []RunState{RunStarting, RunRunning, RunPaused, RunInterrupted} {
		held := append(append([]Run{}, runs...), Run{ID: "r4", EnvID: "e1", AgentID: "a2", State: state})
		var c *ConflictError
		if err := CheckEnvironmentFree("e1", held); !errors.As(err, &c) || c.Rule != RuleEnvBusy {
			t.Errorf("a %s run must hold the environment: %v", state, err)
		}
	}
	if err := CheckEnvironmentFree("e3", runs); err != nil {
		t.Errorf("another environment is free: %v", err)
	}
	// an interrupted run still owns its environment: it is resumed there (#216)
	if (Run{State: RunInterrupted}).Live() || !(Run{State: RunInterrupted}).Owns() {
		t.Error("an interrupted run is not live but owns its environment")
	}
}

func TestStartRunNeedsTheTasksAgent(t *testing.T) {
	a := NewTaskAggregate(Task{ID: "t1", Repo: "o/r", Issue: "1", State: TaskQueued, AgentID: "a1"})
	a.AddEnvironment(Environment{ID: "e1", Backend: "fake", State: EnvRunning})
	wantConflict(t, a.StartRun(Run{ID: "r1", EnvID: "e1", AgentID: "a2"}), RuleRunAgent)
	if err := a.StartRun(Run{ID: "r1", EnvID: "e1", AgentID: "a1"}); err != nil {
		t.Fatal(err)
	}
}

func TestNewWorkspaceIntegrationBranch(t *testing.T) {
	for _, tc := range []struct {
		branch string
		ok     bool
	}{
		{"main", true},
		{"develop", true},
		{"master", true},
		{"trunk", true},
		{"release/1.x", true},
		{"staging", true},
		{"agent/x", false},
		{"-x", false},
		{"HEAD", false},
		{"a..b", false},
		{"", false},
		{"a\x01b", false},
		{"a\nb", false},
	} {
		_, _, err := NewWorkspace("id", "w", "/ws", "a/b", tc.branch, time.Time{})
		if (err == nil) != tc.ok {
			t.Errorf("branch %q: err = %v, want ok = %v", tc.branch, err, tc.ok)
		}
	}
}
