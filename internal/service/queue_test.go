package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
)

const queueColumn = "Agent queue"

func (r *wsRig) withQueue() {
	r.ws.cfg.QueueStatus = queueColumn
}

func (r *wsRig) card(issue int, agent string, at time.Time) forge.QueuedCard {
	return forge.QueuedCard{Repo: "wstein/workharbor", Issue: issue, Agent: agent, UpdatedAt: at}
}

func (r *wsRig) holdOf(task domain.ID) domain.Decision {
	r.t.Helper()
	agg, err := r.store.LoadTask(bg, task)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, d := range agg.Decisions() {
		if d.Cause == domain.CauseBoardQueue {
			return d
		}
	}
	r.t.Fatalf("task %s has no board_queue decision: %+v", task, agg.Decisions())
	return domain.Decision{}
}

// A card in the queue never starts a run: it raises "Accept this task?" for the
// repository's agent, even for a trusted author, and only the human's start makes a run.
func TestACardInTheQueueAsksBeforeAnyRunStarts(t *testing.T) {
	r := newWsRig(t)
	r.create("q")
	r.withQueue()
	r.issues.Issues["wstein/workharbor#7"] = forge.Issue{Repo: "wstein/workharbor", Number: 7, Title: "Docs", Body: "write the manual", Author: "wstein", AuthorAssociation: "OWNER"}
	at := r.clock.now
	r.issues.SetQueue(r.card(7, "", at))

	n, err := r.ws.PollQueue(bg)
	if err != nil || n != 1 {
		t.Fatalf("poll = %d, %v", n, err)
	}
	if len(r.agent.Specs) != 0 {
		t.Fatal("a card started a run")
	}
	tasks, _ := r.store.Tasks(bg, true)
	if len(tasks) != 1 || tasks[0].State != domain.TaskQueued || tasks[0].Issue != "#7" {
		t.Fatalf("tasks = %+v", tasks)
	}
	d := r.holdOf(tasks[0].ID)
	if d.Status != domain.DecisionOpen || !d.Blocking || len(d.Options) != 2 || d.Options[0] != domain.AnswerStart || !strings.Contains(d.Subject, "Accept this task?") {
		t.Errorf("decision = %+v", d)
	}
	if !strings.Contains(d.Input, "moved by: unknown") || !strings.Contains(d.Input, "author: wstein (OWNER)") || !strings.Contains(d.Input, "write the manual") {
		t.Errorf("the decision does not show who and what: %q", d.Input)
	}
	agg, _ := r.store.LoadTask(bg, tasks[0].ID)
	if agg.Task().Untrusted {
		t.Error("a trusted author's issue was marked untrusted")
	}

	// the same card is not asked about again
	if n, err := r.ws.PollQueue(bg); err != nil || n != 0 {
		t.Errorf("a second poll = %d, %v", n, err)
	}
	// nothing runs until the human accepts: the answer starts one run now
	run, err := r.ws.Answer(bg, d.ID, domain.Response{By: "werner", Option: domain.AnswerStart, At: r.clock.now})
	if err != nil || run == "" {
		t.Fatalf("accept = %q, %v", run, err)
	}
	if len(r.agent.Specs) != 1 || !strings.Contains(r.agent.Specs[0].Prompt, "write the manual") {
		t.Errorf("the run did not start from the issue: %+v", r.agent.Specs)
	}
}

// A non-trusted author's issue is marked as untrusted input; declining a card does not
// ask again until the card is moved again.
func TestADeclinedCardAsksAgainOnlyWhenItIsMovedAgain(t *testing.T) {
	r := newWsRig(t)
	r.create("q")
	r.withQueue()
	r.issues.Issues["wstein/workharbor#7"] = forge.Issue{Repo: "wstein/workharbor", Number: 7, Title: "x", Body: "y", Author: "stranger", AuthorAssociation: "NONE"}
	at := r.clock.now
	r.issues.SetQueue(r.card(7, "", at))
	if n, err := r.ws.PollQueue(bg); err != nil || n != 1 {
		t.Fatalf("poll = %d, %v", n, err)
	}
	tasks, _ := r.store.Tasks(bg, true)
	d := r.holdOf(tasks[0].ID)
	agg, _ := r.store.LoadTask(bg, tasks[0].ID)
	if !agg.Task().Untrusted {
		t.Error("an issue by a stranger was not marked untrusted")
	}
	if _, err := r.ws.Answer(bg, d.ID, domain.Response{By: "werner", Option: domain.AnswerCancel, At: r.clock.now}); err != nil {
		t.Fatal(err)
	}
	if n, err := r.ws.PollQueue(bg); err != nil || n != 0 {
		t.Errorf("a declined card asked again: %d, %v", n, err)
	}
	// moved again: a new task and a new question
	r.issues.SetQueue(r.card(7, "", at.Add(time.Hour)))
	if n, err := r.ws.PollQueue(bg); err != nil || n != 1 {
		t.Errorf("a card moved again: %d, %v", n, err)
	}
	if len(r.agent.Specs) != 0 {
		t.Error("a run started without an accepted Decision")
	}
}

