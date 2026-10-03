package serve

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wstein/workharbor/internal/commitlint"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/service"
)

// LintFor returns the host's commit message linter a repository's commit_lint
// names (D51): the Conventional Commits subject check, or this repository's own
// rules. author is the committer's "Name <email>", which the rules need for the
// sign-off of a bot. A repository's own linter is its code and runs only inside
// its check.
func LintFor(linter, author string) func(string) []string {
	if linter == config.CommitLintWorkharbor {
		return func(m string) []string { return commitlint.Lint(m, commitlint.Options{Author: author}) }
	}
	return commitlint.Conventional
}

// RepoCheck returns the check the configuration names for a repository, or "".
func RepoCheck(c *config.Config) func(repo string) string {
	return func(repo string) string {
		for _, r := range c.Repositories {
			if strings.EqualFold(r.Name, repo) {
				return r.Check
			}
		}
		return ""
	}
}

// PublishBase fills what preparing a topic for review needs of one repository
// that this layer owns (D51, #251): the supervisor's copy and the cache, the
// workspaces' export, the bot's signing key and the host's linter, and the
// check that runs in the task's environment. The committer, the Guard, the
// forge name and the Pusher are the publish path's own (#253). A signing key
// that is not configured is left empty, so that preparing fails closed
// (hostgit.ErrNoSigningKey): nothing is ever committed unsigned.
func PublishBase(d Deps, svc *service.Service, ws *service.Workspaces, repo, author string) (service.PublishConfig, error) {
	if d.Topics == nil {
		return service.PublishConfig{}, errors.New("publishing needs the repository copies (Deps.Topics)")
	}
	topics, cache, err := d.Topics(context.Background(), repo)
	if err != nil {
		return service.PublishConfig{}, err
	}
	linter := config.CommitLintConventional
	for _, r := range d.Config.Repositories {
		if strings.EqualFold(r.Name, repo) {
			linter = r.Linter()
		}
	}
	checker := service.NewRepoChecker(svc, service.CheckConfig{
		Repo:    topics,
		Command: RepoCheck(d.Config),
		Source:  defaultBranchSource(d),
	})
	return service.PublishConfig{
		Repo: topics, Cache: cache, Workspaces: ws,
		Prepare: hostgit.PrepareSpec{SigningKey: d.Config.BotSigningKeyFile, Lint: LintFor(linter, author)},
		Checks:  checker.Check,
	}, nil
}

// defaultBranchSource reads a repository's default branch from the forge and
// the supervisor's own mirror of it (D38): never from a workspace.
func defaultBranchSource(d Deps) func(ctx context.Context, repo string) (service.CheckSource, error) {
	return func(ctx context.Context, repo string) (service.CheckSource, error) {
		db, ok := d.Forge.(forge.DefaultBrancher)
		if !ok {
			return service.CheckSource{}, errors.New("the forge cannot name a default branch")
		}
		branch, err := db.DefaultBranchName(ctx, repo)
		if err != nil {
			return service.CheckSource{}, err
		}
		_, cache, err := d.Topics(ctx, repo)
		if err != nil {
			return service.CheckSource{}, err
		}
		if err := cache.Refresh(ctx, branch); err != nil {
			return service.CheckSource{}, fmt.Errorf("fetch %s: %w", repo, err)
		}
		mirror, err := d.Git.OpenBare(ctx, cache.Path())
		if err != nil {
			return service.CheckSource{}, err
		}
		return service.CheckSource{Git: mirror, Ref: "refs/heads/" + branch}, nil
	}
}
