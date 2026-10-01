// Package service is the one layer the JSON API and the web UI call (design
// D8), and the DB-first reconciler (D6, §5.3). It holds no state of its own
// beyond the sessions it has attached: the database is the desired state, the
// runtime and the agent are what is actually there, and every change to a task
// goes through its aggregate.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/store"
)

// Clock is time as the service sees it, so tests run on an injected clock.
type Clock interface {
	Now() time.Time
	// Sleep waits for d or until ctx ends, and returns the context's error then.
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep implements Clock.
func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Config is what the service needs besides its adapters.
type Config struct {
	// Owner is the supervisor's owner label: the reconciler lists only the
	// environments that carry it (design §5.1).
	Owner string
	// Spec returns the StartSpec to resume a run with: the working directory, a
	// prompt such as "continue", the auth mode, the permission mode and the
	// approver. The service sets nothing in it.
	Spec func(task domain.Task, run domain.Run) agent.StartSpec
	// NewID returns a fresh ID for a Decision the service raises.
	NewID func() domain.ID
	// ReadyCmd is a command that succeeds once the environment answers exec
	// (about 100 ms after a start, spike #2). Default ["true"].
	ReadyCmd []string
	// ReadyTimeout bounds the wait, and ReadyInterval is the pause between
	// attempts. Defaults 30 s and 100 ms.
	ReadyTimeout, ReadyInterval time.Duration
	// OnError hears errors that happen in the background, such as a session's
	// event handler losing a compare-and-swap for good. Optional.
	OnError func(error)
}

// Service is the supervisor's use-case layer.
type Service struct {
	store *store.Store
	rt    runtime.Adapter
	ag    agent.Adapter
	clock Clock
	cfg   Config

	wg       sync.WaitGroup
	mu       sync.Mutex
	sessions map[domain.ID]agent.Session // by run
}

// New returns a service.
func New(st *store.Store, rt runtime.Adapter, ag agent.Adapter, clock Clock, cfg Config) *Service {
	if clock == nil {
		clock = SystemClock{}
	}
	if len(cfg.ReadyCmd) == 0 {
		cfg.ReadyCmd = []string{"true"}
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	if cfg.ReadyInterval <= 0 {
		cfg.ReadyInterval = 100 * time.Millisecond
	}
	return &Service{store: st, rt: rt, ag: ag, clock: clock, cfg: cfg, sessions: map[domain.ID]agent.Session{}}
}

// Wait blocks until every attached session's handler has finished.
func (s *Service) Wait() { s.wg.Wait() }

// Shutdown stops every attached session and waits for their handlers. A stop
// leaves a session resumable, so the next start of the supervisor reconciles it
// from the database (design §5.3).
func (s *Service) Shutdown() {
	s.mu.Lock()
	live := make([]agent.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		live = append(live, sess)
	}
	s.mu.Unlock()
	for _, sess := range live {
		_ = sess.Stop(context.Background())
	}
	s.wg.Wait()
}

func (s *Service) report(err error) {
	if err != nil && s.cfg.OnError != nil {
		s.cfg.OnError(err)
	}
}

// maxRetries is how often a change is retried after losing a compare-and-swap.
const maxRetries = 5

// update loads a task, applies fn and saves what changed, retrying from a fresh
// load when another writer got in first. fn may return an error after it
// changed the aggregate (a refused answer can still expire a Decision): the
// change is saved and the error returned.
func (s *Service) update(ctx context.Context, task domain.ID, fn func(*domain.TaskAggregate) error) error {
	var err error
	for range maxRetries {
		var agg *domain.TaskAggregate
		if agg, err = s.store.LoadTask(ctx, task); err != nil {
			return err
		}
		fnErr := fn(agg)
		if len(agg.PendingEvents()) > 0 {
			if _, err = s.store.SaveTask(ctx, agg); err != nil {
				if errors.Is(err, store.ErrStale) {
					continue
				}
				return err
			}
		}
		return fnErr
	}
	return fmt.Errorf("task %s: %w", task, err)
}

// attach pumps a session's events into the task until the session ends: the
// agent's session ID is recorded, a login or quota end suspends the run, and the
// result ends or fails it. Everything goes through the aggregate.
func (s *Service) attach(task, run domain.ID, sess agent.Session) {
	s.mu.Lock()
	s.sessions[run] = sess
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.sessions, run)
			s.mu.Unlock()
		}()
		ctx := context.Background()
		for e := range sess.Events() {
			switch e.Kind {
			case agent.EventSession:
				s.report(s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.RecordSession(run, e.SessionID) }))
			case agent.EventAuthExpired:
				s.report(s.suspend(ctx, task, run, domain.CauseAuthExpired, time.Time{}))
			case agent.EventQuotaExhausted:
				s.report(s.suspend(ctx, task, run, domain.CauseQuotaExhausted, e.ResetAt))
			}
		}
		res, _ := sess.Wait()
		s.report(s.finish(ctx, task, run, res))
	}()
}

