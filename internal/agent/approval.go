package agent

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/domain"
)

// DefaultApprovalTimeout is how long an Approver has when the spec says
// nothing; it matches the Decision default (design §4.2).
const DefaultApprovalTimeout = domain.DefaultApprovalTimeout

// ApprovalRequest is a permission prompt of the agent put to the host.
type ApprovalRequest struct {
	ID    string
	Tool  string
	Input string // capped by Ask; all of it is untrusted
	Plan  bool   // an ExitPlanMode request, whose subject is the plan
}

// Approval is the human's answer.
type Approval struct {
	Allow  bool
	Reason string // optional; passed back to the agent
}

// Approver answers permission prompts for the host, usually by opening an
// approval Decision and waiting for a human.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (Approval, error)
}

// ApproverFunc adapts a function to an Approver.
type ApproverFunc func(ctx context.Context, req ApprovalRequest) (Approval, error)

// Approve implements Approver.
func (f ApproverFunc) Approve(ctx context.Context, req ApprovalRequest) (Approval, error) {
	return f(ctx, req)
}

// Ask puts a permission prompt to the approver and fails closed: an error, a
// cancelled context, no answer within the timeout, or no approver at all is a
// denial, and the reason says why so the agent can see it. Adapters use it for
// every prompt.
func Ask(ctx context.Context, ap Approver, timeout time.Duration, req ApprovalRequest) Approval {
	if ap == nil {
		return Approval{Reason: "no approver: denied"}
	}
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	req.Input = capInput(req.Input)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type answer struct {
		a   Approval
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		a, err := ap.Approve(ctx, req)
		ch <- answer{a, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return Approval{Reason: "approval failed: denied"}
		}
		return r.a
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Approval{Reason: "no answer in time: denied"}
		}
		return Approval{Reason: "cancelled: denied"} // the session was stopped
	}
}

// capInput keeps at most domain.MaxDecisionInput characters.
func capInput(s string) string {
	if utf8.RuneCountInString(s) <= domain.MaxDecisionInput {
		return s
	}
	runes := []rune(s)
	return string(runes[:domain.MaxDecisionInput])
}
