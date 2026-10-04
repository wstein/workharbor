package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Defaults of the publish retries (D51).
const (
	// DefaultPublishBackoff is the wait after a publish's first transport fault;
	// it doubles with every failure up to DefaultPublishBackoffMax.
	DefaultPublishBackoff    = 30 * time.Second
	DefaultPublishBackoffMax = 15 * time.Minute
)

// PipelineConfig is what the publish pipeline needs of the host.
type PipelineConfig struct {
	// Publish returns how a forge repository ("owner/name") is prepared and
	// published: its supervisor copy, the Guard, the committer, the check.
	Publish func(ctx context.Context, repo string) (PublishConfig, error)
	// Text returns the title and body of the pull request for a prepared task. It
	// must use supervisor facts only. Default: the branch and the issue.
	Text func(domain.Task, domain.ReviewCandidate) (title, body string)
	// BackoffBase and BackoffMax shape the retries of a publish that failed on a
	// transport fault; they default to DefaultPublishBackoff and
	// DefaultPublishBackoffMax.
	BackoffBase, BackoffMax time.Duration
}

// Pipeline joins prepare, the "Ready to push?" Decision, the guarded push and
// the pull request into the one path of design §4.5, "Publishing in production"
// (D51): a run that stops is prepared, an allow of the review Decision is
// published, and what a restart interrupted is completed by the reconciler.
// Every change goes through the aggregate and the Guard; nothing here reaches the
// forge on its own.
type Pipeline struct {
	svc *Service
	w   *Workspaces
	cfg PipelineConfig

	mu   sync.Mutex
	busy map[domain.ID]bool // tasks a prepare or a publish is running for
	// reworking counts the answers of rework in progress per task: from the
	// moment the answer is recorded until the new run is saved or the rework
	// failed and is recorded. Kick leaves such a task alone.
	reworking map[domain.ID]int
	retry     map[domain.ID]*publishRetry
}

// publishRetry is the backoff of one task's outstanding publish, in memory: a
// restart forgets it and the first pass tries at once.
type publishRetry struct {
	sha      string
	attempts int
	next     time.Time
}

// NewPipeline makes the service prepare a task when its run stops, publish an
// approved commit and complete an interrupted publish. It is set up before the
// service starts any run.
func NewPipeline(s *Service, w *Workspaces, cfg PipelineConfig) *Pipeline {
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = DefaultPublishBackoff
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = DefaultPublishBackoffMax
	}
	p := &Pipeline{svc: s, w: w, cfg: cfg, busy: map[domain.ID]bool{}, reworking: map[domain.ID]int{}, retry: map[domain.ID]*publishRetry{}}
	s.pipe = p
	if w != nil {
		w.pipe = p
	}
	return p
}

// spawn runs fn for a task in the background, one at a time per task, under the
// service's wait group and context (cancelled by Shutdown). It reports false and
// runs nothing when one is running already or the service is closing.
func (p *Pipeline) spawn(task domain.ID, fn func(ctx context.Context)) bool {
	s := p.svc
	p.mu.Lock()
	if p.busy[task] {
		p.mu.Unlock()
		return false
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		p.mu.Unlock()
		return false
	}
	s.wg.Add(1) // under s.mu, as attach does: before Shutdown sets closing, or not at all
	s.mu.Unlock()
	p.busy[task] = true
	p.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			p.mu.Lock()
			delete(p.busy, task)
			p.mu.Unlock()
		}()
		fn(s.bg)
	}()
	return true
}

// holdRework keeps Kick, and so the reconciler, off the task until the returned
// release is called (more than once is harmless). An answer of rework takes it
// before the answer is recorded: the recorded answer already makes the old run
// look unprepared or the old approval look outstanding, and the new run is saved
// only after its environment is up, so without it a pass in between would push
// what the human chose to rework, or prepare the old run and make the new one
// refused as busy.
func (p *Pipeline) holdRework(task domain.ID) (release func()) {
	p.mu.Lock()
	p.reworking[task]++
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.reworking[task]--; p.reworking[task] <= 0 {
				delete(p.reworking, task)
			}
		})
	}
}

func (p *Pipeline) reworkHeld(task domain.ID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reworking[task] > 0
}

// afterStop prepares the topic of a run that stopped because its agent finished.
// The environment's busy mark was taken before the run was saved stopped and is
// released when the prepare has pinned a revision or been refused; when nothing
// can be started it is released here, and the reconciler repeats the prepare.
func (p *Pipeline) afterStop(task domain.ID, release func()) {
	if !p.spawn(task, func(ctx context.Context) {
		defer release()
		p.prepare(ctx, task)
	}) {
		release()
	}
}

