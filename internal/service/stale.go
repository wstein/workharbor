package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// errStopFailed marks the failure to stop an environment this process did not
// start, which keeps the run from being relaunched (design 4.1, "No surviving
// agent before a relaunch", issue #216).
var errStopFailed = errors.New("stop the environment")

// envStarted reports whether this supervisor process started the environment.
// Only such an environment holds agents this process launched; any other may
// hold an agent an earlier process left behind (spike #7, Case 4).
func (s *Service) envStarted(env domain.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startedEnvs[env]
}

func (s *Service) markEnvStarted(env domain.ID) {
	s.mu.Lock()
	if s.startedEnvs == nil {
		s.startedEnvs = map[domain.ID]bool{}
	}
	s.startedEnvs[env] = true
	s.mu.Unlock()
}

// startEnv starts an environment and remembers that this process did.
func (s *Service) startEnv(ctx context.Context, env string) error {
	if err := s.rt.Start(ctx, env); err != nil {
		return err
	}
	s.markEnvStarted(domain.ID(env))
	return nil
}

// stopLeftover stops an environment this process did not start, which ends every
// process in it, and records the stop in the task when observe is set: the live
// runs in it become interrupted. An environment this process started, or one the
// runtime no longer has, is left alone. The caller holds freshMu.
func (s *Service) stopLeftover(ctx context.Context, task, env domain.ID, observe bool, rep *Report) error {
	if s.envStarted(env) {
		return nil
	}
	info, err := s.rt.Inspect(ctx, string(env))
	switch {
	case errors.Is(err, runtime.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("%w %s: %w", errStopFailed, env, err)
	}
	if info.State != domain.EnvStopped {
		if err := s.rt.Stop(ctx, string(env)); err != nil && !errors.Is(err, runtime.ErrNotFound) {
			return fmt.Errorf("%w %s: %w", errStopFailed, env, err)
		}
	}
	if !observe {
		return nil
	}
	var interrupted []domain.ID
	err = s.update(ctx, task, func(a *domain.TaskAggregate) error {
		interrupted = nil
		before := liveRuns(a)
		if err := a.ObserveEnv(env, domain.EnvStopped); err != nil {
			return err
		}
		for id, st := range liveRuns(a) {
			if st == domain.RunInterrupted && before[id] != domain.RunInterrupted {
				interrupted = append(interrupted, id)
			}
		}
		return nil
	})
	if rep != nil {
		rep.Interrupted = append(rep.Interrupted, interrupted...)
	}
	return err
}

// freshEnv makes the environment one this process started: an environment it did
// not start is stopped, started, and recorded running. A
// failed stop is errStopFailed and nothing is started.
func (s *Service) freshEnv(ctx context.Context, task, env domain.ID, rep *Report) error {
	s.freshMu.Lock()
	defer s.freshMu.Unlock()
	if s.envStarted(env) {
		return nil
	}
	if err := s.stopLeftover(ctx, task, env, true, rep); err != nil {
		return err
	}
	if err := s.startEnv(ctx, string(env)); err != nil {
		return fmt.Errorf("start environment %s: %w", env, err)
	}
	return s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.ObserveEnv(env, domain.EnvRunning) })
}

// freshenRun is what every path that moves a paused or interrupted run to
// starting does first. A failed stop leaves the run where it was; an interrupted
// run counts an attempt, and a run whose attempts are used up fails.
func (s *Service) freshenRun(ctx context.Context, task, run, env domain.ID, rep *Report) error {
	if s.envStarted(env) {
		return nil
	}
	err := s.freshEnv(ctx, task, env, rep)
	if !errors.Is(err, errStopFailed) {
		return err
	}
	var exhausted bool
	uerr := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		exhausted = false
		r, ok := a.Run(run)
		if !ok || r.State != domain.RunInterrupted {
			return nil
		}
		var rerr error
		exhausted, rerr = a.RecordResumeFailure(run, s.cfg.MaxAttempts)
		return rerr
	})
	if exhausted && uerr == nil {
		uerr = s.failRun(ctx, task, run, rep)
	}
	return errors.Join(err, uerr)
}

// freshenForResume is freshenRun for the paths that are not the reconciler (the
// human's resume, an answer that resumes): it looks the run up and skips what
// would not resume anyway. An answer that resumes passes answering, because the
// question it closes is the one that blocks the resume now.
func (s *Service) freshenForResume(ctx context.Context, task, run domain.ID, answering bool) error {
	agg, err := s.loadTask(ctx, task)
	if err != nil {
		return err
	}
	r, ok := agg.Run(run)
	if !ok || (r.State != domain.RunInterrupted && r.State != domain.RunPaused) || (!answering && agg.ResumeBlocked(run) != nil) {
		return nil // the transition itself refuses it
	}
	if env, ok := agg.Environment(r.EnvID); !ok || env.State == domain.EnvDeleted || s.rebuilding(r.WorkspaceID) {
		return nil
	}
	if err := s.checkEnvFree(ctx, r.EnvID, run); err != nil {
		return err
	}
	if s.envStarted(r.EnvID) {
		return nil
	}
	var rep Report
	if err := s.freshenRun(ctx, task, run, r.EnvID, &rep); err != nil {
		return err
	}
	return s.waitReady(ctx, r.EnvID)
}

// restartLeftover stops an environment this process did not start and starts it,
// for a new run's start, which has no task to record the stop in (a run that
// owned the environment would have refused the start before this). A failed stop
// is errStopFailed.
func (s *Service) restartLeftover(ctx context.Context, env domain.ID) error {
	s.freshMu.Lock()
	defer s.freshMu.Unlock()
	if s.envStarted(env) {
		return nil
	}
	if err := s.stopLeftover(ctx, "", env, false, nil); err != nil {
		return err
	}
	if err := s.startEnv(ctx, string(env)); err != nil {
		return fmt.Errorf("start environment %s: %w", env, err)
	}
	return nil
}

// checkEnvFree is the rule of one active run per environment across tasks, with
// an interrupted run counted (design 4.1): it refuses when a run other than run
// owns env. A resume passes its own run; a new run's start passes none. The check
// of a new run's save is repeated inside its transaction.
func (s *Service) checkEnvFree(ctx context.Context, env, run domain.ID) error {
	owning, err := s.store.UnfinishedRuns(ctx, env)
	if err != nil {
		return err
	}
	others := owning[:0:0]
	for _, r := range owning {
		if r.ID != run {
			others = append(others, r)
		}
	}
	return domain.CheckEnvironmentFree(env, others)
}

// retryPendingStops tries again the stop of every environment of a cancelled run
// whose stop failed; one that stops, or that this process started meanwhile, is
// dropped. A failure stays pending for the next pass.
func (s *Service) retryPendingStops(ctx context.Context) []error {
	s.mu.Lock()
	pending := make(map[domain.ID]domain.ID, len(s.pendingStops))
	for env, task := range s.pendingStops {
		pending[env] = task
	}
	s.mu.Unlock()
	var errs []error
	for env, task := range pending {
		s.freshMu.Lock()
		err := s.stopLeftover(ctx, task, env, false, nil)
		s.freshMu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", task, err))
			continue
		}
		s.mu.Lock()
		delete(s.pendingStops, env)
		s.mu.Unlock()
	}
	return errs
}
