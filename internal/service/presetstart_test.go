package service

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
)

// A task whose stored preset cannot be read does not start a run: neither the
// egress gate (which would keep old answers) nor the agent's permission mode
// (which would fall back to the repository's) decides on a guess (§6, #256).
// The run fails and nothing starts; a retry cannot fix a corrupt record.
func TestAnUnreadableStoredPresetRefusesTheRunAtItsStart(t *testing.T) {
	t.Parallel()
	for _, egress := range []bool{true, false} {
		r := newWsRig(t)
		if egress {
			r.withEgressRequests() // the gate reads the preset
		} // else the agent start reads it
		_, a := r.create("ws")
		r.ws.cfg.Workflow = func(string) string { return "removed-preset" } // a task now records it
		var ids []domain.ID
		next := r.ws.cfg.NewID
		r.ws.cfg.NewID = func() domain.ID { id := next(); ids = append(ids, id); return id }
		_, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#7"})
		task, run := ids[0], ids[1] // StartTask takes the task's ID, then the run's
		if err == nil || !strings.Contains(err.Error(), "removed-preset") || !strings.Contains(err.Error(), "preset") {
			t.Fatalf("egress %v: error %v, want one that names the preset", egress, err)
		}
		if r.agent.Started() != 0 {
			t.Errorf("egress %v: the agent started", egress)
		}
		if egress && len(r.openEgress(task)) != 0 {
			t.Errorf("egress %v: egress requests were raised for a refused run", egress)
		}
		agg, _ := r.store.LoadTask(bg, task)
		if rn, _ := agg.Run(run); rn.State != domain.RunFailed {
			t.Errorf("egress %v: run is %s, want failed", egress, rn.State)
		}
	}
}

// An empty stored preset is a task from before presets: it still means none.
func TestAnEmptyStoredPresetStartsAsBefore(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.ws.cfg.Workflow = func(string) string { return "" } // workflowOf records the default
	r.withEgressRequests()
	_, a := r.create("ws")
	if _, _, err := r.ws.StartTask(userContext(), StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Fatal(err)
	}
}

// The unreadable preset refuses a resume too, and the refusal counts as an
// attempt, so the run fails when they are used up instead of looping.
func TestAnUnreadableStoredPresetRefusesAResumeAndCountsAnAttempt(t *testing.T) {
	t.Parallel()
	r := newRig(t, withTask(func(tk *domain.Task) { tk.Workflow = "removed-preset" }))
	r.svc.cfg.MaxAttempts = 2
	launched := len(r.agent.Specs)

	rep := r.reconcileAndResume()
	run, _ := r.load().Run("r1")
	if len(rep.Resumed) != 0 || len(rep.Errors) == 0 || run.State != domain.RunInterrupted || run.ResumeAttempts != 1 || len(r.agent.Specs) != launched {
		t.Fatalf("report %+v, run %s (%d attempts), launched %d to %d", rep, run.State, run.ResumeAttempts, launched, len(r.agent.Specs))
	}
	if !strings.Contains(rep.Errors[0].Error(), "removed-preset") {
		t.Errorf("the error does not name the preset: %v", rep.Errors[0])
	}
	rep = r.reconcileAndResume()
	if r.runState() != domain.RunFailed || len(rep.Failed) != 1 || len(r.agent.Specs) != launched {
		t.Errorf("after the attempts: run %s, report %+v", r.runState(), rep)
	}
}