// Kick starts what is due for a task, if anything: the prepare of a stopped run
// nothing was prepared for, or the publish of an approved commit that is not
// recorded pushed. The reconciler calls it for every task, and an answer calls
// it for its own task, so a restart anywhere in the path loses nothing. It
// returns what it started.
func (p *Pipeline) Kick(ctx context.Context, task domain.ID) (started string, err error) {
	s := p.svc
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return "", err
	}
	// After the load: an answer takes its hold before it is recorded, so a state
	// that shows the answer is always seen with the hold in place.
	if p.reworkHeld(task) {
		return "", nil
	}
	if run, ok := agg.PreparePending(); ok {
		a, err := s.store.Agent(ctx, run.AgentID)
		if err != nil {
			return "", err
		}
		ws, err := s.store.Workspace(ctx, string(a.WorkspaceID))
		if err != nil {
			return "", err
		}
		// Held like the prepare of a run that has just stopped: no run starts in
		// the environment, no other task's commits reach the export. Refused while
		// another run owns it: the next pass tries again.
		release, err := s.HoldEnvironment(ctx, ws)
		if err != nil {
			var c *domain.ConflictError
			if errors.As(err, &c) {
				return "", nil
			}
			return "", err
		}
		if !p.spawn(task, func(ctx context.Context) {
			defer release()
			p.prepare(ctx, task)
		}) {
			release()
			return "", nil
		}
		return "prepare", nil
	}
	if _, cand, ok := agg.OutstandingPublish(); ok {
		if p.backingOff(task, cand.SHA) {
			return "", nil
		}
		if p.spawn(task, func(ctx context.Context) { p.publish(ctx, task) }) {
			return "publish", nil
		}
	}
	return "", nil
}

func (p *Pipeline) backingOff(task domain.ID, sha string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.retry[task]
	return r != nil && r.sha == sha && p.svc.clock.Now().Before(r.next)
}

// prepare prepares the task's stopped run for review: the export, Prepare, the
// check and the pin, then "Ready to push?" for the pinned SHA. A refusal is the
// blocking question prepare_failed for the stopped run. A prepare that ended
// because the supervisor is shutting down raises nothing: the reconciler repeats
// it, which is safe because it starts again from the bundle.
func (p *Pipeline) prepare(ctx context.Context, task domain.ID) {
	s := p.svc
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		s.report(fmt.Errorf("prepare %s: %w", task, err))
		return
	}
	run, ok := agg.PreparePending()
	if !ok {
		return
	}
	err = p.doPrepare(ctx, agg.Task(), run)
	if err == nil || ctx.Err() != nil || errors.Is(err, ErrRebaseBlocked) {
		return // done, shutting down, or the rebase's own question is raised
	}
	if rerr := p.refusePrepare(context.WithoutCancel(ctx), task, run.ID, err); rerr != nil {
		s.report(fmt.Errorf("prepare %s: %w (and the question could not be raised: %w)", task, err, rerr))
	}
}

func (p *Pipeline) doPrepare(ctx context.Context, t domain.Task, run domain.Run) error {
	s := p.svc
	a, err := s.store.Agent(ctx, run.AgentID)
	if err != nil {
		return err
	}
	ws, err := s.store.Workspace(ctx, string(a.WorkspaceID))
	if err != nil {
		return err
	}
	cfg, err := p.cfg.Publish(ctx, t.Repo)
	if err != nil {
		return fmt.Errorf("the repository %s is not set up for publishing: %w", t.Repo, err)
	}
	_, err = NewPublisher(s, cfg).Prepare(ctx, Request{
		Task: t.ID, Agent: a.ID, Branch: a.Branch, Target: ws.Integration, DecisionID: s.cfg.NewID(),
	})
	return err
}

// refusePrepare raises prepare_failed for the stopped run: the reason, and the
// check's capped output when it was the check that failed, both as untrusted
// input (§4.2).
func (p *Pipeline) refusePrepare(ctx context.Context, task, run domain.ID, cause error) error {
	s := p.svc
	return s.update(ctx, task, func(a *domain.TaskAggregate) error {
		if _, ok := a.PreparePending(); !ok {
			return nil // answered or changed meanwhile: nothing to ask
		}
		_, err := a.RaisePrepareFailed(run, s.cfg.NewID(), s.untrustedInput(prepareFailedInput(cause)), s.clock.Now())
		return err
	})
}

// prepareFailedInput is the reason and the check's output tail as the input of
// prepare_failed. The Decision keeps the first MaxDecisionInput characters, so the
// tail of the output is cut to fit after the reason.
func prepareFailedInput(cause error) string {
	reason := textsafe.Escape(oneLine(cause.Error()))
	var ce *CheckError
	if !errors.As(cause, &ce) || ce.Output == "" {
		return reason
	}
	budget := domain.MaxDecisionInput - utf8.RuneCountInString(reason) - 2
	if budget <= 0 {
		return reason
	}
	return reason + "\n\n" + tailRunes(ce.Output, budget)
}

// untrustedInput redacts what the supervisor knows to be secret from text made
// of an error or a tool's output, before it is stored.
func (s *Service) untrustedInput(text string) string {
	return string(s.store.Redact([]byte(text)))
}

