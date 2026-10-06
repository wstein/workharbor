package serve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/forge/forgetest"
	"github.com/wstein/workharbor/internal/gittest"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/service"
)

func TestPublishBaseWiresTheKeyTheLinterAndTheCheckFromTheConfiguration(t *testing.T) {
	t.Parallel()
	d, _ := newDeps(t)
	d.Config = &config.Config{
		BotSigningKeyFile: "/keys/bot",
		Repositories: []config.Repository{
			{Name: "o/strict", CommitLint: config.CommitLintWorkharbor, Check: "make check"},
			{Name: "o/plain"},
		},
	}
	d.Topics = func(context.Context, string) (*hostgit.Repo, *hostgit.Cache, error) { return nil, nil, nil }
	long := "feat(x): " + strings.Repeat("long ", 30) // over 72 characters, no issue trailer

	strict, err := PublishBase(d, nil, nil, "O/Strict", "bot <bot@example.test>")
	if err != nil {
		t.Fatal(err)
	}
	if strict.Prepare.SigningKey != "/keys/bot" || strict.Checks == nil {
		t.Errorf("key %q, checks nil: %v", strict.Prepare.SigningKey, strict.Checks == nil)
	}
	if len(strict.Prepare.Lint(long)) == 0 {
		t.Error("the workharbor linter accepted a long subject without its issue trailer")
	}
	plain, err := PublishBase(d, nil, nil, "o/plain", "bot <bot@example.test>")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Prepare.Lint(long)) != 0 || len(plain.Prepare.Lint("not conventional")) == 0 {
		t.Error("the default linter is the conventional one")
	}
	if got := RepoCheck(d.Config)("o/STRICT"); got != "make check" {
		t.Errorf("check = %q", got)
	}

	// no key configured: left empty, so the prepare fails closed
	d.Config = &config.Config{Repositories: d.Config.Repositories}
	none, err := PublishBase(d, nil, nil, "o/plain", "")
	if err != nil || none.Prepare.SigningKey != "" {
		t.Errorf("no key: %q, %v", none.Prepare.SigningKey, err)
	}
	if none.Checks == nil {
		t.Error("no checker")
	}
}

func TestTheHostLintersRefuseAnUnsquashedCommit(t *testing.T) {
	t.Parallel()
	for _, linter := range []string{config.CommitLintWorkharbor, config.CommitLintConventional} {
		lint := LintFor(linter, "Bot <bot@example.test>")
		if got := lint("fixup! feat: a"); len(got) == 0 {
			t.Errorf("%s accepts a fixup! commit", linter)
		}
		if got := lint("feat: a"); linter == config.CommitLintConventional && len(got) != 0 {
			t.Errorf("%s: %v", linter, got)
		}
	}
}

type stubPusher struct{}

func (stubPusher) Push(context.Context, string, string, string) error { return nil }

