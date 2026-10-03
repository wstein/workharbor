package serve

import (
	"context"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/hostgit"
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
