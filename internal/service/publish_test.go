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
func newPubRig(t *testing.T, opts ...rigOption) *pubRig {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not available")
	}
	r := newRig(t, opts...)
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	p := newPubRig(t)
	p.pub.cfg.Checks = nil
	if _, err := p.pub.Prepare(bg, p.req); !errors.Is(err, ErrNoChecks) {
		t.Fatalf("Prepare without checks = %v, want ErrNoChecks", err)
	}
}

func hasCall(f *remoteForge, prefix string) bool {
	for _, c := range f.Calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// prepared returns an approved rig set to a workflow.
func approvedUnder(t *testing.T, preset policy.Preset, branch string, opts ...rigOption) (*pubRig, string) {
	t.Helper()
	p := newPubRig(t, opts...)
	p.pub.cfg.Workflow, p.pub.cfg.Branch = preset, branch
	prepared, err := p.pub.Prepare(bg, p.req)
	must(t, err)
	must(t, p.allow(prepared.SHA))
	return p, prepared.SHA
}

// Prototype: the approved commit is pushed and the integration branch is moved to
// it as a fast-forward; there is no pull request (D47).
func TestThePrototypeFastForwardsTheIntegrationBranchWithoutAPR(t *testing.T) {
	t.Parallel()
	p, sha := approvedUnder(t, policy.Prototype, "develop")
	pr, err := p.pub.Publish(bg, "t1", "review-1", "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.forge.BranchSHA(bg, "", "agent/topic"); got != sha {
		t.Errorf("the agent branch is at %s, want the approved %s", got, sha)
	}
	if len(p.forge.FastForwards) != 1 || p.forge.FastForwards[0] != "wstein/workharbor:develop@"+sha {
		t.Errorf("moves %v", p.forge.FastForwards)
	}
	if len(p.forge.PRs) != 0 || pr.URL != "" || hasCall(p.forge, "OpenPR") {
		t.Errorf("a prototype opened a PR: %+v %v", p.forge.PRs, p.forge.Calls)
	}
	if _, pushed := p.load().LastPushed(); !pushed {
		t.Error("the approved commit is not recorded as pushed")
	}
}

func TestAPrototypeBranchThatMovedIsRefusedAndNeverForced(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	p, _ := approvedUnder(t, policy.Prototype, "develop")
	p.forge.NotFF = true
	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); !errors.Is(err, forge.ErrNotFastForward) {
		t.Fatalf("a moved branch = %v, want ErrNotFastForward", err)
	}
	if _, pushed := p.load().LastPushed(); pushed {
		t.Error("a refused fast-forward was recorded as pushed")
	}
	if len(p.forge.FastForwards) != 0 {
		t.Errorf("moves %v", p.forge.FastForwards)
	}
	// without a branch to move there is nothing to do
	q, _ := approvedUnder(t, policy.Prototype, "")
	if _, err := q.pub.Publish(bg, "t1", "review-1", "t", "b"); err == nil {
		t.Error("a prototype without an integration branch published")
	}
}

func TestIntegrationOpensAPRIntoTheIntegrationBranchAndPublishedIntoTheDefault(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	p, _ := approvedUnder(t, policy.Integration, "develop")
	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil {
		t.Fatal(err)
	}
	if !hasCall(p.forge, "OpenPRInto wstein/workharbor:develop<-agent/topic@") || len(p.forge.FastForwards) != 0 {
		t.Errorf("integration: %v, moves %v", p.forge.Calls, p.forge.FastForwards)
	}
	q, _ := approvedUnder(t, policy.Published, "ignored")
	if _, err := q.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil {
		t.Fatal(err)
	}
	if hasCall(q.forge, "OpenPRInto") || !hasCall(q.forge, "OpenPR wstein/workharbor:agent/topic@") || len(q.forge.FastForwards) != 0 {
		t.Errorf("published: %v", q.forge.Calls)
	}
	// the default when nothing is set is integration
	d, _ := approvedUnder(t, "", "develop")
	if _, err := d.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil || !hasCall(d.forge, "OpenPRInto") {
		t.Errorf("the default workflow: %v %v", err, d.forge.Calls)
	}
}

