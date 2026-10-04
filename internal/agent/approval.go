package agent

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/domain"
)

// DefaultApprovalTimeout is how long an Approver has when the spec says
// nothing; it matches the Decision default (design §4.2).
const DefaultApprovalTimeout = domain.DefaultApprovalTimeout

// ApprovalRequest is a permission prompt of the agent put to the host.
type ApprovalRequest struct {
	ID    string `json:"id"`
	Tool  string `json:"tool"`
	Input string `json:"input"` // capped by Ask; all of it is untrusted
	// Truncated says Ask cut Input to domain.MaxDecisionInput characters: what
	// the human sees is not all the agent asked to run, which the Decision must
	// say.
	Truncated bool `json:"truncated,omitempty"`
	Plan      bool `json:"plan,omitempty"` // an ExitPlanMode request, whose subject is the plan
}

// Approval is the human's answer.
type Approval struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"` // optional; passed back to the agent
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
	if isNil(ap) {
		return Approval{Reason: "no approver: denied"}
	}
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	req.Input, req.Truncated = capInput(req.Input)

	parent := ctx
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	type answer struct {
		a   Approval
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		// A panic in the approver must deny, not take the supervisor down. Only
		// the panic's type is logged: its value may carry untrusted input or a
		// secret, and the reason the agent sees is fixed text.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("approver panicked", "type", reflect.TypeOf(r).String())
				ch <- answer{err: errApproverPanic}
			}
		}()
		a, err := ap.Approve(ctx, req)
		ch <- answer{a, err}
	}()
	select {
	case r := <-ch:
		// select picks at random when the answer and the done context are both
		// ready, and a cancel reaches the derived context a moment after the
		// caller's: an answer is honoured only while both are still live.
		if parent.Err() != nil {
			return ctxDenial(parent)
		}
		if ctx.Err() != nil {
			return ctxDenial(ctx)
		}
		if r.err != nil {
			return Approval{Reason: "approval failed: denied"}
		}
		return r.a
	case <-ctx.Done():
		return ctxDenial(ctx)
	}
}

var errApproverPanic = errors.New("approver panicked")

// isNil reports an Approver that is nil or holds a nil func, pointer, map,
// slice or interface (a typed nil passes an == nil check).
func isNil(ap Approver) bool {
	if ap == nil {
		return true
	}
	switch v := reflect.ValueOf(ap); v.Kind() {
	case reflect.Func, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// ctxDenial is the denial for a done context, saying whether time ran out or
// the session was stopped.
func ctxDenial(ctx context.Context) Approval {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Approval{Reason: "no answer in time: denied"}
	}
	return Approval{Reason: "cancelled: denied"} // the session was stopped
}

// capInput keeps at most domain.MaxDecisionInput characters and says whether
// it cut any.
func capInput(s string) (string, bool) {
	if utf8.RuneCountInString(s) <= domain.MaxDecisionInput {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:domain.MaxDecisionInput]), true
}
