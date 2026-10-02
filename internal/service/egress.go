package service

import (
	"context"
	"fmt"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
)

// PendingEgress lists the hosts to ask the human about for an environment of a
// repository: the hosts its devcontainer.json requests and its lockfiles
// suggest, without those already answered for the repository, allowed or
// denied. Nothing in the repository allows a host (design §4.2, D38).
func (s *Service) PendingEgress(ctx context.Context, repo string, env devcontainer.Environment) ([]devcontainer.HostRequest, error) {
	answers, err := s.store.EgressHosts(ctx, repo)
	if err != nil {
		return nil, err
	}
	return env.HostRequests(answers.Answered), nil
}

// RequestEgress raises one blocking approval per host for a run that is starting
// or running, in one change: the task moves to awaiting_guidance as for any
// blocking approval. It returns the Decisions' IDs, in the order of the hosts.
// An empty list raises nothing. The answers are kept per repository when they
// come (AnswerDecision), and the environment is provisioned once all are in.
func (s *Service) RequestEgress(ctx context.Context, task, run domain.ID, reqs []devcontainer.HostRequest) ([]domain.ID, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	// All or nothing: update saves what changed even when its function fails, so a
	// host that cannot be asked about must stop the request before any is raised.
	for _, r := range reqs {
		if !domain.ValidHost(r.Host) {
			return nil, fmt.Errorf("egress request: %q is not a host name that can be asked about", r.Host)
		}
	}
	ids := make([]domain.ID, 0, len(reqs))
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		ids = ids[:0]
		for _, r := range reqs {
			id := s.cfg.NewID()
			if _, err := a.RaiseEgressRequest(run, id, r.Host, r.Source, s.clock.Now()); err != nil {
				return fmt.Errorf("egress request for %q: %w", r.Host, err)
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// EgressAllow returns the hosts the human allowed for a repository: what to put
// in the egress allowlist of its environments besides the supervisor's own.
func (s *Service) EgressAllow(ctx context.Context, repo string) ([]string, error) {
	answers, err := s.store.EgressHosts(ctx, repo)
	if err != nil {
		return nil, err
	}
	return answers.Allowed, nil
}

// keepEgressAnswer stores the human's answer to an egress request for the
// repository of its task, so no later run or other workspace of the repository
// is asked again. Only an answered allow or deny is kept: an expired or
// superseded request never reaches here, and it is a denial for its run only.
func (s *Service) keepEgressAnswer(ctx context.Context, d domain.Decision, option string) error {
	agg, err := s.store.LoadTask(ctx, d.TaskID)
	if err != nil {
		return err
	}
	allowed := option == domain.AnswerAllow
	if !allowed && option != domain.AnswerDeny {
		return nil
	}
	return s.store.SetEgressHost(ctx, agg.Task().Repo, d.Host, allowed, d.ID, s.clock.Now())
}

// egressWait is a run that stays starting until the human has answered every
// egress request raised for it (design §4.2). finish starts the agent; sl is the
// run's slot, held so the reconciler does not take the run for lost while it waits.
type egressWait struct {
	task   domain.ID
	finish func(ctx context.Context) error
	sl     *slot
}

// holdForEgress records that a run waits for its egress requests. The wait is in
// memory: after a restart the reconciler interrupts the run, which supersedes the
// requests, and the host is asked again at the next start.
func (s *Service) holdForEgress(run domain.ID, w *egressWait) {
	s.mu.Lock()
	if s.egressWaits == nil {
		s.egressWaits = map[domain.ID]*egressWait{}
	}
	s.egressWaits[run] = w
	s.mu.Unlock()
}

// takeEgressWait removes and returns the wait of a run, if it has one.
func (s *Service) takeEgressWait(run domain.ID) *egressWait {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.egressWaits[run]
	delete(s.egressWaits, run)
	return w
}

// dropEgressWait forgets a run's wait and frees its slot: the run ended without
// starting its agent (the task was cancelled).
func (s *Service) dropEgressWait(run domain.ID) {
	if w := s.takeEgressWait(run); w != nil {
		s.end(run, w.sl)
	}
}

// egressOpen reports whether a run still has an egress request waiting for an
// answer.
func (s *Service) egressOpen(ctx context.Context, task, run domain.ID) (bool, error) {
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return false, err
	}
	if r, ok := agg.Run(run); !ok || r.State != domain.RunStarting {
		return false, nil
	}
	for _, d := range agg.Decisions() {
		if d.RunID == run && d.Cause == domain.CauseEgressRequest && d.Status == domain.DecisionOpen {
			return true, nil
		}
	}
	return false, nil
}

// continueEgress starts the agent of a run that waited for egress requests, once
// none is open: every request was answered, or expired, which is a denial for
// this run only. A run that is no longer starting (cancelled) is dropped.
func (s *Service) continueEgress(ctx context.Context, task, run domain.ID) error {
	s.mu.Lock()
	_, waiting := s.egressWaits[run]
	s.mu.Unlock()
	if !waiting {
		return nil
	}
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return err
	}
	if r, ok := agg.Run(run); !ok || r.State != domain.RunStarting {
		s.dropEgressWait(run)
		return nil
	}
	if open, err := s.egressOpen(ctx, task, run); err != nil || open {
		return err
	}
	w := s.takeEgressWait(run)
	if w == nil {
		return nil // another caller got there first
	}
	return w.finish(ctx)
}

// continueAllEgress is continueEgress for every run that waits: the reconciler
// calls it, because a request that expired opens the way without an answer.
func (s *Service) continueAllEgress(ctx context.Context) []error {
	s.mu.Lock()
	waits := make(map[domain.ID]domain.ID, len(s.egressWaits)) // run -> task
	for run, w := range s.egressWaits {
		waits[run] = w.task
	}
	s.mu.Unlock()
	var errs []error
	for run, task := range waits {
		if err := s.continueEgress(ctx, task, run); err != nil {
			errs = append(errs, fmt.Errorf("run %s: %w", run, err))
		}
	}
	return errs
}

// RepoEnvironment is a repository's environment as read from its default branch,
// with the means to make its image. The supervisor reads it in its own copy,
// never in a workspace (D38).
type RepoEnvironment struct {
	devcontainer.Environment
	// Image returns the image to run: it builds one from the commit when the
	// environment has a Dockerfile, once per commit, or returns the image the
	// file names. Nil when the environment is the supervisor's default.
	Image func(ctx context.Context) (string, error)
}
