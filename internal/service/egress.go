package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/wstein/workharbor/internal/devcontainer"
	"github.com/wstein/workharbor/internal/domain"
)

// PendingEgress lists the hosts to ask the human about for an environment of a
// repository: the hosts its devcontainer.json requests and its lockfiles
// suggest, without those already answered for the repository, allowed or
// denied. Nothing in the repository allows a host (design §4.2, D38).
//
// Under the published preset (D47) a host is asked again when its source changed:
// an answer is kept only while it was given for the devcontainer.json of the
// commit the environment is built from, so one given for another version of the
// file, for a lockfile suggestion, or from before sources were recorded is
// forgotten here and counts as unanswered, neither allowed nor denied. Lockfile
// suggestions are ignored. The other presets ask once per repository.
func (s *Service) PendingEgress(ctx context.Context, repo string, env devcontainer.Environment, published bool) ([]devcontainer.HostRequest, error) {
	answers, err := s.store.EgressHosts(ctx, repo)
	if err != nil {
		return nil, err
	}
	if !published {
		return env.HostRequests(answers.Answered), nil
	}
	env.SuggestedHosts = nil
	var stale []string
	for host, source := range answers.Sources {
		if env.Config.SourceDigest == "" || source != env.Config.SourceDigest {
			stale = append(stale, host)
			delete(answers.Answered, host)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		if err := s.store.ForgetEgressHosts(ctx, repo, stale); err != nil {
			return nil, err
		}
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
	digests := make(map[domain.ID]string, len(reqs))
	ids := make([]domain.ID, 0, len(reqs))
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		ids = ids[:0]
		for _, r := range reqs {
			id := s.cfg.NewID()
			if _, err := a.RaiseEgressRequest(run, id, r.Host, r.Source, s.clock.Now()); err != nil {
				return fmt.Errorf("egress request for %q: %w", r.Host, err)
			}
			ids = append(ids, id)
			digests[id] = r.Digest
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.egressSources == nil {
		s.egressSources = map[domain.ID]string{}
	}
	for id, d := range digests {
		s.egressSources[id] = d
	}
	s.mu.Unlock()
	return ids, nil
}

// PendingFeatureSources lists the features an environment takes from a source outside
// the allowed one that the repository has not answered for (D38, issue #127): never
// answered, or allowed for another digest than the reference resolves to now, because a
// tag can be moved. A deny stays per reference until the human changes it, whatever the
// reference resolves to. Nothing in the repository allows a source.
func (s *Service) PendingFeatureSources(ctx context.Context, repo string, env devcontainer.Environment) ([]devcontainer.ForeignFeature, error) {
	if len(env.ForeignFeatures) == 0 {
		return nil, nil
	}
	answers, err := s.store.FeatureSources(ctx, repo)
	if err != nil {
		return nil, err
	}
	var out []devcontainer.ForeignFeature
	for _, f := range env.ForeignFeatures {
		if got, ok := answers[f.Ref]; ok && (!got.Allowed || got.Digest == f.Digest) {
			continue // denied, or allowed for exactly this digest
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

// ApprovedFeatureSources returns the feature references the human allowed for a
// repository, each with the digest it was allowed at: what feature.Resolver.Approved
// reads when its environment is resolved. A reference counts only at that digest.
func (s *Service) ApprovedFeatureSources(ctx context.Context, repo string) (map[string]string, error) {
	answers, err := s.store.FeatureSources(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for ref, a := range answers {
		if a.Allowed {
			out[ref] = a.Digest
		}
	}
	return out, nil
}

// RequestFeatureSources raises one blocking approval per feature (reference and the
// digest it resolves to) for a starting run, in one change and all or nothing, like
// RequestEgress.
func (s *Service) RequestFeatureSources(ctx context.Context, task, run domain.ID, feats []devcontainer.ForeignFeature) error {
	for _, f := range feats {
		if !domain.ValidFeatureRef(f.Ref) {
			return fmt.Errorf("feature source request: %q is not a reference that can be asked about", f.Ref)
		}
		if !domain.ValidDigest(f.Digest) {
			return fmt.Errorf("feature source request: %q has no manifest digest to ask about", f.Ref)
		}
	}
	if len(feats) == 0 {
		return nil
	}
	return s.update(ctx, task, func(a *domain.TaskAggregate) error {
		for _, f := range feats {
			if _, err := a.RaiseFeatureSource(run, s.cfg.NewID(), f.Ref, f.Digest, s.clock.Now()); err != nil {
				return fmt.Errorf("feature source request for %q: %w", f.Ref, err)
			}
		}
		return nil
	})
}

// keepFeatureAnswer stores the answer to a feature source request for the repository
// of its task, with the digest the human saw. Only an answered allow or deny is kept.
func (s *Service) keepFeatureAnswer(ctx context.Context, d domain.Decision, option string) error {
	if option != domain.AnswerAllow && option != domain.AnswerDeny {
		return nil
	}
	if !domain.ValidDigest(d.FeatureDigest) {
		// Raised before the digest was recorded: nothing is kept for the digest rule,
		// so the digest is asked again at the next start.
		s.report(fmt.Errorf("feature source %s: the Decision has no digest, so its answer is not kept", d.Feature))
		return nil
	}
	agg, err := s.store.LoadTask(ctx, d.TaskID)
	if err != nil {
		return err
	}
	return s.store.SetFeatureSource(ctx, agg.Task().Repo, d.Feature, d.FeatureDigest, option == domain.AnswerAllow, d.ID, s.clock.Now())
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
	s.mu.Lock()
	source := s.egressSources[d.ID] // what it was asked about; lost with a restart, which supersedes the request
	delete(s.egressSources, d.ID)
	s.mu.Unlock()
	return s.store.SetEgressHost(ctx, agg.Task().Repo, d.Host, allowed, d.ID, source, s.clock.Now())
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
		if d.RunID == run && d.Cause.AsksBeforeStart() && d.Status == domain.DecisionOpen {
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
	job := s.startDetached(ctx, run, w.finish)
	if job == nil {
		s.end(run, w.sl) // the supervisor is shutting down: the reconciler resumes the run later
		return nil
	}
	s.reportWhenDone(run, job) // nobody waits for this start, so its failure is reported here
	return nil
}

// startJob is an agent start in progress.
type startJob struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // how the start ended; valid after done is closed
}

// startDetached runs fn, the start of a run's agent, in a goroutine of its own,
// not on the context of the request that caused it: a caller that drops the
// connection must not fail a run whose answer was stored, and a slow postCreate
// must not hold the reconciler. At most one start runs per run: a second call
// returns true without starting another. It is tracked, so Shutdown cancels and
// waits for it, and Cancel stops it. It returns the job, which the caller may
// wait on, or nil when the supervisor is shutting down and nothing was started.
// A second call for a run whose start is running returns that start's job.
func (s *Service) startDetached(parent context.Context, run domain.ID, fn func(ctx context.Context) error) *startJob {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	if job, running := s.starts[run]; running {
		s.mu.Unlock()
		return job
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	job := &startJob{cancel: cancel, done: make(chan struct{})}
	if s.starts == nil {
		s.starts = map[domain.ID]*startJob{}
	}
	s.starts[run] = job
	s.wg.Add(1) // under s.mu, like attach: before Shutdown sets closing or not at all
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer close(job.done)
		err := fn(ctx)
		job.err = err
		s.mu.Lock()
		delete(s.starts, run)
		s.mu.Unlock()
		cancel()
	}()
	return job
}

// reportWhenDone reports how a start ended, if it failed and was not cancelled,
// for a caller that does not wait for it.
func (s *Service) reportWhenDone(run domain.ID, job *startJob) {
	go func() {
		<-job.done
		if job.err != nil && !errors.Is(job.err, context.Canceled) {
			s.report(fmt.Errorf("start run %s: %w", run, job.err))
		}
	}()
}

// cancelStart stops the start of a run, if one is in progress.
func (s *Service) cancelStart(run domain.ID) {
	s.mu.Lock()
	j := s.starts[run]
	s.mu.Unlock()
	if j != nil {
		j.cancel()
	}
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

// startDoneChan returns the channel that closes when the start of a run has
// ended, or nil when none is in progress.
func (s *Service) startDoneChan(run domain.ID) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.starts[run]; j != nil {
		return j.done
	}
	return nil
}
