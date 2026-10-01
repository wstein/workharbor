package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
)

// Errors of preparing and publishing a topic.
var (
	ErrEnvRunning  = errors.New("the environment is still running")
	ErrNotReadyYet = errors.New("the task has no stopped run to prepare")
)

// Checker runs the repository's own checks on a prepared commit. They are the
// repository's code, so an implementation runs them in an environment, never
// on the host (design §4.5).
type Checker func(ctx context.Context, task domain.ID, sha string) error

// PublishConfig is what preparing and pushing a topic needs.
type PublishConfig struct {
	Repo  *hostgit.Repo  // the supervisor's own copy
	Cache *hostgit.Cache // the repository cache holding the target
	Guard *forge.Guard   // the forge, behind the autonomy policy
	// Prepare is the template for hostgit.Prepare: the bot, its key and the
	// message linter. Target and Topic are filled in per task.
	Prepare hostgit.PrepareSpec
	Checks  Checker
	// ForgeRepo is the repository name on the forge ("owner/name").
	ForgeRepo string
	Deepen    hostgit.DeepenOptions
}

// Publisher prepares topics for push and publishes the approved ones. The
// forge's autonomy policy is enforced by the Guard, not here.
type Publisher struct {
	svc *Service
	cfg PublishConfig
}

// NewPublisher returns a publisher for a service.
func NewPublisher(s *Service, cfg PublishConfig) *Publisher { return &Publisher{svc: s, cfg: cfg} }

// Request names the task's checkout.
type Request struct {
	Task     domain.ID
	Checkout string // the agent's checkout on the host
	Branch   string // the topic branch, agent/<topic>
	Target   string // the branch the topic is rebased onto
	// DecisionID is the ID for the "Ready to push?" Decision.
	DecisionID domain.ID
}

// Prepare gets a task ready for review (design §4.5): the environment is
// stopped first, not merely the run, so nothing in the guest can change the
// checkout between the checks and the fetch; the branch is fetched into the
// supervisor's copy; the target and a merge base are made available; the topic
// is rebased, folded, linted and signed; the repository's checks run in an
// environment; the commit is pinned and a "Ready to push?" Decision is raised
// for exactly that SHA. It returns the prepared commit.
func (p *Publisher) Prepare(ctx context.Context, req Request) (hostgit.Prepared, error) {
	s := p.svc
	agg, err := s.store.LoadTask(ctx, req.Task)
	if err != nil {
		return hostgit.Prepared{}, err
	}
	run, ok := agg.LiveRun()
	if ok {
		return hostgit.Prepared{}, fmt.Errorf("%w: run %s is %s", ErrNotReadyYet, run.ID, run.State)
	}
	runs := agg.Runs()
	if len(runs) == 0 || runs[len(runs)-1].State != domain.RunStopped {
		return hostgit.Prepared{}, ErrNotReadyYet
	}
	last := runs[len(runs)-1]
	if err := s.stopEnvironment(ctx, req.Task, last.EnvID); err != nil {
		return hostgit.Prepared{}, err
	}

	if _, err := p.cfg.Repo.FetchBranch(ctx, req.Checkout, req.Branch); err != nil {
		return hostgit.Prepared{}, err
	}
	if err := p.cfg.Cache.Refresh(ctx, req.Target); err != nil {
		return hostgit.Prepared{}, err
	}
	if err := p.cfg.Repo.FetchTarget(ctx, p.cfg.Cache, req.Target, 0); err != nil {
		return hostgit.Prepared{}, err
	}
	if _, err := p.cfg.Cache.EnsureMergeBase(ctx, p.cfg.Repo, req.Target, req.Branch, p.cfg.Deepen); err != nil {
		return hostgit.Prepared{}, err
	}
	spec := p.cfg.Prepare
	spec.Target, spec.Topic = req.Target, req.Branch
	prepared, err := p.cfg.Repo.Prepare(ctx, spec)
	if err != nil {
		return hostgit.Prepared{}, err
	}
	if p.cfg.Checks != nil {
		if err := p.cfg.Checks(ctx, req.Task, prepared.SHA); err != nil {
			return hostgit.Prepared{}, fmt.Errorf("the repository's checks failed: %w", err)
		}
	}

	err = s.update(ctx, req.Task, func(a *domain.TaskAggregate) error {
		if _, err := a.PinRevision(last.ID, req.Branch, prepared.SHA); err != nil {
			return err
		}
		if err := a.MarkReady(false); err != nil {
			return err
		}
		_, err := a.RaiseDecision(domain.NewDecision{
			ID: req.DecisionID, Kind: domain.DecisionReview, Blocking: true, SHA: prepared.SHA,
			Subject: fmt.Sprintf("Ready to push? %d commits on %s", len(prepared.Commits), req.Branch), Now: s.clock.Now(),
		})
		return err
	})
	return prepared, err
}

// stopEnvironment stops a task's environment and records it, so that the
// checkout is read only when nothing can run in the guest.
func (s *Service) stopEnvironment(ctx context.Context, task, env domain.ID) error {
	if err := s.rt.Stop(ctx, string(env)); err != nil {
		return fmt.Errorf("stop environment %s: %w", env, err)
	}
	info, err := s.rt.Inspect(ctx, string(env))
	if err != nil {
		return err
	}
	if info.State != domain.EnvStopped {
		return fmt.Errorf("%w: %s is %s", ErrEnvRunning, env, info.State)
	}
	return s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.ObserveEnv(env, domain.EnvStopped) })
}

