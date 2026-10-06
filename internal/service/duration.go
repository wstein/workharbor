package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// durationAdmission catches up before any execution, independently of adapter usage.
func (s *Service) durationAdmission(ctx context.Context, task, run domain.ID) error {
	if s.cfg.Budgets.PerRun.MaxDuration > 0 || s.cfg.Budgets.PerTask.MaxDuration > 0 {
		if err := s.update(ctx, task, func(a *domain.TaskAggregate) error { return s.accountDuration(a) }); err != nil {
			return err
		}
	}
	s.checkBudgets(ctx, task, run)
	a, err := s.store.LoadTask(ctx, task)
	if err != nil {
		return err
	}
	r, ok := a.Run(run)
	if !ok || r.State.Terminal() || a.Task().State.Terminal() {
		return fmt.Errorf("run %s: execution refused after terminal transition", run)
	}
	return nil
}

// sweepDurations runs before adapter observations, so an unavailable runtime
// cannot suppress supervisor-owned duration limits.
func (s *Service) sweepDurations(ctx context.Context) []error {
	if s.cfg.Budgets.PerRun.MaxDuration <= 0 && s.cfg.Budgets.PerTask.MaxDuration <= 0 {
		return nil
	}
	ids, err := s.store.ActiveTaskIDs(ctx)
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, id := range ids {
		a, err := s.store.LoadTask(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, r := range a.Runs() {
			if !r.State.Terminal() {
				s.checkBudgets(ctx, id, r.ID)
			}
		}
	}
	return errs
}

// A time.Time returned by the real injected clock carries its monotonic reading.
// UTC persistence remains the restart anchor; this floor protects elapsed time
// against wall-clock adjustment within this process.
type durationAnchor struct {
	task   domain.ID
	at     time.Time
	millis int64
}

func (s *Service) accountDuration(a *domain.TaskAggregate) error {
	now := s.clock.Now()
	var err error
	if s.cfg.Budgets.PerTask.MaxDuration > 0 {
		err = a.AccountDuration(now)
	} else if r, ok := a.LiveRun(); ok {
		err = a.AccountRunDuration(now, r.ID)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.durationAnchors == nil {
		s.durationAnchors = map[domain.ID]durationAnchor{}
	}
	for _, r := range a.Runs() {
		if r.State.Terminal() {
			delete(s.durationAnchors, r.ID)
			continue
		}
		millis := r.DurationMillis
		if anchor, ok := s.durationAnchors[r.ID]; ok && !now.Before(anchor.at) {
			millis = max(millis, domain.SatAdd(anchor.millis, now.Sub(anchor.at).Milliseconds()))
		}
		a.AdvanceDuration(r.ID, millis)
		s.durationAnchors[r.ID] = durationAnchor{task: a.Task().ID, at: now, millis: millis}
	}
	return nil
}

// RunDurationBudgets enforces supervisor wall time independently of runtime
// reconciliation and adapter usage, until ctx ends. The one-second bound is
// the observation interval, not extra execution allowance on a launch.
func (s *Service) RunDurationBudgets(ctx context.Context) {
	if s.cfg.Budgets.PerRun.MaxDuration <= 0 && s.cfg.Budgets.PerTask.MaxDuration <= 0 {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.report(errors.Join(s.scheduleDurationChecks(ctx)...))
		}
	}
}

// One check per run may be in progress. A hung stop retains its ownership hold
// while checks for other runs continue, and Shutdown tracks the cleanup jobs.
func (s *Service) scheduleDurationChecks(ctx context.Context) []error {
	errs := s.pruneTerminalDurationAnchors(ctx)
	ids, err := s.store.ActiveTaskIDs(ctx)
	if err != nil {
		return append(errs, err)
	}
	for _, id := range ids {
		a, err := s.store.LoadTask(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, r := range a.Runs() {
			if r.State.Terminal() {
				continue
			}
			s.mu.Lock()
			if s.closing {
				s.mu.Unlock()
				return errs
			}
			if s.durationChecking == nil {
				s.durationChecking = map[domain.ID]bool{}
			}
			if s.durationChecking[r.ID] {
				s.mu.Unlock()
				continue
			}
			s.durationChecking[r.ID] = true
			s.wg.Add(1)
			s.mu.Unlock()
			go func(task, run domain.ID) {
				defer s.wg.Done()
				defer func() { s.mu.Lock(); delete(s.durationChecking, run); s.mu.Unlock() }()
				s.checkBudgets(ctx, task, run)
			}(id, r.ID)
		}
	}
	return errs
}

// Publishing follows the commit: removing a process anchor cannot erase durable
// elapsed time or run provenance, and never happens for an uncommitted change.
func (s *Service) pruneCommittedDurationAnchors(events []domain.Event) {
	for _, e := range events {
		if e.Kind != domain.EventRunState {
			continue
		}
		var p domain.StateChanged
		if json.Unmarshal(e.Payload, &p) != nil || !domain.RunState(p.To).Terminal() {
			continue
		}
		s.mu.Lock()
		delete(s.durationAnchors, p.ID)
		s.mu.Unlock()
	}
}

// A stale accounting closure can recreate a process anchor after a terminal
// event was published. Periodic pruning verifies durable state to reclaim that
// entry too; an unavailable record is reported and never guessed terminal.
func (s *Service) pruneTerminalDurationAnchors(ctx context.Context) []error {
	s.mu.Lock()
	anchors := make(map[domain.ID]durationAnchor, len(s.durationAnchors))
	for id, a := range s.durationAnchors {
		anchors[id] = a
	}
	s.mu.Unlock()
	var errs []error
	for id, anchor := range anchors {
		a, err := s.store.LoadTask(ctx, anchor.task)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		r, ok := a.Run(id)
		if !ok || !r.State.Terminal() {
			continue
		}
		s.mu.Lock()
		delete(s.durationAnchors, id)
		s.mu.Unlock()
	}
	return errs
}
