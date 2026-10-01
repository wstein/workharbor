package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
)

// ---- the live event bus ----

// subBuffer is how many events a subscriber may be behind before it is dropped.
const subBuffer = 256

type subscriber struct {
	ch   chan domain.Event
	task domain.ID
}

// bus delivers events to subscribers from memory. A subscriber that falls
// behind is closed rather than waited for: it reconnects with the last
// sequence number it saw and replays the durable events (design §5.3).
type bus struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func (b *bus) add(task domain.ID) *subscriber {
	s := &subscriber{ch: make(chan domain.Event, subBuffer), task: task}
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[*subscriber]struct{}{}
	}
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *bus) remove(s *subscriber) {
	b.mu.Lock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.ch)
	}
	b.mu.Unlock()
}

func (b *bus) publish(events ...domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range events {
		for s := range b.subs {
			if s.task != "" && s.task != e.TaskID {
				continue
			}
			select {
			case s.ch <- e:
			default: // too slow: drop it
				delete(b.subs, s)
				close(s.ch)
			}
		}
	}
}

// publish hands saved events to subscribers. The caller has committed them.
func (s *Service) publish(events []domain.Event) { s.bus.publish(events...) }

// PublishEphemeral sends an event to subscribers of a task from memory, with no
// sequence number, and never stores it: token deltas and heartbeats (§5.4).
func (s *Service) PublishEphemeral(task domain.ID, kind domain.EventKind, payload []byte) {
	s.bus.publish(domain.Event{TaskID: task, Kind: kind, Tier: domain.TierEphemeral, Payload: payload, At: s.clock.Now()})
}

// record stores one observation of the agent as a transcript-tier event and
// publishes it. A message or a tool call that cannot be stored is reported, not
// fatal: the run goes on.
func (s *Service) record(ctx context.Context, task domain.ID, e agent.Event) {
	payload, err := json.Marshal(e)
	if err != nil {
		s.report(fmt.Errorf("transcript event: %w", err))
		return
	}
	saved, err := s.store.Append(ctx, domain.NewTranscriptEvent(task, payload, e.At))
	if err != nil {
		s.report(fmt.Errorf("transcript event: %w", err))
		return
	}
	s.publish(saved)
}

// Subscribe returns the events of a task after sequence number since: first the
// durable ones from the store, then the live ones as they happen, among them
// ephemeral events that have no sequence number. The channel is closed when ctx
// ends or when the subscriber falls too far behind; a client then reconnects
// with the last sequence number it saw.
func (s *Service) Subscribe(ctx context.Context, task domain.ID, since int64) (<-chan domain.Event, error) {
	sub := s.bus.add(task) // registered first, so nothing between the replay and the live feed is lost
	out := make(chan domain.Event, subBuffer)
	last := since
	// Replay everything stored up to now.
	for {
		page, err := s.store.EventsSince(ctx, task, last, 500)
		if err != nil {
			s.bus.remove(sub)
			return nil, err
		}
		for _, e := range page {
			select {
			case out <- e:
			default:
				s.bus.remove(sub)
				return nil, errors.New("subscribe: the replay is longer than the buffer: ask for a later sequence number")
			}
			last = e.Seq
		}
		if len(page) < 500 {
			break
		}
	}
	go func() {
		defer close(out)
		defer s.bus.remove(sub)
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-sub.ch:
				if !ok {
					return
				}
				if e.Seq != 0 && e.Seq <= last {
					continue // already replayed
				}
				if e.Seq != 0 {
					last = e.Seq
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// ---- say, list, show ----

// Say sends a message to the task's running agent and returns how it was
// delivered (design §5.3): an agent without mid-run instruction answers
// `resumed_turn`, and that is what is shown, never an injection it cannot do.
// The message and its delivery are recorded as an audit event. A task with no
// attached session is a conflict.
func (s *Service) Say(ctx context.Context, task domain.ID, message string) (agent.Delivery, error) {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return "", err
	}
	run, ok := agg.LiveRun()
	if !ok || run.State != domain.RunRunning {
		return "", domain.NewConflict(domain.RuleRunLive, "task %s has no running run to send a message to", task)
	}
	s.mu.Lock()
	sl := s.sessions[run.ID]
	s.mu.Unlock()
	if sl == nil || sl.sess == nil {
		return "", domain.NewConflict(domain.RuleRunLive, "run %s has no attached session", run.ID)
	}
	delivery, err := sl.sess.Instruct(ctx, message)
	if err != nil {
		return "", err
	}
	saved, aerr := s.store.Append(ctx, domain.NewInstructionEvent(task, run.ID, string(delivery), message, s.clock.Now()))
	if aerr != nil {
		return delivery, fmt.Errorf("the message was sent (%s) but its record failed: %w", delivery, aerr)
	}
	s.publish(saved)
	return delivery, nil
}

// List returns a summary of every task, newest first.
func (s *Service) List(ctx context.Context, onlyActive bool) ([]store.TaskSummary, error) {
	return s.store.Tasks(ctx, onlyActive)
}

// TaskView is one task as `whr show` and the API present it.
type TaskView struct {
	Task      domain.Task
	Runs      []domain.Run
	Open      []domain.Decision // the Decisions waiting for the human
	Candidate *domain.ReviewCandidate
}

// Show returns a task with its runs, its open Decisions and its current
// revision. An unknown task is a *domain.NotFoundError.
func (s *Service) Show(ctx context.Context, task domain.ID) (TaskView, error) {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return TaskView{}, err
	}
	v := TaskView{Task: agg.Task(), Runs: agg.Runs()}
	for _, d := range agg.Decisions() {
		if d.Status == domain.DecisionOpen {
			v.Open = append(v.Open, d)
		}
	}
	if c, ok := agg.CurrentCandidate(); ok {
		v.Candidate = &c
	}
	return v, nil
}

// Inbox returns the Decisions waiting for the human, oldest first.
func (s *Service) Inbox(ctx context.Context) ([]domain.Decision, error) {
	rows, err := s.store.InboxDecisions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Decision, len(rows))
	for i, d := range rows {
		out[i] = *d
	}
	return out, nil
}

// WorkspaceView is a workspace with its agents, for lists and completion.
type WorkspaceView struct {
	Workspace domain.Workspace
	Agents    []domain.Agent
}

// WorkspaceList returns every workspace with its agents.
func (s *Service) WorkspaceList(ctx context.Context) ([]WorkspaceView, error) {
	list, err := s.store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceView, 0, len(list))
	for _, w := range list {
		agents, err := s.store.Agents(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, WorkspaceView{Workspace: w, Agents: agents})
	}
	return out, nil
}

// Log returns the durable events of a task after sequence number since, at
// most limit of them (1 to 1000), oldest first. It is the non-streaming form of
// Subscribe: ephemeral events are not in it. An unknown task is a
// *domain.NotFoundError.
func (s *Service) Log(ctx context.Context, task domain.ID, since int64, limit int) ([]domain.Event, error) {
	if _, err := s.store.LoadTask(ctx, task); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	return s.store.EventsSince(ctx, task, since, limit)
}
