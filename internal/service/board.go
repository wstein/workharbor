package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
)

// boardStatus maps a task state to its card status (D30): a task awaiting
// guidance goes first, and states that need no card change map to nothing.
func boardStatus(state domain.TaskState) string {
	switch state {
	case domain.TaskAwaitingGuidance:
		return forge.StatusNeedsYou
	case domain.TaskRunning:
		return forge.StatusInProgress
	case domain.TaskReadyForReview:
		return forge.StatusReadyToPush
	case domain.TaskCompleted:
		return forge.StatusDone
	}
	return ""
}

type boardJob struct {
	task   domain.ID
	status string
}

// boardQueue keeps the project board current without ever holding up a task or
// failing it: the supervisor writes the card from a worker, in order, and a
// write that fails is only reported (design D30).
type boardQueue struct {
	once sync.Once
	jobs chan boardJob
	wg   sync.WaitGroup
}

const boardQueueSize = 64

// mirror queues the card updates that saved events call for: the last task
// state change of each task among them wins. It runs for every saved event, so
// a change made by any path reaches the board.
func (s *Service) mirror(events []domain.Event) {
	if s.cfg.Board == nil {
		return
	}
	last := map[domain.ID]string{}
	var order []domain.ID
	for _, e := range events {
		if e.Kind != domain.EventTaskState {
			continue
		}
		var p domain.StateChanged
		if json.Unmarshal(e.Payload, &p) != nil || p.Object != "task" {
			continue
		}
		if st := boardStatus(domain.TaskState(p.To)); st != "" {
			if _, seen := last[e.TaskID]; !seen {
				order = append(order, e.TaskID)
			}
			last[e.TaskID] = st
		}
	}
	for _, task := range order {
		s.enqueueCard(boardJob{task: task, status: last[task]})
	}
}

func (s *Service) enqueueCard(j boardJob) {
	s.board.once.Do(func() {
		s.board.jobs = make(chan boardJob, boardQueueSize)
		go s.boardWorker()
	})
	s.board.wg.Add(1)
	select {
	case s.board.jobs <- j:
	default:
		s.board.wg.Done()
		s.report(errBoardQueueFull)
	}
}

var errBoardQueueFull = &domain.InvalidError{Msg: "the board's queue is full: a card update was dropped"}

func (s *Service) boardWorker() {
	for j := range s.board.jobs {
		s.report(s.writeCard(j))
		s.board.wg.Done()
	}
}

// writeCard reads the task, resolves the agent's name and writes the card. A
// task that did not start from an issue has no card.
func (s *Service) writeCard(j boardJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agg, err := s.store.LoadTask(ctx, j.task)
	if err != nil {
		return err
	}
	task := agg.Task()
	issue, err := strconv.Atoi(strings.TrimPrefix(task.Issue, "#"))
	if err != nil || task.Repo == "" {
		return nil
	}
	u := forge.CardUpdate{Status: j.status}
	if task.AgentID != "" {
		if a, err := s.store.Agent(ctx, task.AgentID); err == nil {
			if ws, err := s.store.Workspace(ctx, string(a.WorkspaceID)); err == nil {
				u.Session = ws.Name + "/" + a.Role
			}
		}
	}
	if s.cfg.BoardLink != nil {
		u.Link = s.cfg.BoardLink(task.ID)
	}
	return s.cfg.Board.UpdateCard(ctx, task.Repo, issue, u)
}

// WaitBoard blocks until the card updates queued so far are written. Tests use
// it; the supervisor never waits for the board.
func (s *Service) WaitBoard() { s.board.wg.Wait() }
