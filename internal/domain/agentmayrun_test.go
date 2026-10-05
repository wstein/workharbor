package domain

import (
	"testing"
	"time"
)

func TestAgentMayRunNoticeSurvivesAndOnlyAcknowledges(t *testing.T) {
	for _, path := range []string{"cancel", "pause-resume", "interrupt-resume", "stop", "budget"} {
		t.Run(path, func(t *testing.T) {
			a := runningTask(t)
			d, err := a.RaiseAgentMayRun("notice", AgentMayRun{RunID: "r1", EnvID: "e1", Path: "suspension", Error: "<untrusted>"}, tNow)
			if err != nil {
				t.Fatal(err)
			}
			if d.Blocking || !d.Deadline.IsZero() || len(d.Options) != 1 || d.Options[0] != AnswerSeen || a.Task().State != TaskRunning {
				t.Fatalf("notice: %+v", d)
			}
			switch path {
			case "cancel":
				err = a.Cancel()
			case "pause-resume":
				err = a.Pause("r1")
				if err == nil {
					err = a.Resume("r1")
				}
			case "interrupt-resume":
				err = a.Interrupt("r1")
				if err == nil {
					err = a.Resume("r1")
				}
			case "stop":
				err = a.StopRun("r1")
			case "budget":
				err = a.ExceedBudget(BudgetBreach{Scope: BudgetTask, Metric: BudgetTokens, Limit: 1, Used: 2})
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.mustDecision("notice").Status != DecisionOpen {
				t.Fatal("notice superseded")
			}
			before := a.Task().State
			if err = a.Answer("notice", Response{Option: AnswerSeen, By: "werner", At: tNow.Add(time.Second)}); err != nil {
				t.Fatal(err)
			}
			if a.Task().State != before || a.mustDecision("notice").AnsweredBy != "werner" {
				t.Fatal("acknowledgement changed task or lost actor")
			}
		})
	}
}