// An issue that already has an unfinished task is not queued twice, and a card whose
// agent cannot be found is reported once and raises nothing.
func TestAQueueCardIsSkippedOrReportedWhenItCannotBeQueued(t *testing.T) {
	r := newWsRig(t)
	r.create("q")
	r.withQueue()
	r.issues.Issues["wstein/workharbor#7"] = forge.Issue{Repo: "wstein/workharbor", Number: 7, Title: "x", Author: "w", AuthorAssociation: "OWNER"}
	r.issues.Issues["wstein/workharbor#8"] = forge.Issue{Repo: "wstein/workharbor", Number: 8, Title: "y", Author: "w", AuthorAssociation: "OWNER"}
	at := r.clock.now
	r.issues.SetQueue(r.card(7, "", at))
	if n, _ := r.ws.PollQueue(bg); n != 1 {
		t.Fatalf("the first card: %d", n)
	}
	// the same issue moved again while its task waits: still one task
	r.issues.SetQueue(r.card(7, "", at.Add(time.Hour)))
	if n, err := r.ws.PollQueue(bg); err != nil || n != 0 {
		t.Errorf("a second task for the same issue: %d, %v", n, err)
	}
	if tasks, _ := r.store.Tasks(bg, true); len(tasks) != 1 {
		t.Errorf("%d tasks", len(tasks))
	}
	// a Session that names no agent of the repository
	r.forget()
	r.issues.SetQueue(r.card(8, "nope/nobody", at))
	if n, err := r.ws.PollQueue(bg); err != nil || n != 0 {
		t.Errorf("a card with a bad Session: %d, %v", n, err)
	}
	if errs := r.reported(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "not an agent") {
		t.Errorf("reported = %v", errs)
	}
	r.forget()
	if n, _ := r.ws.PollQueue(bg); n != 0 || len(r.reported()) != 0 {
		t.Errorf("the bad card was reported again: %v", r.reported())
	}
	// a card of another repository is not ours
	r.issues.SetQueue(forge.QueuedCard{Repo: "someone/else", Issue: 1, UpdatedAt: at})
	if n, err := r.ws.PollQueue(bg); err != nil || n != 0 {
		t.Errorf("a foreign card: %d, %v", n, err)
	}
	// the trust hook still refuses an issue before any question
	r.issues.Issues["wstein/workharbor#9"] = forge.Issue{Repo: "wstein/workharbor", Number: 9, Author: "x", AuthorAssociation: "NONE"}
	r.ws.cfg.Trust = func(forge.Issue) error { return errors.New("blocked author") }
	r.issues.SetQueue(r.card(9, "", at))
	r.forget()
	if n, _ := r.ws.PollQueue(bg); n != 0 {
		t.Errorf("a refused author was asked about: %d", n)
	}
	if errs := r.reported(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "not trusted") {
		t.Errorf("reported = %v", errs)
	}
}

// Several agents and no Session on the card: nothing to choose from, so nothing is asked.
func TestACardNeedsAnAgentWhenTheRepositoryHasSeveral(t *testing.T) {
	r := newWsRig(t)
	r.create("q")
	if _, err := r.ws.AddAgent(bg, "q", "review", "", ""); err != nil {
		t.Fatal(err)
	}
	r.withQueue()
	r.issues.Issues["wstein/workharbor#7"] = forge.Issue{Repo: "wstein/workharbor", Number: 7, Title: "x", Author: "w", AuthorAssociation: "OWNER"}
	r.issues.SetQueue(r.card(7, "", r.clock.now))
	r.forget()
	if n, _ := r.ws.PollQueue(bg); n != 0 {
		t.Errorf("a card with no agent was asked about: %d", n)
	}
	if errs := r.reported(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "names none") {
		t.Errorf("reported = %v", errs)
	}
	// naming one makes it askable
	r.issues.SetQueue(r.card(7, "q/review", r.clock.now.Add(time.Hour)))
	if n, err := r.ws.PollQueue(bg); err != nil || n != 1 {
		t.Errorf("a card that names its agent: %d, %v", n, err)
	}
	// the column is off when it is not configured
	r.ws.cfg.QueueStatus = ""
	r.issues.SetQueue(r.card(8, "", r.clock.now.Add(2*time.Hour)))
	if n, err := r.ws.PollQueue(bg); err != nil || n != 0 {
		t.Errorf("an unconfigured queue: %d, %v", n, err)
	}
}
