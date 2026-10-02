package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

func TestKillAllStopsEveryRunCancelsTheTasksAndRevokesTokens(t *testing.T) {
	r := newRig(t)
	r.live() // a session is attached to run r1 of task t1
	// A second unfinished task, with no run.
	other := domain.NewTaskAggregate(domain.Task{ID: "t2", Repo: "wstein/other", Issue: "#2", State: domain.TaskQueued, CreatedAt: t0})
	if _, err := r.store.SaveTask(bg, other); err != nil {
		t.Fatal(err)
	}
	// A finished task is left alone.
	done := domain.NewTaskAggregate(domain.Task{ID: "t3", Repo: "wstein/other", Issue: "#3", State: domain.TaskCompleted, CreatedAt: t0})
	if _, err := r.store.SaveTask(bg, done); err != nil {
		t.Fatal(err)
	}
	revoked := 0
	r.svc.cfg.RevokeTokens = func(context.Context) (int, error) { revoked++; return 2, nil }

	rep, err := r.svc.KillAll(bg, "werner")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cancelled) != 2 || rep.TokensRevoked != 2 || len(rep.Problems) != 0 || revoked != 1 {
		t.Fatalf("report = %+v, revoke calls %d", rep, revoked)
	}
	for _, id := range []domain.ID{"t1", "t2"} {
		a, err := r.store.LoadTask(bg, id)
		if err != nil || a.Task().State != domain.TaskCancelled {
			t.Errorf("task %s = %v, %v; want cancelled", id, a.Task().State, err)
		}
	}
	if r.runState() != domain.RunStopped {
		t.Errorf("run r1 = %s, want stopped", r.runState())
	}
	if a, _ := r.store.LoadTask(bg, "t3"); a.Task().State != domain.TaskCompleted {
		t.Errorf("a finished task changed: %s", a.Task().State)
	}

	evs, err := r.store.EventsSince(bg, domain.SupervisorStream, 0, 10)
	if err != nil || len(evs) != 1 || evs[0].Kind != domain.EventKillAll || evs[0].Tier != domain.TierAudit {
		t.Fatalf("audit = %+v, %v", evs, err)
	}
	var k domain.KillAll
	if err := json.Unmarshal(evs[0].Payload, &k); err != nil || k.Actor != "werner" || k.TokensRevoked != 2 || len(k.Cancelled) != 2 {
		t.Errorf("payload = %+v, %v", k, err)
	}
}

func TestKillAllGoesOnAfterAFailureAndSaysWhatFailed(t *testing.T) {
	r := newRig(t)
	r.svc.cfg.RevokeTokens = func(context.Context) (int, error) { return 0, errors.New("revoke the token for a/b: 500") }
	rep, err := r.svc.KillAll(bg, "werner")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cancelled) != 1 || len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "revoke the token") {
		t.Errorf("report = %+v: the task is cancelled and the failed revoke is reported", rep)
	}
	evs, _ := r.store.EventsSince(bg, domain.SupervisorStream, 0, 10)
	if len(evs) != 1 || !strings.Contains(string(evs[0].Payload), "revoke the token") {
		t.Errorf("the audit entry must carry the problem: %+v", evs)
	}
}

func TestKillAllWithNothingRunningStillWritesTheAuditEntry(t *testing.T) {
	r := newRig(t)
	must(t, r.svc.Cancel(bg, "t1"))
	rep, err := r.svc.KillAll(bg, "werner")
	if err != nil || len(rep.Cancelled) != 0 || len(rep.Problems) != 0 {
		t.Fatalf("report = %+v, %v", rep, err)
	}
	if evs, _ := r.store.EventsSince(bg, domain.SupervisorStream, 0, 10); len(evs) != 1 {
		t.Errorf("audit entries = %d, want 1", len(evs))
	}
	if b, _ := json.Marshal(rep); !strings.Contains(string(b), `"cancelled":[]`) || !strings.Contains(string(b), `"problems":[]`) {
		t.Errorf("an empty report has empty lists: %s", b)
	}
}
