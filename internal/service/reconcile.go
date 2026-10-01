package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Report says what one reconciler pass did.
type Report struct {
	Interrupted []domain.ID // runs marked interrupted
	Resumed     []domain.ID // runs relaunched from their session
	Failed      []domain.ID // runs that could not be resumed and now wait on a Decision
	Expired     []domain.ID // Decisions that passed their deadline
	// Errors are per-task problems that did not stop the pass: an environment
	// that never answered exec, a runtime call that failed. The run stays
	// interrupted and the next pass tries again.
	Errors []error
}

// Reconcile is one pass of the DB-first loop (design §5.3). For each active
// task it observes the environments, interrupts the runs whose process or
// environment is gone, brings their environments back, resumes the agent from
// its session, resumes the runs whose quota has reset and expires Decisions
// past their deadline. It never sets a state itself: it calls the aggregate.
// Container addresses are read for nothing and never stored.
func (s *Service) Reconcile(ctx context.Context) (Report, error) {
	var rep Report
	infos, err := s.rt.List(ctx, s.cfg.Owner)
	if err != nil {
		return rep, fmt.Errorf("reconcile: list environments: %w", err)
	}
	seen := map[domain.ID]runtime.Info{}
	for _, in := range infos {
		seen[domain.ID(in.ID)] = in
	}
	tasks, err := s.store.ActiveTaskIDs(ctx)
	if err != nil {
		return rep, err
	}
	for _, id := range tasks {
		if err := s.reconcileTask(ctx, id, seen, &rep); err != nil {
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
			rep.Errors = append(rep.Errors, fmt.Errorf("task %s: %w", id, err))
		}
	}
	return rep, nil
}

func (s *Service) reconcileTask(ctx context.Context, task domain.ID, seen map[domain.ID]runtime.Info, rep *Report) error {
	// 1. Observe the environments and interrupt what they took with them.
	var interrupted, expired []domain.ID
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		interrupted, expired = nil, nil // the closure may run again after a lost compare-and-swap
		for _, env := range a.Environments() {
			if env.State == domain.EnvDeleted {
				continue
			}
			observed := domain.EnvDeleted // the runtime does not know it any more
			if in, ok := seen[env.ID]; ok {
				observed = in.State
			}
			if observed == domain.EnvProvisioning || observed == env.State {
				continue
			}
			before := liveRuns(a)
			if err := a.ObserveEnv(env.ID, observed); err != nil {
				return err
			}
			for id, st := range liveRuns(a) {
				if st == domain.RunInterrupted && before[id] != domain.RunInterrupted {
					interrupted = append(interrupted, id)
				}
			}
		}
		// A live run with no agent session is lost even if its container is
		// up: after a supervisor restart the container survives and the agent
		// process the supervisor owned does not.
		for _, r := range a.Runs() {
			if (r.State == domain.RunStarting || r.State == domain.RunRunning) && !s.attached(r.ID) {
				if err := a.Interrupt(r.ID); err != nil {
					return err
				}
				interrupted = append(interrupted, r.ID)
			}
		}
		expired = a.ExpireDecisions(s.clock.Now())
		return nil
	})
	if err != nil {
		return err
	}
	rep.Interrupted = append(rep.Interrupted, interrupted...)
	rep.Expired = append(rep.Expired, expired...)

	// 2. Bring back what should run: interrupted runs, and paused runs whose
	// quota has reset.
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return err
	}
	var todo []domain.ID
	now := s.clock.Now()
	for _, r := range agg.Runs() {
		if r.State == domain.RunInterrupted && !agg.WaitsForReset(r.ID, now) {
			todo = append(todo, r.ID)
		}
	}
	todo = append(todo, agg.DueResumes(now)...)
	var firstErr error
	for _, run := range todo {
		if err := s.recover(ctx, task, run, rep); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// liveRuns maps each run that is not over to its state.
func liveRuns(a *domain.TaskAggregate) map[domain.ID]domain.RunState {
	out := map[domain.ID]domain.RunState{}
	for _, r := range a.Runs() {
		if !r.State.Terminal() {
			out[r.ID] = r.State
		}
	}
	return out
}

// recover brings one interrupted or due run back: start its environment, wait
// until exec answers, record the environment as running, move the run to
// starting and relaunch the agent from its session. A run that cannot be
// resumed ends failed and waits on a Decision.
func (s *Service) recover(ctx context.Context, task, run domain.ID, rep *Report) error {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return err
	}
	r, ok := agg.Run(run)
	if !ok || (r.State != domain.RunInterrupted && r.State != domain.RunPaused) {
		return nil
	}
	// A run that cannot resume (an open login or quota question) costs no
	// container: the human has to answer first.
	if agg.ResumeBlocked(run) != nil {
		return nil
	}
	env, ok := agg.Environment(r.EnvID)
	if !ok || env.State == domain.EnvDeleted {
		return s.failRun(ctx, task, run, rep) // the environment is gone: nothing to resume in
	}
	if r.SessionID == "" {
		return s.failRun(ctx, task, run, rep) // the agent never reported a session
	}

	if env.State != domain.EnvRunning {
		if err := s.rt.Start(ctx, string(env.ID)); err != nil {
			return fmt.Errorf("start environment %s: %w", env.ID, err)
		}
	}
	if err := s.waitReady(ctx, env.ID); err != nil {
		return err
	}
	// The database says starting before the agent is launched (DB-first). The
	// slot is taken first, so this run is not taken for a lost one meanwhile.
	sl := s.begin(run)
	err = s.update(ctx, task, func(a *domain.TaskAggregate) error {
		if err := a.ObserveEnv(env.ID, domain.EnvRunning); err != nil {
			return err
		}
		return a.Resume(run)
	})
	if err != nil {
		s.end(run, sl)
		return err
	}
	if err := s.launch(ctx, task, run, sl); err != nil {
		if errors.Is(err, agent.ErrNoSession) || errors.Is(err, errAttemptsUsedUp) {
			return s.failRun(ctx, task, run, rep)
		}
		return err
	}
	rep.Resumed = append(rep.Resumed, run)
	return nil
}

