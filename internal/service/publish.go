package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/policy"
)

// Errors of preparing and publishing a topic.
var (
	ErrNotReadyYet = errors.New("the task has no stopped run to prepare")
	ErrNoChecks    = errors.New("the repository's checks are not configured")
	ErrNotReview   = errors.New("the task is not ready for review")
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
	// Workflow is the repository's preset (D47); empty means integration. It
	// chooses the table the Guard decides with and where approved commits go.
	Workflow policy.Preset
	// Branch is the integration branch approved commits go to: the target of the
	// prototype's fast-forward and of the integration workflow's pull request. A
	// published repository's pull request goes to the default branch and has none.
	Branch string
	Deepen hostgit.DeepenOptions
	// Workspaces lets Prepare and OpenCopy export an agent's branch out of its
	// environment as a bundle (D42). Without it only the older fetch from a
	// stopped environment's checkout works.
	Workspaces *Workspaces
	// MaxBundle and ExportTimeout bound an export; they default to
	// DefaultMaxBundle and DefaultExportTimeout.
	MaxBundle     int64
	ExportTimeout time.Duration
}

// Publisher prepares topics for push and publishes the approved ones. The
// forge's autonomy policy is enforced by the Guard, not here.
type Publisher struct {
	svc *Service
	cfg PublishConfig
}

// NewPublisher returns a publisher for a service.
func NewPublisher(s *Service, cfg PublishConfig) *Publisher { return &Publisher{svc: s, cfg: cfg} }

// Request names the task and the agent whose branch is prepared or opened.
type Request struct {
	Task domain.ID
	// Agent is the agent whose branch leaves its environment as a bundle (D42,
	// §4.5): the host runs git in no workspace and the environment keeps running.
	Agent  domain.ID
	Branch string // the agent's branch, agent/<role>
	Target string // the branch the branch is rebased onto
	// DecisionID is the ID for the "Ready to push?" Decision.
	DecisionID domain.ID
}

