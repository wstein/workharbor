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
