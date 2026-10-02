package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/gittest"
	"github.com/wstein/workharbor/internal/hostgit"
)

// guestGit runs git in the agent's checkout, as the guest would.
func (b *pubRig) guestGit(args ...string) (string, error) {
	cmd := gittest.Git(bg, b.home, b.checkout, gittest.Identity, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err != nil {
		return errOut.String(), err
	}
	return out.String(), nil
}

func (b *pubRig) onExec(_ string, cmd []string) ([]byte, string, int, bool) {
	b.cmds = append(b.cmds, strings.Join(cmd, " "))
	if b.guest != nil {
		if out, e, c, ok := b.guest(cmd); ok {
			return out, e, c, true
		}
	}
	return b.defaultExec(cmd)
}

// defaultExec is the guest that works: git run in the agent's checkout.
func (b *pubRig) defaultExec(cmd []string) ([]byte, string, int, bool) {
	if len(cmd) < 5 || cmd[0] != "git" || cmd[1] != "-C" || cmd[2] != b.agent.Worktree {
		return nil, "", 0, false
	}
	switch cmd[3] {
	case "rebase":
		return nil, "", 0, true
	case "merge-base":
		out, err := b.guestGit("merge-base", cmd[4], cmd[5])
		if err != nil {
			return nil, out, 1, true
		}
		return []byte(out), "", 0, true
	case "bundle":
		file := filepath.Join(b.t.TempDir(), "x.bundle")
		if out, err := b.guestGit("bundle", "create", file, cmd[6]); err != nil {
			return nil, out, 128, true
		}
		data, err := os.ReadFile(file) //nolint:gosec // a file the test just made
		must(b.t, err)
		return data, "", 0, true
	}
	return nil, "", 0, false
}

func (b *pubRig) ran(substr string) bool {
	for _, c := range b.cmds {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// The branch comes out of the running environment as a bundle: the environment
// is not stopped, no git runs on the checkout from the host, and what follows
// is as it was.
func TestPrepareFromABundleLeavesTheEnvironmentRunning(t *testing.T) {
	t.Parallel()
	b := newPubRig(t)
	if b.envState() != domain.EnvRunning {
		t.Fatal("setup: the environment should run")
	}
	prepared, err := b.pub.Prepare(bg, b.req)
	if err != nil {
		t.Fatal(err)
	}
	if b.envState() != domain.EnvRunning {
		t.Error("the environment was stopped: a bundle needs no stop")
	}
	if !b.ran("rebase main") || !b.ran("bundle create -") {
		t.Errorf("the guest was not asked to rebase and bundle: %v", b.cmds)
	}
	a := b.load()
	cand, ok := a.CurrentCandidate()
	d, _ := a.Decision("review-1")
	if !ok || cand.SHA != prepared.SHA || d.Kind != domain.DecisionReview || d.SHA != prepared.SHA || a.Task().State != domain.TaskReadyForReview {
		t.Errorf("candidate %+v, decision %+v, task %s", cand, d, a.Task().State)
	}
	if len(prepared.Commits) != 1 || b.checks != 1 {
		t.Errorf("commits %v, checks %d", prepared.Commits, b.checks)
	}
	if b.remoteHas() {
		t.Error("nothing may be pushed before the human approves")
	}
}

func TestExportRefusesWhatIsNotWhole(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	t.Run("larger than the limit", func(t *testing.T) {
		b := newPubRig(t)
		b.pub.cfg.MaxBundle = 10
		if _, err := b.pub.Prepare(bg, b.req); !errors.Is(err, hostgit.ErrBundleTooLarge) {
			t.Errorf("err = %v", err)
		}
		if b.load().Task().State == domain.TaskReadyForReview {
			t.Error("a refused export made the task ready")
		}
	})
	t.Run("cut in half", func(t *testing.T) {
		b := newPubRig(t)
		b.guest = func(cmd []string) ([]byte, string, int, bool) {
			out, e, c, ok := b.defaultExec(cmd)
			if ok && len(cmd) > 3 && cmd[3] == "bundle" {
				return out[:len(out)/2], e, c, true
			}
			return nil, "", 0, false
		}
		if _, err := b.pub.Prepare(bg, b.req); err == nil {
			t.Error("a truncated bundle was accepted")
		}
		if b.checks != 0 {
			t.Error("the checks ran on a refused bundle")
		}
	})
	t.Run("the guest command fails", func(t *testing.T) {
		b := newPubRig(t)
		b.guest = func(cmd []string) ([]byte, string, int, bool) {
			if len(cmd) > 3 && cmd[3] == "bundle" {
				return nil, "fatal: bad object", 128, true
			}
			return nil, "", 0, false
		}
		_, err := b.pub.Prepare(bg, b.req)
		if err == nil || !strings.Contains(err.Error(), "exited 128") || !strings.Contains(err.Error(), "bad object") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("nothing to export", func(t *testing.T) {
		b := newPubRig(t)
		b.guest = func(cmd []string) ([]byte, string, int, bool) {
			if len(cmd) > 3 && cmd[3] == "bundle" {
				return nil, "error: Refusing to create empty bundle.", 128, true
			}
			return nil, "", 0, false
		}
		if _, err := b.pub.Prepare(bg, b.req); !errors.Is(err, ErrNothingToExport) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a branch that is not the agent's", func(t *testing.T) {
		b := newPubRig(t)
		req := b.req
		req.Branch = "agent/other"
		if _, err := b.pub.Prepare(bg, req); err == nil || !strings.Contains(err.Error(), "is not agent") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a publisher without workspaces", func(t *testing.T) {
		b := newPubRig(t)
		b.pub.cfg.Workspaces = nil
		if _, err := b.pub.Prepare(bg, b.req); !errors.Is(err, ErrNoWorkspaces) {
			t.Errorf("err = %v", err)
		}
	})
}

// A rebase that conflicts raises the question of design §4.2 for the stopped
// run, with the paths as untrusted input, and exports nothing.
func TestARebaseConflictRaisesAQuestionForTheStoppedRun(t *testing.T) {
	t.Parallel()
	b := newPubRig(t)
	b.guest = func(cmd []string) ([]byte, string, int, bool) {
		if len(cmd) < 5 {
			return nil, "", 0, false
		}
		switch cmd[3] {
		case "rebase":
			if cmd[4] == "main" {
				return nil, "CONFLICT (content): Merge conflict in docs/a.md", 1, true
			}
		case "diff":
			return []byte("docs/a.md\ncmd/x.go\n"), "", 0, true
		}
		return nil, "", 0, false
	}
	_, err := b.pub.Prepare(bg, b.req)
	var c *domain.ConflictError
	if !errors.Is(err, ErrRebaseBlocked) || !errors.As(err, &c) || c.Rule != domain.RuleRebase {
		t.Fatalf("err = %v, want ErrRebaseBlocked wrapping rebase-conflict", err)
	}
	if !b.ran("rebase --abort") {
		t.Errorf("the rebase was not aborted: %v", b.cmds)
	}
	if b.ran("bundle create") {
		t.Error("a branch that does not rebase was exported")
	}
	a := b.load()
	var q domain.Decision
	for _, d := range a.Decisions() {
		if d.Cause == domain.CauseRebaseConflict && d.Status == domain.DecisionOpen {
			q = d
		}
	}
	if q.RunID != "r1" || !q.Blocking || q.Input != "docs/a.md\ncmd/x.go" || strings.Join(q.Options, ",") != "rework,retry,cancel" {
		t.Errorf("decision %+v", q)
	}
	if a.Task().State != domain.TaskAwaitingGuidance {
		t.Errorf("task = %s, want awaiting_guidance", a.Task().State)
	}
	run, _ := a.Run("r1")
	if run.State != domain.RunStopped {
		t.Errorf("the run is %s: the state machines must not change", run.State)
	}
	if b.checks != 0 {
		t.Error("the checks ran")
	}
}

// After a push the next round is a fast-forward: no rebase before the export, and
// a branch that rewrote the pushed commits is refused.
func TestFollowUpFromABundleIsAFastForwardAndRewritesAreRefused(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newPubRig(t)
	first, err := b.pub.Prepare(bg, b.req)
	must(t, err)
	must(t, b.allow(first.SHA))
	_, err = b.pub.Publish(bg, "t1", "review-1", "Add a", "body")
	must(t, err)

	b.cmds = nil
	b.rework("r2", "b.txt")
	req := b.req
	req.DecisionID = "review-2"
	second, err := b.pub.Prepare(bg, req)
	must(t, err)
	if b.ran("rebase") {
		t.Errorf("a follow-up round must not rebase the pushed commits: %v", b.cmds)
	}
	if len(second.Commits) != 1 {
		t.Fatalf("follow-up commits = %v", second.Commits)
	}
	must(t, b.svc.AnswerDecision(bg, "review-2", domain.Response{By: "werner", Option: domain.AnswerAllow, SHA: second.SHA, At: b.clock.now}))
	if _, err := b.pub.Publish(bg, "t1", "review-2", "Add a and b", "body"); err != nil {
		t.Fatalf("the follow-up publish = %v, want a fast-forward push", err)
	}

	// Now the agent rewrites what was handed in.
	b.rework("r3", "c.txt")
	if out, err := b.guestGit("reset", "--quiet", "--hard", "HEAD~2"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	must(t, os.WriteFile(filepath.Join(b.checkout, "z.txt"), []byte("z\n"), 0o600))
	plainGit(t, b.home, b.checkout, "add", "z.txt")
	plainGit(t, b.home, b.checkout, "commit", "--quiet", "-m", "docs: add z")
	req.DecisionID = "review-3"
	if _, err := b.pub.Prepare(bg, req); !errors.Is(err, hostgit.ErrHistoryRewritten) {
		t.Errorf("a rewrite of pushed commits = %v, want ErrHistoryRewritten", err)
	}
}

// The editor copy is made from a bundle whether or not the environment runs,
// in a directory of its own, and never from the agent's checkout.
func TestOpenCopyIsFreshWhileTheEnvironmentRunsAndNeverTheCheckout(t *testing.T) {
	t.Parallel()
	b := newPubRig(t)
	dir := filepath.Join(t.TempDir(), "copy")
	cp, err := b.pub.OpenCopy(bg, b.req, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Path != dir || cp.Path == b.checkout {
		t.Errorf("copy = %+v: the editor must get its own directory", cp)
	}
	// The agent's checkout is refused as a destination.
	if _, err := b.pub.OpenCopy(bg, b.req, b.checkout); !errors.Is(err, hostgit.ErrInsideWorkspace) {
		t.Errorf("the agent's checkout as the copy = %v, want ErrInsideWorkspace", err)
	}
	if b.envState() != domain.EnvRunning {
		t.Error("the environment was stopped")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Errorf("the agent's file is not in the copy: %v", err)
	}
	if rel, err := filepath.Rel(b.checkout, dir); err == nil && !strings.HasPrefix(rel, "..") {
		t.Error("the copy is inside the agent's checkout")
	}
}

// `whr open` finds the agent of a workspace, exports its branch and makes the
// copy in the supervisor's editor directory.
func TestWorkspacesOpenCopyMakesTheCopyOfTheAgentsBranch(t *testing.T) {
	t.Parallel()
	b := newPubRig(t)
	editor := t.TempDir()
	topics := func(_ context.Context, repo string) (*hostgit.Repo, *hostgit.Cache, error) {
		if repo != "wstein/workharbor" {
			return nil, nil, errors.New("unexpected repository " + repo)
		}
		return b.repo, b.pub.cfg.Cache, nil
	}
	w := NewWorkspaces(b.svc, WorkspaceConfig{NewID: func() domain.ID { return "x" }, Topics: topics, EditorDir: editor})

	cp, err := w.OpenCopy(bg, "docs-ws", "") // the only agent
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(editor, "docs-ws.topic")
	if cp.Path != want {
		t.Errorf("copy at %q, want %q", cp.Path, want)
	}
	if _, err := os.Stat(filepath.Join(want, "a.txt")); err != nil {
		t.Errorf("the agent's file is not in the copy: %v", err)
	}
	if cp2, err := w.OpenCopy(bg, "docs-ws", "topic"); err != nil || cp2.Path != want {
		t.Errorf("by role: %+v, %v", cp2, err)
	}

	// two agents whose names join to the same text do not share a copy
	if filepath.Join(editor, "a-b"+"."+"c") == filepath.Join(editor, "a"+"."+"b-c") {
		t.Error("workspace a-b with role c collides with workspace a with role b-c")
	}

	var nf *domain.NotFoundError
	if _, err := w.OpenCopy(bg, "docs-ws", "nope"); !errors.As(err, &nf) {
		t.Errorf("an unknown role = %v, want not found", err)
	}
	if _, err := w.OpenCopy(bg, "nope", ""); !errors.As(err, &nf) {
		t.Errorf("an unknown workspace = %v, want not found", err)
	}

	// With two agents an empty role is ambiguous and says which exist.
	ws, _ := b.store.Workspace(bg, "docs-ws")
	ag, aev, err := domain.NewAgent("a2", ws.ID, "second", "", "", t0)
	must(t, err)
	must(t, b.store.AddAgent(bg, ag, aev))
	var inv *domain.InvalidError
	if _, err := w.OpenCopy(bg, "docs-ws", ""); !errors.As(err, &inv) || !strings.Contains(err.Error(), "second") || !strings.Contains(err.Error(), "topic") {
		t.Errorf("an ambiguous workspace = %v, want the roles listed", err)
	}

	bare := NewWorkspaces(b.svc, WorkspaceConfig{NewID: func() domain.ID { return "x" }})
	if _, err := bare.OpenCopy(bg, "docs-ws", "topic"); !errors.Is(err, ErrNoEditorCopies) {
		t.Errorf("no setup = %v, want ErrNoEditorCopies", err)
	}
}
