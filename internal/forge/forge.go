// Package forge defines the forge adapter contract: issues, pull requests,
// reviews, metadata and webhooks. Git transport is separate (hostgit); the
// adapter is wrapped in a Guard that enforces the autonomy policy, because
// policy belongs in the adapter and never in a prompt.
// See docs/content/docs/design/interfaces.md §10.
package forge

import (
	"context"
	"net/http"
)

// Issue is a forge issue. Everything in it is untrusted data.
type Issue struct {
	Repo   string
	Number int
	Title  string
	Body   string
	Author string
	// AuthorAssociation is the forge's relationship of the author to the
	// repository (for GitHub: OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, NONE),
	// which the trust tiers read (design §10, issue #53).
	AuthorAssociation string
}

// PullRequest is a forge pull request.
type PullRequest struct {
	Repo   string
	Number int
	URL    string
	Branch string
	SHA    string // the commit the PR was opened or last updated at
}

// Approval is the proof that a human approved one commit: the review Decision
// and the SHA it was answered for. A PR or a push takes it, not just a branch.
type Approval struct {
	DecisionID string
	SHA        string
}

// Adapter talks to one forge instance. It has no merge, tag, release or deploy
// method: an agent can attempt them only through a Guard, which refuses.
type Adapter interface {
	Name() string
	GetIssue(ctx context.Context, repo string, number int) (Issue, error)
	CommentIssue(ctx context.Context, repo string, number int, body string) error
	// BranchSHA returns the commit a branch points at on the forge.
	BranchSHA(ctx context.Context, repo, branch string) (string, error)
	// OpenPR opens a pull request for branch, which must point at sha.
	OpenPR(ctx context.Context, repo, branch, sha, title, body string) (PullRequest, error)
	// UpdatePR updates a pull request whose branch now points at sha.
	UpdatePR(ctx context.Context, pr PullRequest, sha, title, body string) error
	// VerifyWebhook checks the signature of a webhook request.
	VerifyWebhook(h http.Header, body []byte) error
}

// Pusher sends one agent branch to the forge at an exact commit: fast-forward
// only, never forced, never tags (hostgit does it with the run's scoped
// credentials).
type Pusher interface {
	Push(ctx context.Context, repo, branch, sha string) error
}

// Verifier says whether a human approved a commit: the Decision exists, was
// answered allow, and covers exactly this SHA (Decision.Allows).
type Verifier interface {
	Approved(ctx context.Context, a Approval) bool
}
