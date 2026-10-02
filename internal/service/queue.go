package service

import (
	"context"
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
// skipped and remembered, so it asks again only after it is moved.
func (w *Workspaces) PollQueue(ctx context.Context) (int, error) {
	reader, ok := w.cfg.Issues.(forge.QueueReader)
	if !ok || w.cfg.QueueStatus == "" {
		return 0, nil
	}
	cards, err := reader.QueuedCards(ctx, w.cfg.QueueStatus)
	if err != nil {
		return 0, err
	}
	raised := 0
	for _, c := range cards {
		seen, ok, err := w.svc.store.QueueSeen(ctx, c.Repo, c.Issue)
		if err != nil {
			return raised, err
		}
		if ok && !c.UpdatedAt.After(seen) {
			continue
		}
		task, err := w.acceptCard(ctx, c)
		if err != nil {
			w.svc.report(fmt.Errorf("the card of %s#%d is not queued: %w", c.Repo, c.Issue, err))
		}
		if err := w.svc.store.RememberQueue(ctx, c.Repo, c.Issue, c.UpdatedAt, string(task)); err != nil {
			return raised, err
		}
		if task != "" {
			raised++
		}
	}
	return raised, nil
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
			return "", fmt.Errorf("its Session %q is not an agent of %s: set it to <workspace>/<role>", c.Agent, c.Repo)
		}
	case len(candidates) == 1:
		agent = candidates[0]
	default:
		return "", fmt.Errorf("%s has %d agents and the card names none: set its Session to <workspace>/<role>", c.Repo, len(candidates))
	}
	issue, err := w.cfg.Issues.GetIssue(ctx, c.Repo, c.Issue)
	if err != nil {
		return "", fmt.Errorf("load the issue: %w", err)
	}
	if w.cfg.Trust != nil {
		if err := w.cfg.Trust(issue); err != nil {
			return "", fmt.Errorf("the issue is not trusted to start a run: %w", err)
		}
	}
	res, err := w.holdFor(ctx, agent, byAgent[agent.ID], issue, c.Issue, true)
	return res.Task, err
}
