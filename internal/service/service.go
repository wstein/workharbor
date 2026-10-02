// Package service is the one layer the JSON API and the web UI call (design
// D8), and the DB-first reconciler (D6, §5.3). It holds no state of its own
// beyond the sessions it has attached: the database is the desired state, the
// runtime and the agent are what is actually there, and every change to a task
// goes through its aggregate.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/notify"
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
	// MaxAttempts is how many failed launches of the agent a run may have
	// before it ends failed (design §5.3). Default 3.
	MaxAttempts int
	// Notifier gets a push for a new blocking Decision and for a run that ended
	// or failed (design §9.4). It is best effort: a failure is reported through
	// OnError and never fails the change. Optional.
	Notifier notify.Notifier
	// Board, if set, is kept current with the state of every task that started
	// from an issue (design D30): the supervisor's own action, through the
	// forge's autonomy policy. A failed write never affects a task. Optional.
	Board forge.Board
	// BoardLink returns the link to a task in the web UI for its card. Optional.
	BoardLink func(task domain.ID) string
	// RevokeTokens revokes the forge tokens the supervisor holds and returns how
	// many it revoked: the token half of KillAll. Optional.
	RevokeTokens func(ctx context.Context) (int, error)
	// Budgets are the per-run and per-task limits on tokens and cost (§7.4).
	// The zero value sets none. Optional.
	Budgets Budgets
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

	wg        sync.WaitGroup
	mu        sync.Mutex
	sessions  map[domain.ID]*slot               // by run: the sessions the service owns, and launches in progress
	closing   bool                              // set by Shutdown: no session joins the wait group any more
	async     *notify.Async                     // the queue that delivers cfg.Notifier's messages, when set
	bus       bus                               // live events for subscribers (design §5.3)
	board     boardQueue                        // card updates waiting for the worker (D30)
	approvals map[domain.ID]chan agent.Approval // approval Decisions an agent is waiting for (D26)
}

// slot is a run's entry in the sessions map. It is put there before the agent
// is called, so a run whose launch is in progress is not mistaken for a lost
// one, and the session is filled in once the agent is up.
type slot struct{ sess agent.Session }

