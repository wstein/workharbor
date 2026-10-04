package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
)

// DefaultGitBaseURL is where GitHub serves repositories over https.
const DefaultGitBaseURL = "https://github.com"

// GitPusher sends a branch over https with a token that the caller holds only for
// that one push (hostgit.Repo.PushToken).
type GitPusher interface {
	PushToken(ctx context.Context, remote, branch, sha, token string) error
}

// Pusher is the forge.Pusher of the GitHub adapter (D51, design §7.3). For each
// push it mints an installation token restricted to the one repository and to
// contents: write, registers it with the redactor, hands it to git as a value
// and revokes it when the push returns, whichever way it returns. The remote is
// built here from the configured "owner/name", never read from a repository.
type Pusher struct {
	c   *Client
	git GitPusher
}

var _ forge.Pusher = (*Pusher)(nil)

// NewPusher returns the Pusher over git. Only forge.Guard.Push calls it, after
// its approval check.
func (c *Client) NewPusher(git GitPusher) *Pusher { return &Pusher{c: c, git: git} }

// pushPermissions is all a push token may do.
var pushPermissions = map[string]string{"contents": "write"}

// Push implements forge.Pusher.
func (p *Pusher) Push(ctx context.Context, repo, branch, sha string) (err error) {
	if err := p.c.allowed(repo); err != nil {
		return err
	}
	tok, err := p.c.mintPushToken(ctx, repo)
	if err != nil {
		return err
	}
	defer func() {
		// the push's context may be the reason it returned: revoke on a fresh one
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if rerr := p.c.revokeToken(rctx, tok); rerr != nil {
			err = errors.Join(err, fmt.Errorf("revoke the push token for %s: %w", repo, rerr))
		}
	}()
	remote := strings.TrimRight(p.c.gitBase(), "/") + "/" + repo + ".git"
	if perr := p.git.PushToken(ctx, remote, branch, sha, tok); perr != nil {
		msg := perr.Error()
		if p.c.cfg.Redactor != nil {
			msg = p.c.cfg.Redactor.String(msg)
		}
		msg = strings.ReplaceAll(msg, tok, "[redacted]")
		return fmt.Errorf("push %s to %s: %w", branch, repo, scrubbed{msg: msg, err: perr})
	}
	return nil
}

// scrubbed carries the redacted text of a push error and still matches the
// original's sentinels (hostgit.ErrNotFastForward) with errors.Is.
type scrubbed struct {
	msg string
	err error
}

func (s scrubbed) Error() string { return s.msg }

func (s scrubbed) Is(target error) bool { return errors.Is(s.err, target) }

func (c *Client) gitBase() string {
	if c.cfg.GitBaseURL != "" {
		return c.cfg.GitBaseURL
	}
	return DefaultGitBaseURL
}

// mintPushToken mints a fresh, uncached installation token for repo with
// contents: write only, and registers it with the redactor.
func (c *Client) mintPushToken(ctx context.Context, repo string) (string, error) {
	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	id := c.install[strings.ToLower(repo)]
	c.mu.Unlock()
	if id == 0 {
		if id, err = c.findInstallation(ctx, jwt, repo); err != nil {
			return "", err
		}
		c.mu.Lock()
		c.install[strings.ToLower(repo)] = id
		c.mu.Unlock()
	}
	_, name, _ := strings.Cut(repo, "/")
	var tok struct {
		Token string `json:"token"`
	}
	err = c.do(ctx, jwt, http.MethodPost, "/app/installations/"+strconv.FormatInt(id, 10)+"/access_tokens",
		map[string]any{"repositories": []string{name}, "permissions": pushPermissions}, &tok)
	if err != nil {
		return "", fmt.Errorf("mint a push token for %s: %w", repo, err)
	}
	if tok.Token == "" {
		return "", errors.New("github: the push token answer is incomplete")
	}
	if c.cfg.Redactor != nil {
		c.cfg.Redactor.Add(tok.Token)
	}
	return tok.Token, nil
}

// revokeToken revokes a token with DELETE /installation/token, which it
// authenticates itself. A 401 means it is already gone: that is unverified until
// #28 (GitHub's answer for a revoked token was not measured).
func (c *Client) revokeToken(ctx context.Context, tok string) error {
	err := c.do(ctx, tok, http.MethodDelete, "/installation/token", nil, nil)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		return nil
	}
	return err
}

var botSlugRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// BotIdentity is the committer of the commits the supervisor rewrites (D15,
// D51): the App's bot, "<slug>[bot]" with GitHub's noreply address
// "<user id>+<slug>[bot]@users.noreply.github.com", both read from the App and
// never typed into the configuration.
func (c *Client) BotIdentity(ctx context.Context) (hostgit.Identity, error) {
	jwt, err := c.appJWT()
	if err != nil {
		return hostgit.Identity{}, err
	}
	var app struct {
		Slug string `json:"slug"`
	}
	if err := c.do(ctx, jwt, http.MethodGet, "/app", nil, &app); err != nil {
		return hostgit.Identity{}, fmt.Errorf("read the App: %w", err)
	}
	if !botSlugRE.MatchString(app.Slug) {
		return hostgit.Identity{}, errors.New("github: the App has no usable slug")
	}
	name := app.Slug + "[bot]"
	var user struct {
		ID int64 `json:"id"`
	}
	// a public read, made with an installation token like every other call;
	// that GitHub serves /users/<slug>[bot] this way is unverified until #28
	if err := c.call(ctx, c.cfg.Repos[0], http.MethodGet, "/users/"+name, nil, &user); err != nil {
		return hostgit.Identity{}, fmt.Errorf("read the bot user %s: %w", name, err)
	}
	if user.ID <= 0 {
		return hostgit.Identity{}, errors.New("github: the bot user has no ID")
	}
	return hostgit.Identity{Name: name, Email: strconv.FormatInt(user.ID, 10) + "+" + name + "@users.noreply.github.com"}, nil
}
