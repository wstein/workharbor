package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wstein/workharbor/internal/policy"
)

// Errors a Guard returns.
var (
	// ErrForbidden is the autonomy policy refusing an action.
	ErrForbidden = errors.New("forbidden by the autonomy policy")
	// ErrNotApproved means no human approved this commit.
	ErrNotApproved = errors.New("the commit is not approved")
	// ErrBranch means the branch is not an agent branch.
	ErrBranch = errors.New("only agent/* branches are pushed")
	// ErrSHAMismatch means the forge's branch does not point at the approved commit.
	ErrSHAMismatch = errors.New("the branch on the forge is not at the approved commit")
)

// Guard wraps an Adapter and a Pusher with the autonomy table. It is the
// enforcement point (design §6): default deny, an approval per commit SHA, and
// merge, tag, release and deploy refused whatever the table says. A refusal
// never reaches the inner adapter.
type Guard struct {
	inner    Adapter
	pusher   Pusher
	table    policy.Table
	verifier Verifier
}

// NewGuard returns a Guard.
func NewGuard(inner Adapter, pusher Pusher, table policy.Table, v Verifier) *Guard {
	return &Guard{inner: inner, pusher: pusher, table: table, verifier: v}
}

// Name returns the wrapped adapter's name.
func (g *Guard) Name() string { return g.inner.Name() }

// GetIssue reads an issue.
func (g *Guard) GetIssue(ctx context.Context, repo string, number int) (Issue, error) {
	return g.inner.GetIssue(ctx, repo, number)
}

// VerifyWebhook checks a webhook signature.
func (g *Guard) VerifyWebhook(h http.Header, body []byte) error {
	return g.inner.VerifyWebhook(h, body)
}

// CommentIssue comments on an issue when the table lets it run unasked.
func (g *Guard) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	if err := g.allow(policy.CommentIssue); err != nil {
		return err
	}
	return g.inner.CommentIssue(ctx, repo, number, body)
}

// allow refuses unless the table says the action runs on its own.
func (g *Guard) allow(a policy.Action) error {
	if m := g.table.Decide(a); m != policy.Auto {
		return fmt.Errorf("%w: %s is %s", ErrForbidden, a, m)
	}
	return nil
}

// approve checks the autonomy mode of an action that needs a human's approval
// of a commit: forbid refuses, and auto or ask both need the approval, because
// the review Decision is what approves the commit.
func (g *Guard) approve(ctx context.Context, a policy.Action, ap Approval) error {
	if g.table.Decide(a) == policy.Forbid {
		return fmt.Errorf("%w: %s", ErrForbidden, a)
	}
	if ap.SHA == "" || ap.DecisionID == "" || !g.verifier.Approved(ctx, ap) {
		return fmt.Errorf("%w: %s needs an approved commit", ErrNotApproved, a)
	}
	return nil
}

// Push sends an agent branch at the approved commit.
func (g *Guard) Push(ctx context.Context, repo, branch string, ap Approval) error {
	if !strings.HasPrefix(branch, "agent/") || strings.Contains(branch, "..") {
		return fmt.Errorf("%w: %q", ErrBranch, branch)
	}
	if err := g.approve(ctx, policy.PushAgentBranch, ap); err != nil {
		return err
	}
	return g.pusher.Push(ctx, repo, branch, ap.SHA)
}

// onForge checks that the branch on the forge points at the approved commit.
func (g *Guard) onForge(ctx context.Context, repo, branch string, ap Approval) error {
	got, err := g.inner.BranchSHA(ctx, repo, branch)
	if err != nil {
		return err
	}
	if got != ap.SHA {
		return fmt.Errorf("%w: %s is at %s, approved %s", ErrSHAMismatch, branch, got, ap.SHA)
	}
	return nil
}

// OpenPR opens a pull request for the approved commit.
func (g *Guard) OpenPR(ctx context.Context, repo, branch string, ap Approval, title, body string) (PullRequest, error) {
	if err := g.approve(ctx, policy.OpenPR, ap); err != nil {
		return PullRequest{}, err
	}
	if err := g.onForge(ctx, repo, branch, ap); err != nil {
		return PullRequest{}, err
	}
	return g.inner.OpenPR(ctx, repo, branch, ap.SHA, title, body)
}

// UpdatePR updates a pull request to the approved commit.
func (g *Guard) UpdatePR(ctx context.Context, pr PullRequest, ap Approval, title, body string) error {
	if err := g.approve(ctx, policy.OpenPR, ap); err != nil {
		return err
	}
	if err := g.onForge(ctx, pr.Repo, pr.Branch, ap); err != nil {
		return err
	}
	return g.inner.UpdatePR(ctx, pr, ap.SHA, title, body)
}

// Merge is always refused: merging stays with a human on the forge.
func (g *Guard) Merge(context.Context, string, int) error { return g.refuse(policy.Merge) }

// Tag is always refused.
func (g *Guard) Tag(context.Context, string, string) error { return g.refuse(policy.Tag) }

// Release is always refused.
func (g *Guard) Release(context.Context, string, string) error { return g.refuse(policy.Release) }

// Deploy is always refused.
func (g *Guard) Deploy(context.Context, string, string) error { return g.refuse(policy.Deploy) }

// refuse is the default-deny for actions an agent never takes. The table's
// ceilings keep them forbidden even if a configuration says otherwise, and the
// answer here does not depend on it.
func (g *Guard) refuse(a policy.Action) error {
	return fmt.Errorf("%w: %s stays with a human", ErrForbidden, a)
}
