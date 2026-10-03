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

// markEnvStarted marks the environment and reports whether this call set the
// mark (it was not set before).
func (s *Service) markEnvStarted(env domain.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startedEnvs == nil {
		s.startedEnvs = map[domain.ID]bool{}
	}
	was := s.startedEnvs[env]
	s.startedEnvs[env] = true
	return !was
}

func (s *Service) forgetEnvStarted(env domain.ID) {
	s.mu.Lock()
	delete(s.startedEnvs, env)
	s.mu.Unlock()
}

// startEnv starts an environment and remembers that this process did. The mark
// is set before the start, so a reconciler pass cannot stop the environment
// between the two; a failed start takes it back only when this call set it: a
// mark set earlier, perhaps by a concurrent start that succeeded, stays.
func (s *Service) startEnv(ctx context.Context, env string) error {
	set := s.markEnvStarted(domain.ID(env))
	if err := s.rt.Start(ctx, env); err != nil {
		if set {
			s.forgetEnvStarted(domain.ID(env))
		}
		return err
	}
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
		return s.startIfStopped(ctx, task, r.EnvID)
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

// startIfStopped starts an environment this process started once but that is
// stopped now (a pause that stopped it, an observed stop), and records it
// running: a resume of a paused run needs it running (design 4.1, issue #221).
func (s *Service) startIfStopped(ctx context.Context, task, env domain.ID) error {
	s.freshMu.Lock()
	defer s.freshMu.Unlock()
	info, err := s.rt.Inspect(ctx, string(env))
	if err != nil {
		return fmt.Errorf("inspect environment %s: %w", env, err)
	}
	if info.State == domain.EnvRunning {
		return nil
	}
	if err := s.startEnv(ctx, string(env)); err != nil {
		return fmt.Errorf("start environment %s: %w", env, err)
	}
	if err := s.waitReady(ctx, env); err != nil {
		return err
	}
	return s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.ObserveEnv(env, domain.EnvRunning) })
}

// stopLeftoverEnvs is the first step of every reconciler pass (design 4.1, 5.3,
// issue #221): it stops each running environment of the owner that this
// supervisor's store records and this process did not start (the owner label is
// shared by every supervisor of the host, so an environment the store does not
// record may be another supervisor's live one and is left alone), whatever runs it holds, the console's excepted: an agent an
// earlier process left behind (a pause saved before a crash, a cancel whose stop
// failed) ends with it. A paused run in it stays paused. The stopped
// environments are recorded stopped in seen, so the pass observes them; one whose
// stop failed stays running there and is tried again on the next pass.
func (s *Service) stopLeftoverEnvs(ctx context.Context, seen map[domain.ID]runtime.Info) []error {
	var errs []error
	recorded, err := s.store.RecordedEnvironments(ctx)
	if err != nil {
		return []error{err}
	}
	for id, in := range seen {
		if in.State != domain.EnvRunning || in.Labels[ConsoleLabel] == "1" || !recorded[id] || s.envStarted(id) {
			continue
		}
		s.freshMu.Lock()
		err := s.stopLeftover(ctx, "", id, false, nil)
		started := s.envStarted(id)
		s.freshMu.Unlock()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !started {
			in.State = domain.EnvStopped
			seen[id] = in
		}
	}
	return errs
}

// stopEnvForPause ends the agent of a paused run whose own stop failed by
// stopping its environment (design 4.1). When that fails too, the process forgets
// that it started the environment, so the next pass stops it and no launch there
// skips the stop and start; the error says the agent may still run.
func (s *Service) stopEnvForPause(ctx context.Context, task, env domain.ID, cause error) error {
	s.freshMu.Lock()
	err := s.rt.Stop(context.WithoutCancel(ctx), string(env))
	if err != nil && errors.Is(err, runtime.ErrNotFound) {
		err = nil
	}
	s.forgetEnvStarted(env)
	s.freshMu.Unlock()
	if err != nil {
		return fmt.Errorf("task %s is paused, but its agent may still run: stop the agent: %w; stop the environment %s: %w", task, cause, env, err)
	}
	uerr := s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.ObserveEnv(env, domain.EnvStopped) })
	return uerr
}
