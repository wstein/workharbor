// Package forge defines the forge adapter contract: issues, pull requests,
// reviews, metadata and webhooks. It enforces the autonomy policy.
// See docs/design.md §10.
package forge

import "context"

// Issue is a forge issue.
type Issue struct {
	Repo   string
	Number int
	Title  string
	Body   string
	Author string
}

// PullRequest is a forge pull request.
type PullRequest struct {
	Repo   string
	Number int
	URL    string
	Branch string
	SHA    string
}

// Adapter talks to one forge instance.
type Adapter interface {
	Name() string
	GetIssue(ctx context.Context, repo string, number int) (Issue, error)
	CommentIssue(ctx context.Context, repo string, number int, body string) error
	OpenPR(ctx context.Context, repo, branch, title, body string) (PullRequest, error)
	UpdatePR(ctx context.Context, pr PullRequest, title, body string) error
	VerifyWebhook(headers map[string]string, body []byte) error
}
