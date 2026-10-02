package service

import (
	"context"
	"time"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/domain"
)

// approverFor returns the Approver of one run: a permission prompt of the agent
// becomes a blocking approval Decision of the task, and the human's answer is
// what the Approver returns (design §4.2, D26). It fails closed: whatever ends
// the wait, the agent is told no.
func (s *Service) approverFor(task, run domain.ID) agent.Approver {
	return agent.ApproverFunc(func(ctx context.Context, req agent.ApprovalRequest) (agent.Approval, error) {
		return s.askHuman(ctx, task, run, req)
	})
}

// fillApprover gives a manual-mode spec the service's approver, unless the
// caller set one (a test does).
func (s *Service) fillApprover(spec *agent.StartSpec, task, run domain.ID) {
	if spec.Mode() == agent.PermissionManual && spec.Approver == nil {
		spec.Approver = s.approverFor(task, run)
	}
}

func (s *Service) askHuman(ctx context.Context, task, run domain.ID, req agent.ApprovalRequest) (agent.Approval, error) {
	id := s.cfg.NewID()
	ch := make(chan agent.Approval, 1)
	s.mu.Lock()
	s.approvals[id] = ch // before the Decision exists, so an answer cannot beat the wait
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.approvals, id)
		s.mu.Unlock()
	}()

	timeout := domain.DefaultApprovalTimeout
	if dl, ok := ctx.Deadline(); ok {
		timeout = max(time.Until(dl), time.Millisecond)
	}
	err := s.update(ctx, task, func(a *domain.TaskAggregate) error {
		_, err := a.RaiseDecision(domain.NewDecision{
			ID: id, RunID: run, Kind: domain.DecisionApproval, Blocking: true,
			Subject: req.Tool, Input: req.Input, InputTruncated: req.Truncated, Now: s.clock.Now(), Timeout: timeout,
		})
		return err
	})
	if err != nil {
		return agent.Approval{}, err
	}
	select {
	case a := <-ch:
		return a, nil
	case <-ctx.Done():
		// No answer in time, or the session was stopped: the Decision must not
		// stay open for an agent that no longer waits for it.
		s.closeApproval(context.WithoutCancel(ctx), task, id)
		return agent.Approval{}, ctx.Err()
	}
}

// closeApproval expires an approval nobody answered. A Decision that a pause or
// a cancel already superseded is left as it is.
func (s *Service) closeApproval(ctx context.Context, task, id domain.ID) {
	s.report(s.update(ctx, task, func(a *domain.TaskAggregate) error {
		d, ok := a.Decision(id)
		if !ok || d.Status != domain.DecisionOpen {
			return nil
		}
		a.ExpireDecisions(laterOf(s.clock.Now(), d.Deadline))
		return nil
	}))
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// approvalWaiting reports whether an agent is waiting for the answer to this
// approval, so an answer to one nobody asks for is refused, not stored.
func (s *Service) approvalWaiting(id domain.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.approvals[id]
	return ok
}

// deliverApproval hands an answered approval to the Approver waiting for it.
func (s *Service) deliverApproval(d *domain.Decision) {
	if d == nil || d.Kind != domain.DecisionApproval || d.Status != domain.DecisionAnswered {
		return
	}
	s.mu.Lock()
	ch := s.approvals[d.ID]
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- agent.Approval{Allow: d.Allows(""), Reason: d.Reason}:
	default: // already answered: the first answer stands
	}
}