// errAttemptsUsedUp tells recover that a run's launch attempts are used up.
var errAttemptsUsedUp = errors.New("the run's launch attempts are used up")

// launch relaunches the agent of a starting run from its session and attaches
// the session. The first message is the resume briefing (D27). If the agent
// cannot be started the run goes back to interrupted and the attempt is
// counted; when the attempts are used up errAttemptsUsedUp is returned and the
// caller fails the run. A session the agent forgot is ErrNoSession.
func (s *Service) launch(ctx context.Context, task, run domain.ID, sl *slot) error {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		s.end(run, sl)
		return err
	}
	r, ok := agg.Run(run)
	if !ok {
		s.end(run, sl)
		return &domain.NotFoundError{Kind: "run", ID: string(run)}
	}
	spec := s.cfg.Spec(agg.Task(), r)
	spec.Prompt = Briefing(agg.Task(), r, agg.SupersededOf(run), spec.Prompt)
	spec.EnvID, spec.Env = string(r.EnvID), append(spec.Env, s.agentEnv(ctx, r.EnvID)...)
	if r.AgentID != "" { // a resumed agent works in its own worktree, as a started one does
		if a, err := s.store.Agent(ctx, r.AgentID); err == nil {
			spec.Workdir = a.Worktree
		}
	}
	// The session outlives the call that starts it: an answer to a Decision comes
	// in on a request that ends long before the agent does.
	sess, err := s.ag.Resume(context.WithoutCancel(ctx), spec, r.SessionID)
	if err != nil {
		s.end(run, sl)
		if errors.Is(err, agent.ErrNoSession) {
			return err
		}
		var exhausted bool
		if uerr := s.update(ctx, task, func(a *domain.TaskAggregate) error {
			var rerr error
			exhausted, rerr = a.RecordLaunchFailure(run, s.cfg.MaxAttempts)
			return rerr
		}); uerr != nil {
			return errors.Join(err, uerr)
		}
		if exhausted {
			return errors.Join(err, errAttemptsUsedUp)
		}
		return err
	}
	if err := s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.MarkRunning(run) }); err != nil {
		_ = sess.Stop(ctx)
		s.end(run, sl)
		return err
	}
	s.attach(task, run, sl, sess)
	return nil
}

// failRun ends a run that cannot be resumed and opens the retry-or-cancel
// Decision.
func (s *Service) failRun(ctx context.Context, task, run domain.ID, rep *Report) error {
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		r, ok := a.Run(run)
		if !ok || r.State.Terminal() {
			return nil
		}
		if r.State == domain.RunPaused || r.State == domain.RunStarting { // neither can fail directly; it is lost first
			if err := a.Interrupt(run); err != nil {
				return err
			}
		}
		_, err := a.FailRun(run, s.cfg.NewID(), s.clock.Now())
		return err
	})
	if err == nil {
		rep.Failed = append(rep.Failed, run)
	}
	return err
}

// waitReady polls exec until the environment answers, as the recovery loop
// needs (spike #2: about 100 ms after a start), bounded by ReadyTimeout on the
// injected clock.
func (s *Service) waitReady(ctx context.Context, env domain.ID) error {
	deadline := s.clock.Now().Add(s.cfg.ReadyTimeout)
	for {
		st, err := s.rt.Exec(ctx, string(env), runtime.ExecRequest{Cmd: s.cfg.ReadyCmd})
		if err == nil {
			if _, _, code, werr := runtime.Collect(st); werr == nil && code == 0 {
				return nil
			}
		}
		if !s.clock.Now().Before(deadline) {
			return fmt.Errorf("environment %s did not answer exec within %s", env, s.cfg.ReadyTimeout)
		}
		if err := s.clock.Sleep(ctx, s.cfg.ReadyInterval); err != nil {
			return err
		}
	}
}
