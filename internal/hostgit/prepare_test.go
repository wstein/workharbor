package hostgit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/commitlint"
)

type prep struct {
	t       *testing.T
	g       *Git
	base    string
	forge   *forgeRepo
	cache   *Cache
	repo    *Repo
	topic   string // the agent's checkout
	key     string
	env     []string
	lintFn  func(string) []string
	topicBr string
}

// newPrep builds the whole chain: a forge with a main, the cache, a topic
// clone in which an agent makes two commits and a fixup, and the supervisor's
// bare copy holding the topic and the target.
func newPrep(t *testing.T) *prep {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not available")
	}
	ctx := context.Background()
	p := &prep{t: t, g: newGit(t), base: t.TempDir(), topicBr: "agent/topic"}
	p.env = plainEnv(filepath.Join(p.base, "home"))
	p.forge = newForge(t, p.base, 4)
	p.cache = openCache(t, p.g, p.base, p.forge, 0)
	if err := p.cache.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}

	p.key = filepath.Join(p.base, "bot-key")
	if out, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", p.key).CombinedOutput(); err != nil { //nolint:gosec // a test key in a temp dir
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	p.topic = filepath.Join(p.base, "t1")
	if _, err := p.cache.CloneTopic(ctx, p.topic, "main", p.topicBr); err != nil {
		t.Fatal(err)
	}
	p.commitFile("a.txt", "docs: add a")
	first := mustGit(t, p.env, p.topic, "rev-parse", "HEAD")
	p.commitFile("b.txt", "docs: add b")
	// A later attempt to fix the first commit, as an agent would leave it.
	if err := os.WriteFile(filepath.Join(p.topic, "a.txt"), []byte("fixed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, p.env, p.topic, "add", "a.txt")
	mustGit(t, p.env, p.topic, "commit", "--quiet", "--fixup="+first)

	listed := newGit(t, WithAlternates(p.cache.ObjectsDir()))
	p.g = listed
	var err error
	if p.repo, err = listed.InitBare(ctx, filepath.Join(p.base, "supervisor.git")); err != nil {
		t.Fatal(err)
	}
	p.fetch()
	p.lintFn = func(msg string) []string {
		return commitlint.Lint(msg, commitlint.Options{Author: "whr-bot <bot@example.test>"})
	}
	return p
}

func (p *prep) commitFile(name, msg string) {
	p.t.Helper()
	if err := os.WriteFile(filepath.Join(p.topic, name), []byte(name+"\n"), 0o600); err != nil {
		p.t.Fatal(err)
	}
	mustGit(p.t, p.env, p.topic, "add", name)
	mustGit(p.t, p.env, p.topic, "commit", "--quiet", "-m", msg)
}

func (p *prep) fetch() {
	p.t.Helper()
	ctx := context.Background()
	if _, err := p.repo.FetchBranch(ctx, p.topic, p.topicBr); err != nil {
		p.t.Fatal(err)
	}
	if err := p.repo.FetchTarget(ctx, p.cache, "main", 0); err != nil {
		p.t.Fatal(err)
	}
}

func (p *prep) spec() PrepareSpec {
	return PrepareSpec{
		Target: "main", Topic: p.topicBr, Committer: Identity{Name: "whr-bot", Email: "bot@example.test"},
		SigningKey: p.key, Lint: p.lintFn,
	}
}

func (p *prep) rev(ref string) string {
	return mustGit(p.t, p.env, p.repo.Path(), "rev-parse", ref)
}

func TestPrepareRebasesFoldsAndSigns(t *testing.T) {
	ctx := context.Background()
	p := newPrep(t)
	// The target moved on while the agent worked.
	p.forge.commit(3)
	if err := p.cache.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	p.fetch()
	oldTip := p.rev("refs/heads/agent/topic")

	got, err := p.repo.Prepare(ctx, p.spec())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Commits) != 2 {
		t.Fatalf("commits = %v, want the fixup folded into two", got.Commits)
	}
	if p.rev("refs/heads/agent/topic") != got.SHA || got.SHA == oldTip {
		t.Errorf("the topic branch is at %s, want the new tip %s", p.rev("refs/heads/agent/topic"), got.SHA)
	}
	// On top of the moved target, in a line.
	if base := mustGit(t, p.env, p.repo.Path(), "merge-base", "refs/heads/main", got.SHA); base != p.rev("refs/heads/main") {
		t.Errorf("the topic is not on the target tip")
	}
	// The fixup landed in the first commit.
	if blob := mustGit(t, p.env, p.repo.Path(), "show", got.Commits[0]+":a.txt"); blob != "fixed" {
		t.Errorf("a.txt in the first commit = %q, want the fixed content", blob)
	}
	for _, c := range got.Commits {
		if committer := mustGit(t, p.env, p.repo.Path(), "log", "-1", "--format=%cn <%ce>", c); committer != "whr-bot <bot@example.test>" {
			t.Errorf("committer of %s = %q", c, committer)
		}
		if author := mustGit(t, p.env, p.repo.Path(), "log", "-1", "--format=%an", c); author != "t" {
			t.Errorf("the author was rewritten to %q", author)
		}
		if raw := mustGit(t, p.env, p.repo.Path(), "cat-file", "commit", c); !strings.Contains(raw, "gpgsig") {
			t.Errorf("commit %s is not signed", c)
		}
	}
	// No worktree is left behind.
	if list := mustGit(t, p.env, p.repo.Path(), "worktree", "list"); strings.Count(list, "\n") != 0 {
		t.Errorf("worktrees left: %s", list)
	}
}

