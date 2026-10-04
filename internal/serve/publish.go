package serve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/wstein/workharbor/internal/commitlint"
	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/policy"
	"github.com/wstein/workharbor/internal/service"
)

// LintFor returns the host's commit message linter a repository's commit_lint
// names (D51): the Conventional Commits subject check, or this repository's own
// rules. author is the committer's "Name <email>", which the rules need for the
// sign-off of a bot. Both refuse a fixup!, squash! or amend! commit: a prepared
// commit is about to be pushed. A repository's own linter is its code and runs only inside
// its check.
func LintFor(linter, author string) func(string) []string {
	if linter == config.CommitLintWorkharbor {
		return func(m string) []string { return commitlint.Lint(m, commitlint.Options{Author: author, Final: true}) }
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

// PublishFor returns what the publish pipeline asks for a repository (D51, #253):
// PublishBase, plus the committer, the Guard with the forge's Pusher and the
// repository's workflow. The committer is the GitHub App's bot identity, read
// from the App the first time it is needed and kept, never typed into the
// configuration (D15). A repository that is not in the configuration is refused:
// nothing is prepared or pushed for one the supervisor was not set up for. It
// returns nil when the host cannot publish: no repository copies, no pusher or
// no committer.
func PublishFor(d Deps, svc *service.Service, ws *service.Workspaces) func(ctx context.Context, repo string) (service.PublishConfig, error) {
	if d.Topics == nil || d.NewPusher == nil || d.Committer == nil || d.Forge == nil || d.Config == nil {
		return nil
	}
	var mu sync.Mutex
	var bot *hostgit.Identity
	committer := func(ctx context.Context) (hostgit.Identity, error) {
		mu.Lock()
		defer mu.Unlock()
		if bot != nil {
			return *bot, nil
		}
		id, err := d.Committer(ctx)
		if err != nil {
			return hostgit.Identity{}, fmt.Errorf("read the bot identity from the App: %w", err)
		}
		bot = &id
		return id, nil
	}
	return func(ctx context.Context, repo string) (service.PublishConfig, error) {
		var r *config.Repository
		for i := range d.Config.Repositories {
			if strings.EqualFold(d.Config.Repositories[i].Name, repo) {
				r = &d.Config.Repositories[i]
			}
		}
		if r == nil {
			return service.PublishConfig{}, fmt.Errorf("%s is not a configured repository", repo)
		}
		id, err := committer(ctx)
		if err != nil {
			return service.PublishConfig{}, err
		}
		cfg, err := PublishBase(d, svc, ws, r.Name, id.Name+" <"+id.Email+">")
		if err != nil {
			return service.PublishConfig{}, err
		}
		cfg.Prepare.Committer = id
		// The table here only has to exist: the publisher chooses the table of the
		// task's workflow for every push and pull request.
		cfg.Guard = forge.NewGuard(d.Forge, d.NewPusher(cfg.Repo), policy.Default(), svc.VerifierFor())
		cfg.ForgeRepo, cfg.Workflow, cfg.Branch = r.Name, r.Preset(), workflowBranch(*r)
		return cfg, nil
	}
}
