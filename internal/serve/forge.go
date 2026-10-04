package serve

import (
	"context"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/policy"
)

// ForgeAccess is what the rest of serve may do with the forge. The raw
// forge.Adapter is held only here and in Build (real.go): every consumer gets
// one of these narrow members, and every effect goes through a forge.Guard
// built by Guard. TestRawForgeStaysAtTheRoot fails when another package names
// the adapter type (issue #247).
type ForgeAccess struct {
	// Guard returns a forge.Guard over the forge with the given pusher and
	// verifier, under the default table: the guarded publish and, with a nil
	// pusher and verifier, the guarded board.
	Guard func(p forge.Pusher, v forge.Verifier) *forge.Guard
	// DefaultBranch reads a repository's default branch from the forge; nil when
	// the adapter cannot name one.
	DefaultBranch forge.DefaultBrancher
	// RevokeTokens revokes the tokens the forge adapter holds; nil when it has no
	// way to (the GitHub App client has).
	RevokeTokens func(context.Context) (int, error)
}

// NewForgeAccess narrows a forge adapter to a ForgeAccess. It is called by Build
// and by tests with a fake, never by a consumer.
func NewForgeAccess(a forge.Adapter) ForgeAccess {
	if a == nil {
		return ForgeAccess{}
	}
	fa := ForgeAccess{Guard: func(p forge.Pusher, v forge.Verifier) *forge.Guard {
		return forge.NewGuard(a, p, policy.Default(), v)
	}}
	if db, ok := a.(forge.DefaultBrancher); ok {
		fa.DefaultBranch = defaultBranchFunc(db.DefaultBranchName)
	}
	if r, ok := a.(interface {
		RevokeTokens(context.Context) (int, error)
	}); ok {
		fa.RevokeTokens = r.RevokeTokens
	}
	return fa
}

// defaultBranchFunc is a method value that names a default branch: a
// forge.DefaultBrancher that holds no adapter, so a type assertion on it
// cannot reach FastForward, OpenPRInto or anything else of the client.
type defaultBranchFunc func(ctx context.Context, repo string) (string, error)

// DefaultBranchName calls the method value.
func (f defaultBranchFunc) DefaultBranchName(ctx context.Context, repo string) (string, error) {
	return f(ctx, repo)
}

// issueClient is what NewIssueAccess takes from the forge client.
type issueClient interface {
	GetIssue(ctx context.Context, repo string, number int) (forge.Issue, error)
	QueuedCards(ctx context.Context, status string) ([]forge.QueuedCard, error)
	DefaultBranchName(ctx context.Context, repo string) (string, error)
}

// IssueAccess is the issue source the service gets (service.Config.Issues): the
// three reads it asserts on (GetIssue, QueuedCards, DefaultBranchName) as
// method values, never the client, so a type assertion on it cannot reach
// forge.Adapter, forge.FastForwarder or forge.BasedPRs (issue #247).
type IssueAccess struct {
	getIssue          func(ctx context.Context, repo string, number int) (forge.Issue, error)
	queuedCards       func(ctx context.Context, status string) ([]forge.QueuedCard, error)
	defaultBranchName func(ctx context.Context, repo string) (string, error)
}

// NewIssueAccess narrows a forge client to an IssueAccess. Build calls it;
// tests call it with a fake.
func NewIssueAccess(c issueClient) *IssueAccess {
	return &IssueAccess{getIssue: c.GetIssue, queuedCards: c.QueuedCards, defaultBranchName: c.DefaultBranchName}
}

// GetIssue loads an issue.
func (a *IssueAccess) GetIssue(ctx context.Context, repo string, number int) (forge.Issue, error) {
	return a.getIssue(ctx, repo, number)
}

// QueuedCards reads the cards of a board column.
func (a *IssueAccess) QueuedCards(ctx context.Context, status string) ([]forge.QueuedCard, error) {
	return a.queuedCards(ctx, status)
}

// DefaultBranchName names a repository's default branch.
func (a *IssueAccess) DefaultBranchName(ctx context.Context, repo string) (string, error) {
	return a.defaultBranchName(ctx, repo)
}