func TestPrepareSignsEvenWhenNothingMoved(t *testing.T) {
	ctx := context.Background()
	p := newPrep(t)
	got, err := p.repo.Prepare(ctx, p.spec())
	if err != nil {
		t.Fatal(err)
	}
	// A second run on its own result still rewrites and signs.
	again, err := p.repo.Prepare(ctx, p.spec())
	if err != nil || len(again.Commits) != len(got.Commits) {
		t.Fatalf("second prepare: %+v, %v", again, err)
	}
}

func TestPrepareRefusesBadCommitMessagesAndKeepsTheBranch(t *testing.T) {
	ctx := context.Background()
	p := newPrep(t)
	p.commitFile("c.txt", "oops this is not conventional")
	p.fetch()
	before := p.rev("refs/heads/agent/topic")

	_, err := p.repo.Prepare(ctx, p.spec())
	if !errors.Is(err, ErrLint) || strings.Count(err.Error(), "\n") == 0 {
		t.Fatalf("Prepare = %v, want ErrLint with the problems listed", err)
	}
	if p.rev("refs/heads/agent/topic") != before {
		t.Error("a refused topic was moved")
	}
	if list := mustGit(t, p.env, p.repo.Path(), "worktree", "list"); strings.Count(list, "\n") != 0 {
		t.Errorf("worktrees left: %s", list)
	}
}