// Publish pushes the approved commit and opens or updates the pull request
// (design §4.5). Nothing is sent unless the review Decision allows exactly the
// pinned commit: the Guard checks it again at the forge, so a Decision that
// was answered for an earlier revision never covers a later one.
func (p *Publisher) Publish(ctx context.Context, task, decision domain.ID, title, body string) (forge.PullRequest, error) {
	s := p.svc
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return forge.PullRequest{}, err
	}
	cand, ok := agg.CurrentCandidate()
	d, found := agg.Decision(decision)
	if !ok || !found || !d.Allows(cand.SHA) {
		return forge.PullRequest{}, fmt.Errorf("%w: decision %s does not allow the current revision", forge.ErrNotApproved, decision)
	}
	ap := forge.Approval{DecisionID: string(decision), SHA: cand.SHA}
	if err := p.cfg.Guard.Push(ctx, p.cfg.ForgeRepo, cand.Branch, ap); err != nil {
		return forge.PullRequest{}, err
	}
	var pr forge.PullRequest
	if cand.PRURL != "" {
		pr = forge.PullRequest{Repo: p.cfg.ForgeRepo, Branch: cand.Branch, URL: cand.PRURL, SHA: cand.SHA}
		pr.Number = parsePRNumber(cand.PRURL)
		if err := p.cfg.Guard.UpdatePR(ctx, pr, ap, title, body); err != nil {
			return forge.PullRequest{}, err
		}
	} else if pr, err = p.cfg.Guard.OpenPR(ctx, p.cfg.ForgeRepo, cand.Branch, ap, title, body); err != nil {
		return forge.PullRequest{}, err
	}
	err = s.update(ctx, task, func(a *domain.TaskAggregate) error { return a.RecordPR(cand.SHA, pr.URL) })
	return pr, err
}

func parsePRNumber(url string) int {
	n := 0
	mul := 1
	for i := len(url) - 1; i >= 0 && url[i] >= '0' && url[i] <= '9'; i-- {
		n += int(url[i]-'0') * mul
		mul *= 10
	}
	return n
}

// VerifierFor returns the forge.Verifier over the service's store: a commit is
// approved when the review Decision exists and was answered allow for exactly
// that SHA.
func (s *Service) VerifierFor() forge.Verifier { return storeVerifier{s} }

type storeVerifier struct{ s *Service }

func (v storeVerifier) Approved(ctx context.Context, ap forge.Approval) bool {
	d, err := v.s.store.LoadDecision(ctx, domain.ID(ap.DecisionID))
	return err == nil && d.Kind == domain.DecisionReview && d.Allows(ap.SHA)
}

// RepoPusher is a forge.Pusher that sends the branch with hostgit.
type RepoPusher struct {
	Repo   *hostgit.Repo
	Remote string
}

// Push implements forge.Pusher.
func (r RepoPusher) Push(ctx context.Context, _, branch, sha string) error {
	return r.Repo.Push(ctx, r.Remote, branch, sha)
}

// EditorCopy is what `whr open` and the web UI's editor launch return: the path
// of a supervisor-owned copy of the topic, never the agent's checkout, and the
// files in it that an editor may act on by itself, for the UI to warn about.
type EditorCopy struct {
	Path     string
	Warnings []string
	// Stale is set when the environment still runs: the copy is the last one
	// made from a stopped environment and was not refreshed, because hostgit
	// reads an agent's checkout only once nothing can run in the guest.
	Stale bool
}

// OpenCopy prepares the editor copy of a task's topic in dir. With the
// environment stopped it fetches the agent's branch and refreshes the copy by
// fast-forward; with the environment running it offers the last copy as stale,
// and fails if there is none yet (ErrEnvRunning).
func (p *Publisher) OpenCopy(ctx context.Context, req Request, dir string) (EditorCopy, error) {
	agg, err := p.svc.store.LoadTask(ctx, req.Task)
	if err != nil {
		return EditorCopy{}, err
	}
	runs := agg.Runs()
	if len(runs) == 0 {
		return EditorCopy{}, ErrNotReadyYet
	}
	env, ok := agg.Environment(runs[len(runs)-1].EnvID)
	if !ok {
		return EditorCopy{}, ErrNotReadyYet
	}
	info, err := p.svc.rt.Inspect(ctx, string(env.ID))
	if err != nil {
		return EditorCopy{}, err
	}
	if info.State == domain.EnvRunning {
		if _, err := p.cfg.Repo.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+req.Branch); err != nil {
			return EditorCopy{}, fmt.Errorf("%w: no copy has been made yet", ErrEnvRunning)
		}
		warn, err := p.cfg.Repo.EditorCopy(ctx, dir, req.Branch)
		return EditorCopy{Path: dir, Warnings: warn, Stale: true}, err
	}
	if _, err := p.cfg.Repo.FetchBranch(ctx, req.Checkout, req.Branch); err != nil {
		return EditorCopy{}, err
	}
	warn, err := p.cfg.Repo.EditorCopy(ctx, dir, req.Branch)
	return EditorCopy{Path: dir, Warnings: warn}, err
}