// No preset sends anything without the approval of exactly the pinned commit.
func TestEveryPresetNeedsTheApprovalOfTheCommit(t *testing.T) {
	t.Parallel()
	for _, preset := range []policy.Preset{policy.Prototype, policy.Integration, policy.Published} {
		p := newPubRig(t)
		p.pub.cfg.Workflow, p.pub.cfg.Branch = preset, "develop"
		_, err := p.pub.Prepare(bg, p.req)
		must(t, err)
		if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); !errors.Is(err, forge.ErrNotApproved) {
			t.Errorf("%s: an open decision = %v", preset, err)
		}
		if p.remoteHas() || len(p.forge.PRs) != 0 || len(p.forge.FastForwards) != 0 {
			t.Errorf("%s: a refused publish reached the forge: %v", preset, p.forge.Calls)
		}
	}
}

// A task keeps the branch it started with, and publishes under the stricter of
// its own preset and the repository's current one (D47, §6).
func TestATaskPublishesToItsOwnBranchUnderTheStricterPreset(t *testing.T) {
	t.Parallel()
	started := func(workflow, branch string) rigOption {
		return withTask(func(tk *domain.Task) { tk.Workflow, tk.Branch = workflow, branch })
	}
	// the configuration moved the integration branch: the task still goes to its own
	p, sha := approvedUnder(t, policy.Prototype, "elsewhere", started("prototype", "develop"))
	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil {
		t.Fatal(err)
	}
	if len(p.forge.FastForwards) != 1 || p.forge.FastForwards[0] != "wstein/workharbor:develop@"+sha {
		t.Errorf("the task did not keep its branch: %v", p.forge.FastForwards)
	}
	// a prototype task in a repository that is now published opens a PR into the
	// default branch, and is no longer fast-forwarded
	q, _ := approvedUnder(t, policy.Published, "", started("prototype", "develop"))
	if _, err := q.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil {
		t.Fatal(err)
	}
	if len(q.forge.FastForwards) != 0 || hasCall(q.forge, "OpenPRInto") || !hasCall(q.forge, "OpenPR wstein/workharbor:agent/topic@") {
		t.Errorf("a stricter repository did not apply: %v, moves %v", q.forge.Calls, q.forge.FastForwards)
	}
	// a published task in a repository that is now a prototype stays published
	r, _ := approvedUnder(t, policy.Prototype, "develop", started("published", ""))
	if _, err := r.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil {
		t.Fatal(err)
	}
	if len(r.forge.FastForwards) != 0 || hasCall(r.forge, "OpenPRInto") {
		t.Errorf("a looser repository applied to a started task: %v, moves %v", r.forge.Calls, r.forge.FastForwards)
	}
	// an integration task goes into its own branch, not the repository's new one
	s, _ := approvedUnder(t, policy.Integration, "next", started("integration", "develop"))
	if _, err := s.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil || !hasCall(s.forge, "OpenPRInto wstein/workharbor:develop<-agent/topic@") {
		t.Errorf("integration task: %v %v", err, s.forge.Calls)
	}
}

// A task whose stored preset cannot be parsed fails the publish closed: nothing
// is pushed, and neither the repository's preset nor the strictest applies
// (§6, #236).
func TestATaskWithAnUnparsablePresetFailsThePublishClosed(t *testing.T) {
	t.Parallel()
	for _, repo := range []policy.Preset{policy.Prototype, policy.Published, policy.Integration} {
		p, _ := approvedUnder(t, repo, "develop", withTask(func(tk *domain.Task) { tk.Workflow = "removed-preset" }))
		if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); err == nil {
			t.Fatalf("repo %s: the publish succeeded with an unparsable task preset", repo)
		}
		if p.remoteHas() || len(p.forge.FastForwards) != 0 || hasCall(p.forge, "OpenPR") || hasCall(p.forge, "Push") {
			t.Errorf("repo %s: something was sent: %v, moves %v", repo, p.forge.Calls, p.forge.FastForwards)
		}
	}
}

// An empty stored preset is a task from before presets, not a corrupt value: it
// still means "no preset" and publishes under the repository's (§6, #236).
func TestATaskWithAnEmptyPresetStillPublishesUnderTheRepositoryPreset(t *testing.T) {
	t.Parallel()
	p, _ := approvedUnder(t, policy.Published, "", withTask(func(tk *domain.Task) { tk.Workflow = "" }))
	if _, err := p.pub.Publish(bg, "t1", "review-1", "t", "b"); err != nil {
		t.Fatalf("an empty task preset failed the publish: %v", err)
	}
	if !hasCall(p.forge, "OpenPR") {
		t.Errorf("the repository's published preset did not apply: %v", p.forge.Calls)
	}
}