// New returns a service.
func New(st *store.Store, rt runtime.Adapter, ag agent.Adapter, clock Clock, cfg Config) *Service {
	if clock == nil {
		clock = SystemClock{}
	}
	if len(cfg.ReadyCmd) == 0 {
		cfg.ReadyCmd = []string{"true"}
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	if cfg.ReadyInterval <= 0 {
		cfg.ReadyInterval = 100 * time.Millisecond
	}
	s := &Service{store: st, rt: rt, ag: ag, clock: clock, cfg: cfg, sessions: map[domain.ID]*slot{}, approvals: map[domain.ID]chan agent.Approval{}}
	if cfg.Notifier != nil {
		// A slow relay must never hold up a reconcile pass or a session
		// handler: messages go through a bounded queue (design §9.4).
		s.async = notify.NewAsync(cfg.Notifier, 64, 10*time.Second)
		s.async.OnError(s.report)
		s.cfg.Notifier = s.async
	}
	return s
}

// Wait blocks until every attached session's handler has finished.
func (s *Service) Wait() { s.wg.Wait() }

// Shutdown stops every attached session and waits for their handlers. A stop
// leaves a session resumable, so the next start of the supervisor reconciles it
// from the database (design §5.3).
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.closing = true // from now on attach stops a session instead of adding it
	live := make([]agent.Session, 0, len(s.sessions))
	for _, sl := range s.sessions {
		if sl.sess != nil {
			live = append(live, sl.sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range live {
		_ = sess.Stop(context.Background())
	}
	s.wg.Wait()
	s.waitBoard(10 * time.Second) // the last card updates, bounded: the board is never worth a hang
	if s.async != nil {
		s.async.Close() // deliver what is queued, then stop
	}
}

func (s *Service) waitBoard(limit time.Duration) {
	done := make(chan struct{})
	go func() { s.board.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
	}
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
			var saved []domain.Event
			if saved, err = s.store.SaveTask(ctx, agg); err != nil {
				if errors.Is(err, store.ErrStale) {
					continue
				}
				return err
			}
			s.publish(saved)
			s.notify(ctx, saved)
		}
		return fnErr
	}
	return fmt.Errorf("task %s: %w", task, err)
}

// notify pushes what the saved events call for. It runs after the change is
// committed and never fails it; the inbox is the source of truth.
func (s *Service) notify(ctx context.Context, events []domain.Event) {
	if s.cfg.Notifier == nil {
		return
	}
	cancelled := false
	for _, e := range events {
		if e.Kind == domain.EventBudgetExceeded {
			cancelled = true // the budget message says why the run ended
		}
		if e.Kind == domain.EventTaskState {
			var p domain.StateChanged
			if json.Unmarshal(e.Payload, &p) == nil && p.To == string(domain.TaskCancelled) {
				cancelled = true
			}
		}
	}
	for _, m := range notify.FromEvents(events) {
		if cancelled && m.Kind == notify.KindRunEnded {
			continue // the human cancelled it; they know
		}
		s.report(s.cfg.Notifier.Notify(ctx, m)) // queued: returns at once
	}
}

// begin marks a run's launch as in progress.
func (s *Service) begin(run domain.ID) *slot {
	sl := &slot{}
	s.mu.Lock()
	s.sessions[run] = sl
	s.mu.Unlock()
	return sl
}

// end removes a run's entry, but only if it is still this one: a relaunch
// during a pass may have put a new session there.
func (s *Service) end(run domain.ID, sl *slot) {
	s.mu.Lock()
	if s.sessions[run] == sl {
		delete(s.sessions, run)
	}
	s.mu.Unlock()
}

// attached reports whether the service owns a session, or a launch in
// progress, for a run.
func (s *Service) attached(run domain.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.sessions[run]
	return ok
}

// attach pumps a session's events into the task until the session ends: the
// agent's session ID is recorded, a login or quota end suspends the run, and the
// result ends, fails or interrupts it. Everything goes through the aggregate.
func (s *Service) attach(task, run domain.ID, sl *slot, sess agent.Session) {
	s.mu.Lock()
	if s.closing {
		// Shutdown has begun: the session never joins the wait group, so
		// wg.Add cannot race wg.Wait. It is stopped and stays resumable; the
		// next start reconciles the run (design §5.3).
		s.mu.Unlock()
		_ = sess.Stop(context.Background())
		s.end(run, sl)
		return
	}
	sl.sess = sess
	s.wg.Add(1) // under s.mu, so it happens before Shutdown sets closing or not at all
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer s.end(run, sl)
		ctx := context.Background()
		for e := range sess.Events() {
			switch e.Kind {
			case agent.EventSession:
				s.report(s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.RecordSession(run, e.SessionID) }))
			case agent.EventAuthExpired:
				s.report(s.suspend(ctx, task, run, domain.CauseAuthExpired, time.Time{}))
			case agent.EventQuotaExhausted:
				s.report(s.suspend(ctx, task, run, domain.CauseQuotaExhausted, e.ResetAt))
			case agent.EventUsage:
				s.recordUsage(ctx, task, run, e)
			default:
				s.record(ctx, task, e)
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
		case agent.ResultCompleted:
			return a.StopRun(run)
		case agent.ResultStopped:
			// A stop the human asked for (cancel) has ended the run already, so
			// a live run here lost its agent to the supervisor: a shutdown. The
			// run is interrupted and resumes on the next start.
			return a.Interrupt(run)
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
	// An egress request is asked before the agent starts, so no agent waits for it.
	if row.Kind == domain.DecisionApproval && row.Cause != domain.CauseEgressRequest && row.Status == domain.DecisionOpen && !s.approvalWaiting(id) {
		// The agent that asked is gone (stopped, paused, or the supervisor
		// restarted), so there is nothing to answer: refused, not stored (D23).
		return domain.NewConflict(domain.RuleDecisionClosed, "no agent is waiting for approval %s: its run was stopped or the supervisor restarted; the agent asks again after it resumes", id)
	}
	resumes := r.Option == domain.AnswerResume && row.Cause != domain.CauseRunFailed && row.RunID != ""
	var sl *slot
	if resumes {
		sl = s.begin(row.RunID) // the run is about to start: it is not lost
	}
	d, answered, err := s.store.RespondDecision(ctx, id, r)
	s.publish(answered)
	if err == nil {
		s.deliverApproval(d)
	}
	if err != nil {
		if sl != nil {
			s.end(row.RunID, sl)
		}
		return err
	}
	if row.Cause == domain.CauseEgressRequest && row.Host != "" {
		if err := s.keepEgressAnswer(ctx, *row, r.Option); err != nil {
			return fmt.Errorf("keep the answer for %s: %w", row.Host, err)
		}
	}
	switch {
	case r.Option == domain.AnswerCancel:
		s.stopSession(row.RunID)
	case resumes:
		err := s.launch(ctx, row.TaskID, row.RunID, sl)
		if errors.Is(err, agent.ErrNoSession) || errors.Is(err, errAttemptsUsedUp) {
			// The session is gone, or the attempts are used up: the run ends
			// failed and waits on a retry-or-cancel Decision.
			var rep Report
			return errors.Join(err, s.failRun(ctx, row.TaskID, row.RunID, &rep))
		}
		return err
	}
	return nil
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
	sl := s.sessions[run]
	s.mu.Unlock()
	if sl != nil && sl.sess != nil {
		_ = sl.sess.Stop(context.Background())
	}
}

// agentEnv is the environment the supervisor adds for an agent process: the
// egress proxy's address, read from the runtime now because it changes with
// every start of the environment and is never stored. Without a sidecar there
// is no proxy and nothing is added. It holds no secret.
func (s *Service) agentEnv(ctx context.Context, env domain.ID) []string {
	info, err := s.rt.Inspect(ctx, string(env))
	if err != nil || info.Proxy == "" {
		if err != nil {
			s.report(fmt.Errorf("find the egress proxy of %s: %w", env, err))
		}
		return nil
	}
	return []string{"HTTPS_PROXY=" + info.Proxy, "HTTP_PROXY=" + info.Proxy, "NO_PROXY=localhost,127.0.0.1,::1"}
}