// publish completes the task's outstanding publish: the push and the pull
// request, each step idempotent for the approved SHA and checked again by the
// Guard (design §4.5). A transport fault leaves it outstanding, to be retried
// with backoff, and every failure is an event on the task; a refusal a retry
// cannot change ends it with the question publish_failed.
func (p *Pipeline) publish(ctx context.Context, task domain.ID) {
	s := p.svc
	agg, err := s.store.LoadTask(ctx, task)
	if err != nil {
		s.report(fmt.Errorf("publish %s: %w", task, err))
		return
	}
	d, cand, ok := agg.OutstandingPublish()
	if !ok {
		return
	}
	cfg, err := p.cfg.Publish(ctx, agg.Task().Repo)
	if err == nil {
		title, body := p.text(agg.Task(), cand)
		_, err = NewPublisher(s, cfg).Publish(ctx, task, d.ID, title, body)
	}
	if err == nil {
		p.mu.Lock()
		delete(p.retry, task)
		p.mu.Unlock()
		return
	}
	if ctx.Err() != nil {
		return // shutting down: still outstanding, the next start completes it
	}
	if rerr := p.failPublish(context.WithoutCancel(ctx), task, cand.SHA, err); rerr != nil {
		s.report(fmt.Errorf("publish %s: %w (and its record failed: %w)", task, err, rerr))
	}
}

func (p *Pipeline) text(t domain.Task, c domain.ReviewCandidate) (string, string) {
	if p.cfg.Text != nil {
		return p.cfg.Text(t, c)
	}
	title := "workharbor: " + c.Branch
	if t.Issue != "" {
		title += " (" + t.Issue + ")"
	}
	return title, fmt.Sprintf("Prepared and approved through workharbor.\n\nTask: %s\nCommit: %s\n", t.ID, c.SHA)
}

// failPublish records a failed publish: a transport fault keeps it outstanding
// with a backoff, anything else ends it with publish_failed.
func (p *Pipeline) failPublish(ctx context.Context, task domain.ID, sha string, cause error) error {
	s := p.svc
	msg := s.untrustedInput(textsafe.Escape(oneLine(cause.Error())))
	if Transient(cause) {
		p.mu.Lock()
		r := p.retry[task]
		if r == nil || r.sha != sha {
			r = &publishRetry{sha: sha}
			p.retry[task] = r
		}
		r.attempts++
		wait := p.cfg.BackoffBase
		for i := 1; i < r.attempts && wait < p.cfg.BackoffMax; i++ {
			wait *= 2
		}
		wait = min(wait, p.cfg.BackoffMax)
		r.next = s.clock.Now().Add(wait)
		attempt := domain.PublishAttempt{SHA: sha, Attempt: r.attempts, Transient: true, Error: msg, RetryAt: r.next}
		p.mu.Unlock()
		return p.recordAttempt(ctx, task, attempt)
	}
	var raised bool
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		raised = false
		if _, _, ok := a.OutstandingPublish(); !ok {
			return nil // cancelled, superseded or already asked
		}
		if _, err := a.RaisePublishFailed(s.cfg.NewID(), msg, s.clock.Now()); err != nil {
			return err
		}
		raised = true
		return nil
	})
	if err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.retry, task)
	p.mu.Unlock()
	if !raised {
		return nil
	}
	return p.recordAttempt(ctx, task, domain.PublishAttempt{SHA: sha, Attempt: 1, Error: msg})
}

func (p *Pipeline) recordAttempt(ctx context.Context, task domain.ID, a domain.PublishAttempt) error {
	s := p.svc
	saved, err := s.store.Append(ctx, domain.NewPublishAttemptEvent(task, a, s.clock.Now()))
	if err != nil {
		return err
	}
	s.publish(saved)
	return nil
}

// Transient reports whether a failed publish step is a transport fault that
// trying again can fix: the network, a timeout, a server error or a rate limit.
// It is false for every refusal (not a fast-forward, a target the Guard refuses,
// no approval for the SHA, forbidden, a missing installation) and for what is
// unknown: only a known transport fault keeps a publish outstanding (design
// §4.5).
func Transient(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, forge.ErrNotApproved), errors.Is(err, forge.ErrForbidden), errors.Is(err, forge.ErrNotFastForward),
		errors.Is(err, hostgit.ErrNotFastForward), errors.Is(err, forge.ErrTarget), errors.Is(err, forge.ErrSHAMismatch), errors.Is(err, forge.ErrBranch):
		return false
	case errors.Is(err, hostgit.ErrTransport), errors.Is(err, forge.ErrTransient), errors.Is(err, context.DeadlineExceeded):
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

// reworkNotes is the part of a refused-prepare or refused-publish Decision's input
// the agent's next run is told: the reason and the last lines of the output, each
// shortened when the briefing is built, passed on as data and never as an
// instruction.
func reworkNotes(input string) string {
	var lines []string
	for _, l := range strings.Split(input, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	const keep = 12
	if len(lines) > keep {
		lines = append(lines[:1:1], lines[len(lines)-keep+1:]...)
	}
	return strings.Join(lines, "\n")
}
