package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/forge/forgetest"
)

func TestTaskStatesMapToBoardStatuses(t *testing.T) {
	for state, want := range map[domain.TaskState]string{
		domain.TaskAwaitingGuidance: forge.StatusNeedsYou,
		domain.TaskRunning:          forge.StatusInProgress,
		domain.TaskReadyForReview:   forge.StatusReadyToPush,
		domain.TaskCompleted:        forge.StatusDone,
		domain.TaskQueued:           "",
		domain.TaskCancelled:        "",
		domain.TaskFailed:           "",
	} {
		if got := boardStatus(state); got != want {
			t.Errorf("%s -> %q, want %q", state, got, want)
		}
	}
}

func stateEvent(to string) domain.Event {
	p, _ := json.Marshal(domain.StateChanged{Object: "task", ID: "t1", To: to})
	return domain.Event{TaskID: "t1", Kind: domain.EventTaskState, Payload: p}
}

// A task awaiting guidance moves its card to "Needs you" and back when it is
// answered; the card names the repository's issue and links the task (D30).
func TestTheBoardFollowsTheTaskAndNeedsYouComesFirst(t *testing.T) {
	r := newRig(t)
	fake := forgetest.NewFake()
	r.svc.cfg.Board = fake
	r.svc.cfg.BoardLink = func(task domain.ID) string { return "https://whr.example.test/tasks/" + string(task) }
	r.live()

	res := r.ask(bg, "make deploy")
	d := r.openApproval()
	r.svc.WaitBoard()
	cards := fake.CardsSeen()
	if len(cards) != 1 || cards[0].Update.Status != forge.StatusNeedsYou || cards[0].Repo != "wstein/workharbor" || cards[0].Issue != 23 ||
		cards[0].Update.Link != "https://whr.example.test/tasks/t1" {
		t.Fatalf("cards %+v, want one Needs you for wstein/workharbor#23 with the task link", cards)
	}

	must(t, r.svc.AnswerDecision(bg, d.ID, domain.Response{By: "werner", Option: domain.AnswerAllow, At: r.clock.now}))
	<-res
	r.svc.WaitBoard()
	cards = fake.CardsSeen()
	if len(cards) != 2 || cards[1].Update.Status != forge.StatusInProgress {
		t.Errorf("cards %+v, want In progress after the answer", cards)
	}
}

// A board write that fails is reported and affects nothing else.
func TestAFailingBoardWriteNeverAffectsTheTask(t *testing.T) {
	r := newRig(t)
	fake := forgetest.NewFake()
	fake.CardErr = errors.New("github: POST /graphql: 503")
	r.svc.cfg.Board = fake
	r.live()

	res := r.ask(bg, "ls")
	d := r.openApproval()
	must(t, r.svc.AnswerDecision(bg, d.ID, domain.Response{By: "werner", Option: domain.AnswerAllow, At: r.clock.now}))
	if got := <-res; got.err != nil || !got.a.Allow {
		t.Errorf("the agent was told %+v (%v): a board failure must not change the answer", got.a, got.err)
	}
	r.svc.WaitBoard()
	if st := r.load().Task().State; st != domain.TaskRunning {
		t.Errorf("task %s, want running", st)
	}
	if len(fake.CardsSeen()) != 2 {
		t.Errorf("cards %+v: a failed write must not stop the later ones", fake.CardsSeen())
	}
	if len(r.errs) != 2 {
		t.Errorf("reported %v, want both failures", r.errs)
	}
}

// Only a task that started from an issue has a card, and only a state with a
// status changes it; the last change of a batch wins.
func TestOnlyTasksFromIssuesAndStatesWithAStatusAreMirrored(t *testing.T) {
	r := newRig(t)
	fake := forgetest.NewFake()
	r.svc.cfg.Board = fake

	// a task with no issue (a bare task of the older checkout path) has no card
	a := domain.NewTaskAggregate(domain.Task{ID: "t-bare", Repo: "wstein/workharbor", State: domain.TaskRunning, CreatedAt: t0})
	_, err := r.store.SaveTask(bg, a)
	must(t, err)
	bare := stateEvent("running")
	bare.TaskID = "t-bare"
	r.svc.mirror([]domain.Event{bare})
	// states that map to no status, and events that are not task states
	r.svc.mirror([]domain.Event{stateEvent("cancelled"), stateEvent("failed"), stateEvent("queued")})
	run := domain.Event{TaskID: "t1", Kind: domain.EventRunState, Payload: []byte(`{"object":"run","id":"r1","to":"running"}`)}
	r.svc.mirror([]domain.Event{run})
	r.svc.WaitBoard()
	if got := fake.CardsSeen(); len(got) != 0 {
		t.Fatalf("cards %+v, want none", got)
	}

	r.svc.mirror([]domain.Event{stateEvent("awaiting_guidance"), stateEvent("running")})
	r.svc.WaitBoard()
	if got := fake.CardsSeen(); len(got) != 1 || got[0].Update.Status != forge.StatusInProgress || got[0].Issue != 23 {
		t.Errorf("cards %+v, want the last change, In progress for #23", got)
	}
}

// The card's Session is the agent as <workspace>/<role>, read from the records.
func TestTheCardNamesTheAgent(t *testing.T) {
	r := newRig(t)
	ws, wev, err := domain.NewWorkspace("w9", "docs-ws", "/ws/docs", "wstein/workharbor", "main", t0)
	must(t, err)
	must(t, r.store.AddWorkspace(bg, ws, wev))
	ag, aev, err := domain.NewAgent("a9", ws.ID, "runtime", "", "", t0)
	must(t, err)
	must(t, r.store.AddAgent(bg, ag, aev))
	fake := forgetest.NewFake()
	r.svc.cfg.Board = fake

	a := domain.NewTaskAggregate(domain.Task{ID: "t7", Repo: "wstein/workharbor", Issue: "#7", AgentID: "a9", State: domain.TaskRunning, CreatedAt: t0})
	_, err = r.store.SaveTask(bg, a)
	must(t, err)
	must(t, r.svc.writeCard(boardJob{task: "t7", status: forge.StatusReadyToPush}))
	got := fake.CardsSeen()
	if len(got) != 1 || got[0].Update.Session != "docs-ws/runtime" || got[0].Update.Status != forge.StatusReadyToPush || got[0].Issue != 7 {
		t.Errorf("cards %+v", got)
	}
}

// Shutdown writes what is queued and does not hang on a board that does not answer.
func TestShutdownDoesNotWaitForeverForTheBoard(t *testing.T) {
	r := newRig(t)
	block := make(chan struct{})
	r.svc.cfg.Board = boardFunc(func(ctx context.Context, _ string, _ int, _ forge.CardUpdate) error {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return nil
	})
	r.svc.mirror([]domain.Event{stateEvent("running")})
	start := time.Now()
	r.svc.waitBoard(50 * time.Millisecond)
	if time.Since(start) > 2*time.Second {
		t.Errorf("waiting for the board took %s", time.Since(start))
	}
	close(block)
	r.svc.WaitBoard()
}

type boardFunc func(ctx context.Context, repo string, issue int, u forge.CardUpdate) error

func (f boardFunc) UpdateCard(ctx context.Context, repo string, issue int, u forge.CardUpdate) error {
	return f(ctx, repo, issue, u)
}