func (s *Service) suspend(ctx context.Context, task, run domain.ID, cause domain.DecisionCause, reset time.Time) error {
	return s.update(ctx, task, func(a *domain.TaskAggregate) error {
		if r, ok := a.Run(run); !ok || r.State != domain.RunRunning {
			return nil // already suspended or over: the event is a repeat
		}
		_, err := a.SuspendRun(run, cause, reset, s.cfg.NewID(), s.clock.Now())
		return err
	})
}

// finish applies a session's result to its run, unless the run is no longer
// the live one (a stop or a suspension got there first).
func (s *Service) finish(ctx context.Context, task, run domain.ID, res agent.Result) error {
	return s.update(ctx, task, func(a *domain.TaskAggregate) error {
		r, ok := a.Run(run)
		if !ok || r.State.Terminal() || r.State == domain.RunPaused || r.State == domain.RunInterrupted {
			return nil
		}
		switch res.Status {
		case agent.ResultCompleted, agent.ResultStopped:
			return a.StopRun(run)
		case agent.ResultFailed:
			_, err := a.FailRun(run, s.cfg.NewID(), s.clock.Now())
			return err
		case agent.ResultAuthExpired:
			_, err := a.SuspendRun(run, domain.CauseAuthExpired, time.Time{}, s.cfg.NewID(), s.clock.Now())
			return err
		case agent.ResultQuotaExhausted:
			_, err := a.SuspendRun(run, domain.CauseQuotaExhausted, res.ResetAt, s.cfg.NewID(), s.clock.Now())
			return err
		}
		return nil
	})
}

// AnswerDecision records a human's answer. An answer that resumes a run
// ("resume") relaunches the agent from its session; "cancel" stops it. The
// error of a refused answer is returned after what it changed was saved.
func (s *Service) AnswerDecision(ctx context.Context, id domain.ID, r domain.Response) error {
	row, err := s.store.LoadDecision(ctx, id)
	if err != nil {
		return err
	}
	var before domain.RunState
	if run, ok := s.runOf(ctx, row.TaskID, row.RunID); ok {
		before = run.State
	}
	if _, _, err := s.store.RespondDecision(ctx, id, r); err != nil {
		return err
	}
	switch {
	case r.Option == domain.AnswerCancel:
		s.stopSession(row.RunID)
	case r.Option == domain.AnswerResume && row.Cause != domain.CauseRunFailed && before != "":
		return s.launch(ctx, row.TaskID, row.RunID)
	}
	return nil
}

func (s *Service) runOf(ctx context.Context, task, run domain.ID) (domain.Run, bool) {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return domain.Run{}, false
	}
	return agg.Run(run)
}

// Cancel cancels a task: its live run is stopped, its Decisions are
// superseded and its agent session ends.
func (s *Service) Cancel(ctx context.Context, task domain.ID) error {
	var live domain.ID
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		if r, ok := a.LiveRun(); ok {
			live = r.ID
		}
		return a.Cancel()
	})
	if err == nil {
		s.stopSession(live)
	}
	return err
}

func (s *Service) stopSession(run domain.ID) {
	s.mu.Lock()
	sess := s.sessions[run]
	s.mu.Unlock()
	if sess != nil {
		_ = sess.Stop(context.Background())
	}
}