func TestPublishForWiresTheCommitterTheGuardAndTheWorkflow(t *testing.T) {
	t.Parallel()
	d, _ := newDeps(t)
	d.Config = &config.Config{
		BotSigningKeyFile: "/keys/bot",
		Repositories:      []config.Repository{{Name: "o/r", Check: "make check", Workflow: "integration", IntegrationBranch: "develop"}},
	}
	if PublishFor(d, nil, nil) != nil {
		t.Error("publishing is on without a pusher, a committer, copies or a forge")
	}
	d.Topics = func(context.Context, string) (*hostgit.Repo, *hostgit.Cache, error) { return nil, nil, nil }
	d.Forge = NewForgeAccess(forgetest.NewFake())
	pushers := 0
	d.NewPusher = func(*hostgit.Repo) forge.Pusher { pushers++; return stubPusher{} }
	reads := 0
	d.Committer = func(context.Context) (hostgit.Identity, error) {
		reads++
		return hostgit.Identity{Name: "app[bot]", Email: "1+app[bot]@users.noreply.github.com"}, nil
	}
	svc := service.New(d.Store, d.Runtime, d.Agent, nil, service.Config{})
	t.Cleanup(svc.Shutdown)
	publish := PublishFor(d, svc, nil)
	if publish == nil {
		t.Fatal("publishing is off with everything wired")
	}
	if _, err := publish(context.Background(), "o/other"); err == nil {
		t.Error("a repository that is not configured was accepted")
	}
	cfg, err := publish(context.Background(), "O/R")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prepare.Committer.Name != "app[bot]" || cfg.Prepare.Committer.Email != "1+app[bot]@users.noreply.github.com" || cfg.Prepare.SigningKey != "/keys/bot" {
		t.Errorf("prepare = %+v", cfg.Prepare)
	}
	if cfg.Branch != "develop" || cfg.Workflow != "integration" {
		t.Errorf("workflow %q, branch %q", cfg.Workflow, cfg.Branch)
	}
	if cfg.Guard == nil || cfg.ForgeRepo != "o/r" || cfg.Checks == nil || pushers != 1 {
		t.Errorf("guard nil=%v, repo %q, checks nil=%v, %d pushers", cfg.Guard == nil, cfg.ForgeRepo, cfg.Checks == nil, pushers)
	}
	if _, err := publish(context.Background(), "o/r"); err != nil || reads != 1 {
		t.Errorf("the identity was read %d times (%v): it is read once and kept", reads, err)
	}
	// a failed read of the App is an error, and is tried again
	d.Committer = func(context.Context) (hostgit.Identity, error) { return hostgit.Identity{}, errors.New("no network") }
	if _, err := PublishFor(d, svc, nil)(context.Background(), "o/r"); err == nil || !strings.Contains(err.Error(), "no network") {
		t.Errorf("a failed identity read = %v", err)
	}
}

// The host reads a stored message: a scissors line is text there, so what
// follows it stays visible, and git still reads the trailers before the full
// cut line.
func TestLintForReadsAStoredMessageWithScissorsLines(t *testing.T) {
	t.Parallel()
	const bot = "whr-bot <bot@example.test>"
	const cut = "# ------------------------ >8 ------------------------"
	lint := LintFor(config.CommitLintWorkharbor, bot)
	for name, msg := range map[string]string{
		"signoff after the line":    "docs: a\n\nRefs: #1\n" + cut + "\n\nSigned-off-by: P <p@example.test>",
		"signoff before the line":   "docs: a\n\nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
		"person before the line":    "docs: a\n\nCo-Authored-By: P <p@example.test>\n" + cut + "\n\nprose",
		"merge, signoff before":     "Merge branch 'x'\n\nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
		"signoff before a partial":  "docs: a\n\nSigned-off-by: P <p@example.test>\n# ------------------------ >8\n\nprose\n\nSigned-off-by: Q <q@example.test>",
		"loose cut then exact":      "docs: a\n\n" + cut + " \nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
		"loose tab then exact":      "docs: a\n\n" + cut + "\t\nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
		"loose CRLF then exact":     "docs: a\n\n" + cut + "\r\nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
		"loose then exact, merge":   "Merge branch 'x'\n\n" + cut + " \nSigned-off-by: P <p@example.test>\n" + cut + "\n\nprose",
		"loose then exact, person":  "docs: a\n\n" + cut + " \nCo-Authored-By: P <p@example.test>\n" + cut + "\n\nprose",
		"signoff without a space":   "docs: a\n\nSigned-off-by:P <p@example.test>",
		"spaced signoff key":        "docs: a\n\nSigned-off-by : P <p@example.test>",
		"signoff next to two prose": "docs: a\n\nSigned-off-by: P <p@example.test>\nprose\nprose",
	} {
		if got := lint(msg); len(got) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := lint("docs: a\n\nRefs: #1\n" + gittest.AICoauthor + "\n" + cut + "\n\ndiff --git"); len(got) != 0 {
		t.Errorf("a scissors line with no trailer after it: %v", got)
	}
	if got := LintFor(config.CommitLintConventional, bot)("docs: a\n\n" + cut + "\n\nfixup! x"); len(got) != 0 {
		t.Errorf("conventional: %v", got)
	}
}
