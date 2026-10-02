package service

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/commitlint"
	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/forge/forgetest"
	"github.com/wstein/workharbor/internal/gittest"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/policy"
	"github.com/wstein/workharbor/internal/runtime/runtimetest"
)

func plainGit(t *testing.T, home, dir string, args ...string) {
	t.Helper()
	cmd := gittest.Git(context.Background(), home, dir, gittest.Identity, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// remoteForge is a fake forge whose branches are read from a real bare
// repository, so the Guard's check that the branch is at the approved commit
// is against what was really pushed.
type remoteForge struct {
	*forgetest.Fake
	t      *testing.T
	remote string
	home   string
}

func (f *remoteForge) BranchSHA(_ context.Context, _, branch string) (string, error) {
	cmd := gittest.Git(context.Background(), f.home, f.remote, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("no such branch")
	}
	return strings.TrimSpace(string(out)), nil
}

type pubRig struct {
	*rig
	pub      *Publisher
	forge    *remoteForge
	checkout string
	home     string
	repo     *hostgit.Repo
	remote   string
	req      Request
	checks   int
	checkErr error

	// the agent's side: a workspace and agent record, and the fake guest that
	// answers the git commands of an export with real git in checkout
	agent domain.Agent
	guest func(cmd []string) (stdout []byte, stderr string, code int, handled bool)
	cmds  []string
}

// newPubRig builds the chain: a forge repository, the cache, an agent
// checkout with two commits and a fixup, the supervisor's copy, a remote, and
// a task whose run has stopped.
func newPubRig(t *testing.T) *pubRig {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not available")
	}
	r := newRig(t)
	pr := &pubRig{rig: r}
	base := t.TempDir()
	pr.home = filepath.Join(base, "home")
	// The forge's repository lives outside the workspace root: a cache is never
	// fed from something an agent can write.
	outside, err := os.MkdirTemp("", "forge-")
	must(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	src := filepath.Join(outside, "src")
	for _, d := range []string{pr.home, src} {
		must(t, os.MkdirAll(d, 0o750))
	}
	plainGit(t, pr.home, src, "init", "--quiet", "-b", "main")
	for i, name := range []string{"one", "two", "three"} {
		must(t, os.WriteFile(filepath.Join(src, "file.txt"), []byte(name+"\n"), 0o600))
		plainGit(t, pr.home, src, "add", "file.txt")
		plainGit(t, pr.home, src, "commit", "--quiet", "-m", "c"+string(rune('0'+i)))
	}

	root, err := filepath.EvalSymlinks(base)
	must(t, err)
	g0, err := hostgit.New(hostgit.WithWorkspaceRoot(root))
	must(t, err)
	t.Cleanup(func() { _ = g0.Close() })
	cache, err := g0.OpenCache(bg, filepath.Join(root, "cache.git"), hostgit.CacheConfig{Source: src})
	must(t, err)
	must(t, cache.Refresh(bg, "main"))
	pr.checkout = filepath.Join(root, "ws1")
	plainGit(t, pr.home, root, "clone", "--quiet", "--no-tags", "--shared", "--branch", "main", cache.Path(), pr.checkout)
	plainGit(t, pr.home, pr.checkout, "checkout", "--quiet", "-b", "agent/topic")
	must(t, os.WriteFile(filepath.Join(pr.checkout, "a.txt"), []byte("a\n"), 0o600))
	plainGit(t, pr.home, pr.checkout, "add", "a.txt")
	plainGit(t, pr.home, pr.checkout, "commit", "--quiet", "-m", "docs: add a")

	g, err := hostgit.New(hostgit.WithWorkspaceRoot(root))
	must(t, err)
	t.Cleanup(func() { _ = g.Close() })
	pr.repo, err = g.InitBare(bg, filepath.Join(root, "supervisor.git"))
	must(t, err)
	pr.remote = filepath.Join(root, "remote.git")
	_, err = g.InitBare(bg, pr.remote)
	must(t, err)

	key := filepath.Join(root, "bot-key")
	if out, err := gittest.SSHKeygen(bg, pr.home, "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	fake := forgetest.NewFake()
	pr.forge = &remoteForge{Fake: fake, t: t, remote: pr.remote, home: pr.home}
	guard := forge.NewGuard(pr.forge, RepoPusher{Repo: pr.repo, Remote: pr.remote}, policy.Default(), r.svc.VerifierFor())
	pr.pub = NewPublisher(r.svc, PublishConfig{
		Repo: pr.repo, Cache: cache, Guard: guard, ForgeRepo: "wstein/workharbor",
		Prepare: hostgit.PrepareSpec{
			Committer: hostgit.Identity{Name: "whr-bot", Email: "bot@example.test"}, SigningKey: key,
			Lint: func(m string) []string {
				return commitlint.Lint(m, commitlint.Options{Author: "whr-bot <bot@example.test>"})
			},
		},
		Checks: func(context.Context, domain.ID, string) error { pr.checks++; return pr.checkErr },
	})
	pr.req = Request{Task: "t1", Branch: "agent/topic", Target: "main", DecisionID: "review-1"}

	// The run stops (the agent finished), but the environment still runs.
	a := r.load()
	must(t, a.StopRun("r1"))
	_, err = r.store.SaveTask(bg, a)
	must(t, err)

	// The agent lives in a workspace whose environment is the task's; the fake
	// runtime answers the git commands an export sends.
	ws, wev, err := domain.NewWorkspace("w1", "docs-ws", "/ws/docs", "wstein/workharbor", "main", t0)
	must(t, err)
	ws.EnvID = pr.env
	must(t, r.store.AddWorkspace(bg, ws, wev))
	must(t, r.store.SetWorkspaceEnv(bg, ws.ID, pr.env))
	ag, aev, err := domain.NewAgent("a1", ws.ID, "topic", "", "", t0) // branch agent/topic, as the checkout
	must(t, err)
	must(t, r.store.AddAgent(bg, ag, aev))
	pr.agent = ag
	pr.req.Agent = ag.ID
	pr.pub.cfg.Workspaces = NewWorkspaces(r.svc, WorkspaceConfig{NewID: func() domain.ID { return "x" }})
	pr.rt.Adapter.(*runtimetest.Fake).OnExec = pr.onExec
	return pr
}

func (p *pubRig) envState() domain.EnvState {
	info, err := p.rt.Adapter.Inspect(bg, string(p.env))
	must(p.t, err)
	return info.State
}

func (p *pubRig) allow(sha string) error {
	return p.svc.AnswerDecision(bg, "review-1", domain.Response{By: "werner", Option: domain.AnswerAllow, SHA: sha, At: p.clock.now})
}

// remoteHas reports whether the remote has the topic branch.
func (p *pubRig) remoteHas() bool {
	_, err := p.forge.BranchSHA(bg, "", "agent/topic")
	return err == nil
}

func TestPrepareRefusesALiveRun(t *testing.T) {
	p := newPubRig(t)
	// A live run.
	a := p.load()
	must(t, a.StartRun(domain.Run{ID: "r2", EnvID: p.env}))
	_, err := p.store.SaveTask(bg, a)
	must(t, err)
	if _, err := p.pub.Prepare(bg, p.req); !errors.Is(err, ErrNotReadyYet) {
		t.Errorf("a live run = %v, want ErrNotReadyYet", err)
	}
}

func TestFailingChecksStopTheFlowBeforeAnyDecision(t *testing.T) {
	p := newPubRig(t)
	p.checkErr = errors.New("make check failed")
	if _, err := p.pub.Prepare(bg, p.req); err == nil || !strings.Contains(err.Error(), "make check failed") {
		t.Fatalf("Prepare = %v", err)
	}
	if len(p.load().Decisions()) != 0 || p.load().Task().State == domain.TaskReadyForReview {
		t.Error("a decision was raised for a commit whose checks failed")
	}
}

func TestPublishNeedsTheApprovalOfExactlyThePinnedCommit(t *testing.T) {
	p := newPubRig(t)
	prepared, err := p.pub.Prepare(bg, p.req)
	must(t, err)

	// Not answered yet.
	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); !errors.Is(err, forge.ErrNotApproved) {
		t.Errorf("an open decision = %v, want ErrNotApproved", err)
	}
	// An allow for another commit is a denial.
	if err := p.allow("ffffffffffffffffffffffffffffffffffffffff"); !errors.Is(err, domain.ErrSHAMismatch) {
		t.Fatalf("answer = %v, want ErrSHAMismatch", err)
	}
	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); !errors.Is(err, forge.ErrNotApproved) {
		t.Errorf("a denied decision = %v, want ErrNotApproved", err)
	}
	if p.remoteHas() || len(p.forge.PRs) != 0 {
		t.Fatal("a refused publish reached the forge")
	}
	_ = prepared
}

func TestPublishPushesTheApprovedCommitAndOpensThePR(t *testing.T) {
	p := newPubRig(t)
	prepared, err := p.pub.Prepare(bg, p.req)
	must(t, err)
	must(t, p.allow(prepared.SHA))

	pr, err := p.pub.Publish(bg, "t1", "review-1", "Add a", "body")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := p.forge.BranchSHA(bg, "", "agent/topic")
	if got != prepared.SHA || pr.SHA != prepared.SHA || pr.URL == "" {
		t.Errorf("remote at %s, PR %+v; want the approved %s", got, pr, prepared.SHA)
	}
	cand, _ := p.load().CurrentCandidate()
	if cand.PRURL != pr.URL {
		t.Errorf("the candidate records %q, want the PR URL %q", cand.PRURL, pr.URL)
	}
	// Merging, tagging and releasing stay refused through the same guard.
	g := p.pub.cfg.Guard
	for name, err := range map[string]error{"merge": g.Merge(bg, "r", 1), "tag": g.Tag(bg, "r", "v1"), "release": g.Release(bg, "r", "v1"), "deploy": g.Deploy(bg, "r", "prod")} {
		if !errors.Is(err, forge.ErrForbidden) {
			t.Errorf("%s = %v", name, err)
		}
	}
	// Webhooks go through the contract with an http.Header.
	p.forge.WebhookSecret = "s"
	h := http.Header{}
	h.Set("X-Signature", "s")
	if err := g.VerifyWebhook(h, nil); err != nil {
		t.Errorf("webhook: %v", err)
	}
}

// rework starts and stops a second run on the same environment, as a rework
// after review does, and lets the agent commit more work in its checkout.
func (p *pubRig) rework(id domain.ID, file string) {
	p.t.Helper()
	must(p.t, p.rt.Adapter.Start(bg, string(p.env)))
	a := p.load()
	must(p.t, a.ObserveEnv(p.env, domain.EnvRunning))
	must(p.t, a.StartRun(domain.Run{ID: id, EnvID: p.env}))
	must(p.t, a.StopRun(id))
	_, err := p.store.SaveTask(bg, a)
	must(p.t, err)
	must(p.t, os.WriteFile(filepath.Join(p.checkout, file), []byte(file+"\n"), 0o600))
	plainGit(p.t, p.home, p.checkout, "add", file)
	plainGit(p.t, p.home, p.checkout, "commit", "--quiet", "-m", "docs: add "+file)
}

// #79: after a push, the next round extends the pushed commit instead of
// rewriting it, so the second push is a fast-forward.
func TestFollowUpRoundPushesAsAFastForward(t *testing.T) {
	p := newPubRig(t)
	first, err := p.pub.Prepare(bg, p.req)
	must(t, err)
	must(t, p.allow(first.SHA))
	_, err = p.pub.Publish(bg, "t1", "review-1", "Add a", "body")
	must(t, err)
	if c, ok := p.load().LastPushed(); !ok || c.SHA != first.SHA {
		t.Fatalf("LastPushed = %+v, %v; want the published %s", c, ok, first.SHA)
	}

	p.rework("r2", "b.txt")
	req := p.req
	req.DecisionID = "review-2"
	second, err := p.pub.Prepare(bg, req)
	must(t, err)
	if len(second.Commits) != 1 {
		t.Fatalf("follow-up commits = %v, want only the new one", second.Commits)
	}
	must(t, p.svc.AnswerDecision(bg, "review-2", domain.Response{By: "werner", Option: domain.AnswerAllow, SHA: second.SHA, At: p.clock.now}))
	if _, err := p.pub.Publish(bg, "t1", "review-2", "Add a and b", "body"); err != nil {
		t.Fatalf("the follow-up publish = %v, want a fast-forward push", err)
	}
	if got, _ := p.forge.BranchSHA(bg, "", "agent/topic"); got != second.SHA {
		t.Fatalf("the remote has %s, want %s", got, second.SHA)
	}

	// The first approval does not cover the new revision.
	err = p.pub.cfg.Guard.Push(bg, "wstein/workharbor", "agent/topic", forge.Approval{DecisionID: "review-1", SHA: first.SHA})
	if !errors.Is(err, forge.ErrNotApproved) {
		t.Errorf("an approval for the earlier revision = %v, want ErrNotApproved", err)
	}
}

// #79: an approval given before the task was cancelled does not publish.
func TestCancelledTaskDoesNotPublish(t *testing.T) {
	p := newPubRig(t)
	prepared, err := p.pub.Prepare(bg, p.req)
	must(t, err)
	must(t, p.allow(prepared.SHA))
	must(t, p.svc.Cancel(bg, "t1"))

	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); err == nil {
		t.Fatal("a cancelled task was published")
	}
	err = p.pub.cfg.Guard.Push(bg, "wstein/workharbor", "agent/topic", forge.Approval{DecisionID: "review-1", SHA: prepared.SHA})
	if !errors.Is(err, forge.ErrNotApproved) {
		t.Errorf("the guard with a cancelled task's approval = %v, want ErrNotApproved", err)
	}
	if p.remoteHas() {
		t.Fatal("a cancelled task's commit reached the remote")
	}
}

// #79: the repository's checks are required, never silently skipped.
func TestPrepareNeedsChecks(t *testing.T) {
	p := newPubRig(t)
	p.pub.cfg.Checks = nil
	if _, err := p.pub.Prepare(bg, p.req); !errors.Is(err, ErrNoChecks) {
		t.Fatalf("Prepare without checks = %v, want ErrNoChecks", err)
	}
}