func TestPrepareReportsAConflictAndKeepsTheBranch(t *testing.T) {
	ctx := context.Background()
	p := newPrep(t)
	// The agent and the target both change file.txt.
	if err := os.WriteFile(filepath.Join(p.topic, "file.txt"), []byte("agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, p.env, p.topic, "add", "file.txt")
	mustGit(t, p.env, p.topic, "commit", "--quiet", "-m", "docs: change file")
	p.forge.commit(2) // rewrites file.txt
	if err := p.cache.Refresh(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	p.fetch()
	before := p.rev("refs/heads/agent/topic")

	if _, err := p.repo.Prepare(ctx, p.spec()); !errors.Is(err, ErrRebaseConflict) {
		t.Fatalf("Prepare = %v, want ErrRebaseConflict", err)
	}
	if p.rev("refs/heads/agent/topic") != before {
		t.Error("a conflicting topic was moved")
	}
	if list := mustGit(t, p.env, p.repo.Path(), "worktree", "list"); strings.Count(list, "\n") != 0 {
		t.Errorf("a failed rebase left a worktree: %s", list)
	}
}

func TestPrepareNeedsTheBotKeyAndIdentity(t *testing.T) {
	ctx := context.Background()
	p := newPrep(t)
	for name, mod := range map[string]func(*PrepareSpec){
		"no key":            func(s *PrepareSpec) { s.SigningKey = "" },
		"a relative key":    func(s *PrepareSpec) { s.SigningKey = "key" },
		"a missing key":     func(s *PrepareSpec) { s.SigningKey = filepath.Join(p.base, "nope") },
		"no committer name": func(s *PrepareSpec) { s.Committer.Name = "" },
	} {
		s := p.spec()
		mod(&s)
		if _, err := p.repo.Prepare(ctx, s); !errors.Is(err, ErrNoSigningKey) {
			t.Errorf("%s: Prepare = %v, want ErrNoSigningKey", name, err)
		}
	}
	s := p.spec()
	s.Topic = "--upload-pack=x"
	if _, err := p.repo.Prepare(ctx, s); !errors.Is(err, ErrBadBranch) {
		t.Errorf("a topic named like an option = %v", err)
	}
	s.Topic = "agent/missing"
	if _, err := p.repo.Prepare(ctx, s); !errors.Is(err, ErrBadBranch) {
		t.Errorf("an unknown topic = %v", err)
	}
}

func TestPushSendsTheApprovedCommitOnly(t *testing.T) {
	ctx := context.Background()
	p := newPrep(t)
	prepared, err := p.repo.Prepare(ctx, p.spec())
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(p.base, "remote.git")
	if _, err := p.g.InitBare(ctx, remote); err != nil {
		t.Fatal(err)
	}

	if err := p.repo.Push(ctx, remote, "main", prepared.SHA); !errors.Is(err, ErrBadBranch) {
		t.Errorf("a push to main = %v, want ErrBadBranch", err)
	}
	for _, sha := range []string{"", "HEAD", "refs/heads/agent/topic", prepared.SHA[:10], strings.Repeat("0", 40)} {
		if err := p.repo.Push(ctx, remote, "agent/topic", sha); err == nil {
			t.Errorf("Push(%q) was accepted", sha)
		}
	}
	if err := p.repo.Push(ctx, "ssh://git@host/repo", "agent/topic", prepared.SHA); !errors.Is(err, ErrBadSource) {
		t.Errorf("an ssh remote = %v, want ErrBadSource", err)
	}
	if err := p.repo.Push(ctx, remote, "agent/topic", prepared.SHA); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, p.env, remote, "rev-parse", "refs/heads/agent/topic"); got != prepared.SHA {
		t.Errorf("the remote has %s, want the approved %s", got, prepared.SHA)
	}
	if tags := mustGit(t, p.env, remote, "tag", "--list"); tags != "" {
		t.Errorf("tags were pushed: %q", tags)
	}
	if branches := mustGit(t, p.env, remote, "branch", "--list"); strings.Contains(branches, "main") {
		t.Errorf("another branch went along: %q", branches)
	}
	// Pushed commits are never rewritten: a rewritten history is not a fast-forward.
	p.commitFile("late.txt", "docs: late")
	p.fetch()
	respec := p.spec()
	respec.Committer = Identity{Name: "another-bot", Email: "other@example.test"} // new committer: new commit IDs
	rewritten, err := p.repo.Prepare(ctx, respec)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.repo.Push(ctx, remote, "agent/topic", rewritten.SHA); !errors.Is(err, ErrNotFastForward) {
		t.Errorf("a push of rewritten history = %v, want ErrNotFastForward", err)
	}
	if got := mustGit(t, p.env, remote, "rev-parse", "refs/heads/agent/topic"); got != prepared.SHA {
		t.Errorf("a refused push changed the remote to %s", got)
	}
}
