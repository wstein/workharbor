// Package ci defines the CI adapter contract. It is an interface only in
// release 1. See docs/design.md §10.
package ci

import "context"

// Status is the CI result for one commit SHA.
type Status struct {
	SHA   string
	State string // "pending" | "success" | "failure"
	URL   string
}

// Adapter reads and controls pipelines.
type Adapter interface {
	Name() string
	StatusFor(ctx context.Context, repo, sha string) (Status, error)
	Retry(ctx context.Context, repo, sha string) error
	Cancel(ctx context.Context, repo, sha string) error
}
