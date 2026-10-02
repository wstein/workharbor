package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
)

// DefaultQueueEvery is how often the board's agent queue is read: polling, since a
// webhook needs an address the forwarder does not publish (D29).
const DefaultQueueEvery = time.Minute

// maxQueueQuestions caps the "Accept this task?" questions one poll raises: a board
// writer who moves many cards at once does not flood the human, and the cards left
// over are read again by the next poll.
const maxQueueQuestions = 5

// refusal marks an acceptCard error that is a decision about the card, not a failure
// to read something: it will come out the same until the card is moved again.
type refusal struct{ error }

func (r refusal) Unwrap() error { return r.error }

// RunQueue reads the board's agent queue every interval until ctx ends (design D30,
// D40, issue #71). It does nothing without a queue column or a forge that can read
// the board. Each pass is PollQueue; a failure is reported and the next pass tries
// again.
func (w *Workspaces) RunQueue(ctx context.Context, every time.Duration) {
	if w.cfg.QueueStatus == "" {
		return
	}
	if every <= 0 {
		every = DefaultQueueEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := w.PollQueue(ctx); err != nil && ctx.Err() == nil {
			w.svc.report(fmt.Errorf("the agent queue: %w", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// PollQueue reads the cards in the queue column once and raises an "Accept this
// task?" Decision for each that is new or was moved again since it was last acted on.
// A card never starts a run: the task waits, queued, until the human accepts it, and
// a card nobody accepts never runs. It returns how many Decisions it raised. A card
// is acted on once per state: one whose agent cannot be found, whose issue is not
// trusted to start a run or that already has an unfinished task is reported or
// skipped and remembered, so it asks again only after it is moved. A transient
// failure (the store, the forge) is reported and not remembered, so a later poll
// tries the card again after a backoff of 1, 2, 4 ... polls (at most maxBackoffPolls),
// and the error is reported once. A poll raises at most maxQueueQuestions questions
// and starts after the card handled last, so a failing card cannot starve the others.
func (w *Workspaces) PollQueue(ctx context.Context) (int, error) {
	reader, ok := w.cfg.Issues.(forge.QueueReader)
	if !ok || w.cfg.QueueStatus == "" {
		return 0, nil
	}
	cards, err := reader.QueuedCards(ctx, w.cfg.QueueStatus)
	if err != nil {
		return 0, err
	}
	w.failMu.Lock()
	w.polls++
	poll, start := w.polls, 0
	inQueue := make(map[string]bool, len(cards))
	for i, c := range cards {
		key := cardKey(c)
		inQueue[key] = true
		if key == w.lastCard {
			start = i + 1 // begin after the card handled last, so later cards are reached
		}
	}
	for key := range w.failures {
		if !inQueue[key] {
			delete(w.failures, key) // the card left the queue: its backoff ends
		}
	}
	w.failMu.Unlock()
	raised, tried := 0, 0
	for i := range cards {
		if raised >= maxQueueQuestions || tried >= maxQueueQuestions {
			break
		}
		c := cards[(start+i)%len(cards)]
		if w.backedOff(c, poll) {
			continue // a skipped card does not count toward the cap
		}
		seen, ok, err := w.svc.store.QueueSeen(ctx, c.Repo, c.Issue)
		if err != nil {
			return raised, err
		}
		if ok && !c.UpdatedAt.After(seen) {
			continue
		}
		w.setLast(c)
		task, err := w.acceptCard(ctx, c)
		refused := err != nil && errors.As(err, new(refusal))
		if err != nil {
			tried++ // a failing card counts toward the cap, so a bad board does not cost unbounded forge calls
			if w.firstFailure(c, err, poll, refused) {
				w.svc.report(fmt.Errorf("the card of %s#%d is not queued: %w", c.Repo, c.Issue, err))
			}
			if !refused {
				continue // transient: not remembered, so a later poll tries again
			}
		} else {
			w.clearFailure(c)
		}
		if err := w.svc.store.RememberQueue(ctx, c.Repo, c.Issue, c.UpdatedAt, string(task)); err != nil {
			return raised, err
		}
		if refused {
			w.clearFailure(c) // remembered in the store: asked again only after a move, and reported again then
		}
		if task != "" {
			raised++
		}
	}
	return raised, nil
}

// maxFailureMemory bounds the failures remembered so each is reported once.
const maxFailureMemory = 1000

// maxBackoffPolls caps how many polls a failing card is skipped: one hour of polls at
// DefaultQueueEvery. It is a count of polls, so a different interval stretches it.
const maxBackoffPolls = int(time.Hour / DefaultQueueEvery)

// cardFailure is what the queue remembers of a card that fails: the error text last
// reported, the consecutive transient failures, and the poll it is tried again at.
type cardFailure struct {
	text    string
	count   int
	retryAt uint64
}

func cardKey(c forge.QueuedCard) string { return c.Repo + "#" + strconv.Itoa(c.Issue) }

func (w *Workspaces) setLast(c forge.QueuedCard) {
	w.failMu.Lock()
	w.lastCard = cardKey(c)
	w.failMu.Unlock()
}

// backedOff reports whether the card is still being skipped at this poll.
func (w *Workspaces) backedOff(c forge.QueuedCard, poll uint64) bool {
	w.failMu.Lock()
	defer w.failMu.Unlock()
	f := w.failures[cardKey(c)]
	return f != nil && poll < f.retryAt
}

// firstFailure records a failure of the card at this poll and reports whether this is
// the first time it fails with this error text since it last succeeded or the process
// started. A transient failure doubles the polls the card is skipped (1, 2, 4 ... up to
// maxBackoffPolls); a refusal is remembered in the store, so it needs no backoff. The
// memory is bounded: when full it is dropped, which at worst reports a failure again.
func (w *Workspaces) firstFailure(c forge.QueuedCard, err error, poll uint64, refused bool) bool {
	key, text := cardKey(c), err.Error()
	w.failMu.Lock()
	defer w.failMu.Unlock()
	f := w.failures[key]
	if f == nil {
		if w.failures == nil || len(w.failures) >= maxFailureMemory {
			w.failures = map[string]*cardFailure{}
		}
		f = &cardFailure{}
		w.failures[key] = f
	}
	first := f.text != text
	f.text = text
	if refused {
		f.count, f.retryAt = 0, 0
		return first
	}
	f.count++
	skip := maxBackoffPolls
	if f.count-1 < 6 && 1<<(f.count-1) < skip {
		skip = 1 << (f.count - 1)
	}
	f.retryAt = poll + 1 + uint64(skip)
	return first
}

func (w *Workspaces) clearFailure(c forge.QueuedCard) {
	w.failMu.Lock()
	delete(w.failures, cardKey(c))
	w.failMu.Unlock()
}

// acceptCard holds a task for one card and returns its ID, or "" when there is nothing
// to ask: the repository is not worked on here, or the issue already has an unfinished
// task.
func (w *Workspaces) acceptCard(ctx context.Context, c forge.QueuedCard) (domain.ID, error) {
	list, err := w.svc.store.Workspaces(ctx)
	if err != nil {
		return "", err
	}
	var candidates []domain.Agent
	byAgent := map[domain.ID]domain.Workspace{}
	for _, ws := range list {
		if !strings.EqualFold(ws.Repo, c.Repo) {
			continue
		}
		agents, err := w.svc.store.Agents(ctx, ws.ID)
		if err != nil {
			return "", err
		}
		for _, a := range agents {
			candidates = append(candidates, a)
			byAgent[a.ID] = ws
		}
	}
	if len(candidates) == 0 {
		return "", nil // not a repository of this supervisor
	}
	active, err := w.svc.store.Tasks(ctx, true)
	if err != nil {
		return "", err
	}
	for _, t := range active {
		if strings.EqualFold(t.Repo, c.Repo) && t.Issue == "#"+strconv.Itoa(c.Issue) {
			return "", nil // already queued or running: one task per issue at a time
		}
	}
	var agent domain.Agent
	switch {
	case c.Agent != "":
		wsName, role, _ := strings.Cut(c.Agent, "/")
		found := false
		for _, a := range candidates {
			if byAgent[a.ID].Name == wsName && a.Role == role {
				agent, found = a, true
			}
		}
		if !found {
			return "", refusal{fmt.Errorf("its Session %q is not an agent of %s: set it to <workspace>/<role>", c.Agent, c.Repo)}
		}
	case len(candidates) == 1:
		agent = candidates[0]
	default:
		return "", refusal{fmt.Errorf("%s has %d agents and the card names none: set its Session to <workspace>/<role>", c.Repo, len(candidates))}
	}
	issue, err := w.cfg.Issues.GetIssue(ctx, c.Repo, c.Issue)
	if err != nil {
		return "", fmt.Errorf("load the issue: %w", err)
	}
	if w.cfg.Trust != nil {
		if err := w.cfg.Trust(issue); err != nil {
			return "", refusal{fmt.Errorf("the issue is not trusted to start a run: %w", err)}
		}
	}
	res, err := w.holdFor(ctx, agent, byAgent[agent.ID], issue, c.Issue, true)
	return res.Task, err
}
