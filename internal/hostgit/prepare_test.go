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
	"github.com/wstein/workharbor/internal/gittest"
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
	if out, err := gittest.SSHKeygen(ctx, t.TempDir(), "-q", "-t", "ed25519", "-N", "", "-f", p.key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	p.topic = filepath.Join(p.base, "t1")
	mustGit(t, p.env, p.base, "clone", "--quiet", "--no-tags", "--shared", "--branch", "main", p.cache.Path(), p.topic)
	mustGit(t, p.env, p.topic, "checkout", "--quiet", "-b", p.topicBr)
	p.commitFile("a.txt", "docs: add a")
	first := mustGit(t, p.env, p.topic, "rev-parse", "HEAD")
	p.commitFile("b.txt", "docs: add b")
	// A later attempt to fix the first commit, as an agent would leave it.
	if err := os.WriteFile(filepath.Join(p.topic, "a.txt"), []byte("fixed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, p.env, p.topic, "add", "a.txt")
	mustGit(t, p.env, p.topic, "commit", "--quiet", "--fixup="+first)

	var err error
	if p.repo, err = p.g.InitBare(ctx, filepath.Join(p.base, "supervisor.git")); err != nil {
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
	importBranch(p.t, p.repo, p.topic, p.topicBr)
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
	t.Parallel()
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
	// the diff stat against the target: a.txt and b.txt, one line each, nothing removed
	if got.Files != 2 || got.Added != 2 || got.Removed != 0 {
		t.Errorf("diff stat = %d files +%d -%d, want 2 files +2 -0", got.Files, got.Added, got.Removed)
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// #79: after a push, a follow-up round rebases only the agent's new commits
// onto the pushed commit, so the second push is a fast-forward and the pushed
// commits are not rewritten.
func TestPrepareFollowUpExtendsThePushedCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newPrep(t)
	first, err := p.repo.Prepare(ctx, p.spec())
	if err != nil {
		t.Fatal(err)
	}
	if first.Source == "" || first.Source == first.SHA {
		t.Fatalf("Source = %q, want the agent's own tip before the rewrite", first.Source)
	}
	remote := filepath.Join(p.base, "remote.git")
	if _, err := p.g.InitBare(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if err := p.repo.Push(ctx, remote, "agent/topic", first.SHA); err != nil {
		t.Fatal(err)
	}

	// The agent goes on from its own history, which the push did not change.
	p.commitFile("c.txt", "docs: add c")
	p.fetch()
	spec := p.spec()
	spec.Onto, spec.Upstream = first.SHA, first.Source
	second, err := p.repo.Prepare(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Commits) != 1 {
		t.Fatalf("follow-up commits = %v, want only the new one", second.Commits)
	}
	if base := mustGit(t, p.env, p.repo.Path(), "rev-parse", second.SHA+"^"); base != first.SHA {
		t.Fatalf("the new commit's parent is %s, want the pushed %s", base, first.SHA)
	}
	if err := p.repo.Push(ctx, remote, "agent/topic", second.SHA); err != nil {
		t.Fatalf("the follow-up push = %v, want a fast-forward", err)
	}

	// An agent that rewrote what it already handed in is refused, not guessed at.
	mustGit(t, p.env, p.topic, "reset", "--quiet", "--hard", "HEAD~3")
	p.commitFile("d.txt", "docs: add d")
	p.fetch()
	if _, err := p.repo.Prepare(ctx, spec); !errors.Is(err, ErrHistoryRewritten) {
		t.Fatalf("a rewritten agent history = %v, want ErrHistoryRewritten", err)
	}
	// Onto and Upstream go together and must be full commit IDs.
	bad := p.spec()
	bad.Onto = first.SHA
	if _, err := p.repo.Prepare(ctx, bad); err == nil {
		t.Fatal("Onto without Upstream was accepted")
	}
}

func TestNumstatReadsFilesLinesAndBinaries(t *testing.T) {
	t.Parallel()
	out := []byte("3\t1\ta.go\x00-\t-\timg.png\x0010\t0\tdir/new file.txt\x00")
	if f, a, r := numstat(out); f != 3 || a != 13 || r != 1 {
		t.Errorf("numstat = %d files +%d -%d, want 3 files +13 -1 (a binary adds no lines)", f, a, r)
	}
	if f, a, r := numstat(nil); f != 0 || a != 0 || r != 0 {
		t.Errorf("numstat of nothing = %d %d %d", f, a, r)
	}
}

// A topic with no commit after its target is an error, never a revision with no
// commits offered for review.
func TestPrepareRefusesATopicWithNoNewCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newPrep(t)
	mustGit(t, p.env, p.repo.Path(), "branch", "agent/empty", "refs/heads/main")
	spec := p.spec()
	spec.Topic = "agent/empty"
	if got, err := p.repo.Prepare(ctx, spec); !errors.Is(err, ErrNoCommits) {
		t.Fatalf("Prepare = %+v, %v, want ErrNoCommits", got, err)
	}
}
