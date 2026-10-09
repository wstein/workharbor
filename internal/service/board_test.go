package service

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/forge/forgetest"
)

func TestTaskStatesMapToBoardStatuses(t *testing.T) {
	t.Parallel()
	for state, want := range map[domain.TaskState]string{
		domain.TaskAwaitingGuidance: forge.StatusNeedsYou,
		domain.TaskRunning:          forge.StatusInProgress,
		domain.TaskReadyForReview:   forge.StatusNeedsYou,
		domain.TaskCompleted:        forge.StatusDone,
		domain.TaskFailed:           forge.StatusNeedsYou, // a human has to look
		domain.TaskCancelled:        forge.StatusTodo,     // no card stays In progress for a stopped task
		domain.TaskQueued:           "",
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
	t.Parallel()
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

	must(t, r.svc.AnswerDecision(userContext(), d.ID, domain.Response{By: "werner", Option: domain.AnswerAllow, At: r.clock.now}))
	<-res
	r.svc.WaitBoard()
	cards = fake.CardsSeen()
	if len(cards) != 2 || cards[1].Update.Status != forge.StatusInProgress {
		t.Errorf("cards %+v, want In progress after the answer", cards)
	}
}

// A board write that fails is reported and affects nothing else.
func TestAFailingBoardWriteNeverAffectsTheTask(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	fake := forgetest.NewFake()
	fake.CardErr = errors.New("github: POST /graphql: 503")
	r.svc.cfg.Board = fake
	r.live()

	res := r.ask(bg, "ls")
	d := r.openApproval()
	must(t, r.svc.AnswerDecision(userContext(), d.ID, domain.Response{By: "werner", Option: domain.AnswerAllow, At: r.clock.now}))
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
	if len(r.reported()) != 2 {
		t.Errorf("reported %v, want both failures", r.reported())
	}
}

// Only a task that started from an issue has a card, and only a state with a
// status changes it; the last change of a batch wins.
func TestOnlyTasksFromIssuesAndStatesWithAStatusAreMirrored(t *testing.T) {
	t.Parallel()
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
	r.svc.mirror([]domain.Event{stateEvent("queued"), stateEvent("not-a-state")})
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
	t.Parallel()
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
	must(t, r.svc.writeCard(boardJob{task: "t7", status: forge.StatusNeedsYou}))
	got := fake.CardsSeen()
	if len(got) != 1 || got[0].Update.Session != "docs-ws/runtime" || got[0].Update.Status != forge.StatusNeedsYou || got[0].Issue != 7 {
		t.Errorf("cards %+v", got)
	}
}

// Shutdown writes what is queued and does not hang on a board that does not answer.
func TestShutdownDoesNotWaitForeverForTheBoard(t *testing.T) {
	t.Parallel()
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

// The board's worker goes with its service: after Shutdown no boardWorker goroutine
// is left (they piled up across tests and starved `make race`), a late update is
// dropped instead of sent on a closed queue, and Shutdown twice is harmless.
func TestShutdownStopsTheBoardWorker(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	fake := forgetest.NewFake()
	r.svc.cfg.Board = fake
	r.svc.mirror([]domain.Event{stateEvent("running")})
	r.svc.WaitBoard()
	if len(fake.CardsSeen()) != 1 {
		t.Fatalf("cards %+v", fake.CardsSeen())
	}
	if !boardWorkerRunning() {
		t.Fatal("the test did not start a worker, so it proves nothing")
	}
	r.svc.Shutdown()
	if boardWorkerRunning() {
		t.Error("a boardWorker goroutine outlived Shutdown")
	}
	r.svc.mirror([]domain.Event{stateEvent("completed")}) // a late update: dropped, no panic
	r.svc.Shutdown()                                      // twice is harmless
	if len(fake.CardsSeen()) != 1 {
		t.Errorf("a card was written after Shutdown: %+v", fake.CardsSeen())
	}
}

// boardWorkerRunning says whether some service's board worker goroutine exists in
// this process; it waits a moment for one that is still returning.
func boardWorkerRunning() bool {
	for range 40 {
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		if !strings.Contains(string(buf), "(*Service).boardWorker") {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
	return true
}
