// Package forgetest has an in-memory forge for tests.
package forgetest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/wstein/workharbor/internal/forge"
)

// Fake is an in-memory forge and pusher. It records every call, so a test can
// show that a refused action never reached it.
type Fake struct {
	mu       sync.Mutex
	Issues   map[string]forge.Issue // "repo#number"
	Branches map[string]string      // "repo:branch" to commit
	PRs      []forge.PullRequest
	Comments []string
	// DefaultBranch is what DefaultBranchName answers; empty means "main".
	DefaultBranch string
	Calls         []string
	// Cards are the board updates it was asked for, in order, and CardErr is
	queue []forge.QueuedCard // what QueuedCards returns
	// what UpdateCard returns (a board write that fails).
	Cards   []Card
	CardErr error
	// FastForwards are the branch moves it was asked for, and NotFF makes the next
	// one refuse as the forge does for a branch that moved.
	FastForwards []string
	NotFF        bool
	// WebhookSecret is the header value VerifyWebhook accepts in X-Signature.
	WebhookSecret string
}

// NewFake returns an empty fake.
func NewFake() *Fake {
	return &Fake{Issues: map[string]forge.Issue{}, Branches: map[string]string{}}
}

func (f *Fake) call(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, fmt.Sprintf(format, args...))
}

// Name implements forge.Adapter.
func (f *Fake) Name() string { return "fake" }

// GetIssue implements forge.Adapter.
func (f *Fake) GetIssue(_ context.Context, repo string, n int) (forge.Issue, error) {
	f.call("GetIssue %s#%d", repo, n)
	if i, ok := f.Issues[fmt.Sprintf("%s#%d", repo, n)]; ok {
		return i, nil
	}
	return forge.Issue{}, errors.New("no such issue")
}

// CommentIssue implements forge.Adapter.
func (f *Fake) CommentIssue(_ context.Context, repo string, n int, body string) error {
	f.call("CommentIssue %s#%d", repo, n)
	f.Comments = append(f.Comments, body)
	return nil
}

// BranchSHA implements forge.Adapter.
func (f *Fake) BranchSHA(_ context.Context, repo, branch string) (string, error) {
	f.call("BranchSHA %s:%s", repo, branch)
	if sha, ok := f.Branches[repo+":"+branch]; ok {
		return sha, nil
	}
	return "", errors.New("no such branch")
}

// OpenPR implements forge.Adapter.
func (f *Fake) OpenPR(_ context.Context, repo, branch, sha, title, _ string) (forge.PullRequest, error) {
	f.call("OpenPR %s:%s@%s", repo, branch, sha)
	pr := forge.PullRequest{Repo: repo, Number: len(f.PRs) + 1, URL: fmt.Sprintf("https://forge.test/%s/pull/%d", repo, len(f.PRs)+1), Branch: branch, SHA: sha}
	f.PRs = append(f.PRs, pr)
	_ = title
	return pr, nil
}

// UpdatePR implements forge.Adapter.
func (f *Fake) UpdatePR(_ context.Context, pr forge.PullRequest, sha, _, _ string) error {
	f.call("UpdatePR %s#%d@%s", pr.Repo, pr.Number, sha)
	for i := range f.PRs {
		if f.PRs[i].Number == pr.Number && f.PRs[i].Repo == pr.Repo {
			f.PRs[i].SHA = sha
			return nil
		}
	}
	return errors.New("no such pull request")
}

// VerifyWebhook implements forge.Adapter.
func (f *Fake) VerifyWebhook(h http.Header, _ []byte) error {
	f.call("VerifyWebhook")
	if f.WebhookSecret == "" || h.Get("X-Signature") != f.WebhookSecret {
		return errors.New("bad signature")
	}
	return nil
}

// Push implements forge.Pusher.
func (f *Fake) Push(_ context.Context, repo, branch, sha string) error {
	f.call("Push %s:%s@%s", repo, branch, sha)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Branches[repo+":"+branch] = sha
	return nil
}

// Card is one board update a Fake received.
type Card struct {
	Repo   string
	Issue  int
	Update forge.CardUpdate
}

// UpdateCard implements forge.Board.
func (f *Fake) UpdateCard(_ context.Context, repo string, issue int, u forge.CardUpdate) error {
	f.call("UpdateCard %s#%d %s", repo, issue, u.Status)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cards = append(f.Cards, Card{Repo: repo, Issue: issue, Update: u})
	return f.CardErr
}

// CardsSeen returns a copy of the board updates received so far.
func (f *Fake) CardsSeen() []Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Card(nil), f.Cards...)
}

// DefaultBranchName implements forge.DefaultBrancher: "main" unless DefaultBranch is set.
func (f *Fake) DefaultBranchName(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DefaultBranch != "" {
		return f.DefaultBranch, nil
	}
	return "main", nil
}

// FastForward implements forge.FastForwarder.
func (f *Fake) FastForward(_ context.Context, repo, branch, sha string) error {
	f.call("FastForward %s:%s@%s", repo, branch, sha)
	if f.NotFF {
		return forge.ErrNotFastForward
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FastForwards = append(f.FastForwards, repo+":"+branch+"@"+sha)
	f.Branches[repo+":"+branch] = sha
	return nil
}

// OpenPRInto implements forge.BasedPRs.
func (f *Fake) OpenPRInto(ctx context.Context, repo, base, branch, sha, title, body string) (forge.PullRequest, error) {
	f.call("OpenPRInto %s:%s<-%s@%s", repo, base, branch, sha)
	return f.OpenPR(ctx, repo, branch, sha, title, body)
}

// SetQueue sets the cards QueuedCards returns.
func (f *Fake) SetQueue(cards ...forge.QueuedCard) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = cards
}

// QueuedCards implements forge.QueueReader.
func (f *Fake) QueuedCards(_ context.Context, status string) ([]forge.QueuedCard, error) {
	f.call("QueuedCards %s", status)
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]forge.QueuedCard(nil), f.queue...), nil
}