// Prepare gets a task ready for review (design §4.5): the target is refreshed
// into the supervisor's repository, the agent's branch is exported from its
// environment as a bundle and imported (a first round is rebased first), a merge
// base is made available, the branch is rebased, folded, linted and signed, the
// repository's checks run in an environment, the commit is pinned and a "Ready
// to push?" Decision is raised for exactly that SHA. It returns the prepared
// commit.
func (p *Publisher) Prepare(ctx context.Context, req Request) (hostgit.Prepared, error) {
	s := p.svc
	if p.cfg.Checks == nil {
		return hostgit.Prepared{}, ErrNoChecks
	}
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
	_, pushed := agg.LastPushed()
	// The bundle's prerequisite is a commit of the target's history, so the
	// target comes first. A rebase before the export is only for the first
	// round: later rounds are fast-forwards and pushed commits are never
	// rewritten (§4.5).
	if err := p.fetchTarget(ctx, req.Target); err != nil {
		return hostgit.Prepared{}, err
	}
	if err := p.exportBranch(ctx, req, last.ID, !pushed); err != nil {
		return hostgit.Prepared{}, err
	}
	if _, err := p.cfg.Cache.EnsureMergeBase(ctx, p.cfg.Repo, req.Target, req.Branch, p.cfg.Deepen); err != nil {
		return hostgit.Prepared{}, err
	}
	spec := p.cfg.Prepare
	spec.Target, spec.Topic = req.Target, req.Branch
	// After a push, only the agent's newer commits are rebased onto the pushed
	// revision: pushed commits are never rewritten (design §4.5).
	if pushed, ok := agg.LastPushed(); ok {
		if pushed.Source == "" {
			return hostgit.Prepared{}, fmt.Errorf("%w: the pushed revision %s does not record its source", hostgit.ErrHistoryRewritten, pushed.SHA)
		}
		spec.Onto, spec.Upstream = pushed.SHA, pushed.Source
	}
	prepared, err := p.cfg.Repo.Prepare(ctx, spec)
	if err != nil {
		return hostgit.Prepared{}, err
	}
	if err := p.cfg.Checks(ctx, req.Task, prepared.SHA); err != nil {
		return hostgit.Prepared{}, fmt.Errorf("the repository's checks failed: %w", err)
	}

	err = s.update(ctx, req.Task, func(a *domain.TaskAggregate) error {
		if _, err := a.PinPrepared(last.ID, req.Branch, prepared.SHA, prepared.Source); err != nil {
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

// fetchTarget refreshes the mirror from the forge and brings the target into
// the supervisor's repository.
func (p *Publisher) fetchTarget(ctx context.Context, target string) error {
	if err := p.cfg.Cache.Refresh(ctx, target); err != nil {
		return err
	}
	return p.cfg.Repo.FetchTarget(ctx, p.cfg.Cache, target, 0)
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
	if agg.Task().State != domain.TaskReadyForReview {
		return forge.PullRequest{}, fmt.Errorf("%w: it is %s", ErrNotReview, agg.Task().State)
	}
	cand, ok := agg.CurrentCandidate()
	d, found := agg.Decision(decision)
	if !ok || !found || !d.Allows(cand.SHA) {
		return forge.PullRequest{}, fmt.Errorf("%w: decision %s does not allow the current revision", forge.ErrNotApproved, decision)
	}
	ap := forge.Approval{DecisionID: string(decision), SHA: cand.SHA}
	// The guard decides with the run's context: an untrusted input asks where the
	// table would let an action run on its own. The supervisor does not know
	// whether the repository is private, so it assumes it is (design §7.1).
	preset := effectivePreset(agg.Task().Workflow, p.cfg.Workflow)
	branch := p.branchOf(agg.Task())
	guard := p.cfg.Guard.WithTable(preset.Table()).For(policy.Context{UntrustedInput: agg.Task().Untrusted, PrivateData: true, Egress: true})
	if err := guard.Push(ctx, p.cfg.ForgeRepo, cand.Branch, ap); err != nil {
		return forge.PullRequest{}, err
	}
	var pr forge.PullRequest
	switch {
	case !preset.OpensPR():
		// prototype: the integration branch moves to the approved commit, only as
		// a fast-forward and never forced; there is no pull request (D47).
		if branch == "" {
			return forge.PullRequest{}, errors.New("the prototype workflow needs the integration branch to move")
		}
		if err := guard.FastForward(ctx, p.cfg.ForgeRepo, cand.Branch, branch, ap); err != nil {
			return forge.PullRequest{}, err
		}
		pr = forge.PullRequest{Repo: p.cfg.ForgeRepo, Branch: cand.Branch, SHA: cand.SHA}
	case cand.PRURL != "":
		pr = forge.PullRequest{Repo: p.cfg.ForgeRepo, Branch: cand.Branch, URL: cand.PRURL, SHA: cand.SHA}
		pr.Number = parsePRNumber(cand.PRURL)
		if err := guard.UpdatePR(ctx, pr, ap, title, body); err != nil {
			return forge.PullRequest{}, err
		}
	default:
		var err error
		if base := p.prBase(preset, branch); base != "" {
			pr, err = guard.OpenPRInto(ctx, p.cfg.ForgeRepo, base, cand.Branch, ap, title, body)
		} else {
			pr, err = guard.OpenPR(ctx, p.cfg.ForgeRepo, cand.Branch, ap, title, body)
		}
		if err != nil {
			return forge.PullRequest{}, err
		}
	}
	err = s.update(ctx, task, func(a *domain.TaskAggregate) error {
		if err := a.RecordPushed(cand.SHA); err != nil {
			return err
		}
		if pr.URL == "" { // no pull request: the prototype moved the branch
			return nil
		}
		return a.RecordPR(cand.SHA, pr.URL)
	})
	return pr, err
}

// effectivePreset is the preset a task publishes under: the stricter of its own,
// from when it started, and the repository's current one, so a looser preset never
// applies to a run already started and a stricter one applies at once (D47, §6).
// Without a task preset the repository's is used, and without either the default.
func effectivePreset(task string, repo policy.Preset) policy.Preset {
	own := policy.Preset("")
	if p, err := policy.ParsePreset(task); err == nil && task != "" {
		own = p
	}
	switch {
	case own == "" && repo == "":
		return policy.DefaultPreset
	case own == "":
		return repo
	case repo == "":
		return own
	}
	return policy.Stricter(own, repo)
}

// branchOf is the integration branch a task publishes to: the one it started
// with, and the repository's current one only for a task from before it was
// recorded (D47, §6).
func (p *Publisher) branchOf(t domain.Task) string {
	if t.Branch != "" {
		return t.Branch
	}
	return p.cfg.Branch
}

// prBase is the branch a pull request goes into: the integration branch, or the
// default branch (empty) for a published repository.
func (p *Publisher) prBase(preset policy.Preset, branch string) string {
	if preset.ToDefaultBranch() {
		return ""
	}
	return branch
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
// that SHA, its task is ready for review, and the SHA is the task's current
// revision. A cancelled task, or an approval of an earlier revision, approves
// nothing.
func (s *Service) VerifierFor() forge.Verifier { return storeVerifier{s} }

type storeVerifier struct{ s *Service }

func (v storeVerifier) Approved(ctx context.Context, ap forge.Approval) bool {
	d, err := v.s.store.LoadDecision(ctx, domain.ID(ap.DecisionID))
	if err != nil || d.Kind != domain.DecisionReview || !d.Allows(ap.SHA) {
		return false
	}
	agg, err := v.s.store.LoadTask(ctx, d.TaskID)
	if err != nil || agg.Task().State != domain.TaskReadyForReview {
		return false
	}
	cand, ok := agg.CurrentCandidate()
	return ok && cand.SHA == ap.SHA
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
// of a supervisor-owned copy of the branch, never the agent's worktree, and the
// files in it that an editor may act on by itself, for the UI to warn about.
type EditorCopy struct {
	Path     string
	Warnings []string
}

// OpenCopy prepares the editor copy of an agent's branch in dir: the branch is
// exported from the environment, which keeps running, so the copy is fresh, and
// the copy is cloned from the supervisor's repository, never from the workspace;
// a later call refreshes it by fast-forward and never overwrites the
// developer's edits.
func (p *Publisher) OpenCopy(ctx context.Context, req Request, dir string) (EditorCopy, error) {
	if err := p.fetchTarget(ctx, req.Target); err != nil {
		return EditorCopy{}, err
	}
	if err := p.exportBranch(ctx, req, "", false); err != nil {
		return EditorCopy{}, err
	}
	warn, err := p.cfg.Repo.EditorCopy(ctx, dir, req.Branch)
	return EditorCopy{Path: dir, Warnings: warn}, err
}
