package service

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

func rebuiltEvents(t *testing.T, r *wsRig, ws domain.Workspace) []domain.WorkspaceRebuilt {
	t.Helper()
	evs, err := r.store.EventsSince(bg, domain.WorkspaceStream(ws.ID), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.WorkspaceRebuilt
	for _, e := range evs {
		if e.Kind != domain.EventWorkspaceRebuilt {
			continue
		}
		if e.Tier != domain.TierAudit {
			t.Errorf("the rebuild entry is in tier %s, want audit", e.Tier)
		}
		var rb domain.WorkspaceRebuilt
		if err := json.Unmarshal(e.Payload, &rb); err != nil {
			t.Fatal(err)
		}
		out = append(out, rb)
	}
	return out
}

func envCount(t *testing.T, r *wsRig) int {
	t.Helper()
	infos, err := r.rt.Adapter.List(bg, r.rt.Owner)
	if err != nil {
		t.Fatal(err)
	}
	return len(infos)
}

func TestRebuildReplacesTheEnvironmentAndKeepsTheVolumesAndTheWorktrees(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.home = true
	w, a := r.create("docs-ws")
	oldEnv := w.EnvID
	oldInfo, err := r.rt.Adapter.Inspect(bg, string(oldEnv))
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.ws.Rebuild(bg, "docs-ws", "werner")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.store.Workspace(bg, "docs-ws")
	if err != nil {
		t.Fatal(err)
	}
	if got.EnvID == oldEnv || string(got.EnvID) != res.NewEnv || res.OldEnv != string(oldEnv) {
		t.Fatalf("env %s -> %s, result %+v", oldEnv, got.EnvID, res)
	}
	if _, err := r.rt.Adapter.Inspect(bg, string(oldEnv)); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("the old environment is still there: %v", err)
	}
	info, err := r.rt.Adapter.Inspect(bg, string(got.EnvID))
	if err != nil || info.State != domain.EnvRunning {
		t.Fatalf("the new environment: %+v, %v", info, err)
	}
	if n := envCount(t, r); n != 1 {
		t.Errorf("%d environments exist after the rebuild, want one", n)
	}
	// The volume and the folder are the same: what the agents keep is not lost.
	var oldVol, newVol, newBind string
	for _, m := range oldInfo.Mounts {
		if m.Kind == runtime.MountVolume {
			oldVol = m.Source
		}
	}
	for _, m := range info.Mounts {
		switch {
		case m.Kind == runtime.MountVolume:
			newVol = m.Source
		case m.Target == WorkspaceMount:
			newBind = m.Source
		}
	}
	if oldVol == "" || newVol != oldVol || newBind != w.Path {
		t.Errorf("volume %q -> %q, folder %q (want %q)", oldVol, newVol, newBind, w.Path)
	}
	if inv, _ := r.rt.Adapter.Inventory(bg); len(inv.Volumes) != 1 || inv.Volumes[0] != oldVol {
		t.Errorf("volumes after the rebuild: %v, want just %s", inv.Volumes, oldVol)
	}
	// Nothing in the clone is touched: the worktree was made once, at creation.
	if n := strings.Count(r.logs(got.EnvID), "worktree add"); n != 0 {
		t.Errorf("the new environment made a worktree %d times: the agent's worktree is in the folder already", n)
	}
	// The audit entry names both environments and the images.
	evs := rebuiltEvents(t, r, w)
	if len(evs) != 1 {
		t.Fatalf("rebuild entries: %+v", evs)
	}
	e := evs[0]
	if e.Actor != "werner" || e.OldEnv != oldEnv || string(e.NewEnv) != res.NewEnv || e.OldImage == "" || e.NewImage == "" ||
		!strings.HasPrefix(e.OldDigest, "sha256:") || !strings.HasPrefix(e.NewDigest, "sha256:") {
		t.Errorf("audit = %+v", e)
	}
	// An agent can start a task in the new environment.
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"}); err != nil {
		t.Errorf("a task does not start in the rebuilt environment: %v", err)
	}
}

func TestRebuildIsRefusedWhileARunIsLiveAndNamesIt(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	w, a := r.create("docs-ws")
	_, run, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#7"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ws.Rebuild(bg, "docs-ws", "werner")
	var ce *domain.ConflictError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), string(run)) {
		t.Fatalf("err = %v, want a conflict that names run %s", err, run)
	}
	got, _ := r.store.Workspace(bg, "docs-ws")
	if got.EnvID != w.EnvID || envCount(t, r) != 1 {
		t.Errorf("a refused rebuild changed something: env %s, %d environments", got.EnvID, envCount(t, r))
	}
	if info, _ := r.rt.Adapter.Inspect(bg, string(w.EnvID)); info.State != domain.EnvRunning {
		t.Errorf("the old environment was stopped by a refused rebuild: %v", info.State)
	}
	if len(rebuiltEvents(t, r, w)) != 0 {
		t.Error("a refused rebuild was audited as done")
	}
}

// A new environment that cannot be brought up is taken back and the old one runs
// again, as it was.
func TestAFailedRebuildRestoresTheOldEnvironment(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.home = true
	w, _ := r.create("docs-ws")
	fail := false
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if fail && len(cmd) == 2 && cmd[0] == "git" && cmd[1] == "--version" {
			return nil, "no git here", 1, true
		}
		return nil, "", 0, false
	}
	fail = true
	_, err := r.ws.Rebuild(bg, "docs-ws", "werner")
	if err == nil || !strings.Contains(err.Error(), "as it was") {
		t.Fatalf("err = %v, want the failure and that the old environment is as it was", err)
	}
	fail = false
	got, _ := r.store.Workspace(bg, "docs-ws")
	if got.EnvID != w.EnvID {
		t.Errorf("the record changed to %s", got.EnvID)
	}
	if info, err := r.rt.Adapter.Inspect(bg, string(w.EnvID)); err != nil || info.State != domain.EnvRunning {
		t.Errorf("the old environment: %+v, %v", info, err)
	}
	if n := envCount(t, r); n != 1 {
		t.Errorf("%d environments after a failed rebuild, want the old one only", n)
	}
	if inv, _ := r.rt.Adapter.Inventory(bg); len(inv.Volumes) != 1 || len(inv.Networks) != 1 {
		t.Errorf("a failed rebuild left resources behind or took the old ones: %+v", inv)
	}
	if len(rebuiltEvents(t, r, w)) != 0 {
		t.Error("a failed rebuild was audited as done")
	}
	// And a rebuild afterwards works: nothing of the failed one is in the way.
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Errorf("a rebuild after a failed one: %v", err)
	}
}

// A task is not started while the workspace is being rebuilt.
func TestATaskIsNotStartedWhileTheWorkspaceIsBeingRebuilt(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	w, a := r.create("docs-ws")
	var (
		once    sync.Once
		startEr error
	)
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 2 && cmd[0] == "git" && cmd[1] == "--version" {
			once.Do(func() { // the new environment is being checked: a start comes in
				_, _, startEr = r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#9"})
			})
		}
		return nil, "", 0, false
	}
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Fatal(err)
	}
	var ce *domain.ConflictError
	if !errors.As(startEr, &ce) || !strings.Contains(startEr.Error(), "being rebuilt") {
		t.Errorf("a task started during the rebuild: %v", startEr)
	}
	if _, _, err := r.ws.StartTask(bg, StartRequest{AgentID: a.ID, Issue: "#9"}); err != nil {
		t.Errorf("after the rebuild a task is refused: %v", err)
	}
	_ = w
}

func TestRebuildOfAnUnknownWorkspaceOrOneWithoutAnEnvironment(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	if _, err := r.ws.Rebuild(bg, "nope", "werner"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown workspace: %v", err)
	}
}

// interruptedRun saves a task whose run in the workspace's environment was
// interrupted with a session, the state the reconciler resumes.
func interruptedRun(t *testing.T, r *wsRig, w domain.Workspace, a domain.Agent, id domain.ID) {
	t.Helper()
	agg := domain.NewTaskAggregate(domain.Task{ID: "t-" + id, Repo: w.Repo, Issue: "#3", State: domain.TaskQueued, AgentID: a.ID, CreatedAt: r.svc.clock.Now()})
	agg.AddEnvironment(domain.Environment{ID: w.EnvID, Backend: "fake", State: domain.EnvRunning})
	for _, err := range []error{
		agg.StartRun(domain.Run{ID: id, WorkspaceID: w.ID, AgentID: a.ID, EnvID: w.EnvID}),
		agg.MarkRunning(id),
		agg.RecordSession(id, "sess-"+string(id)),
		agg.Interrupt(id),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.store.SaveTask(bg, agg); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildIsRefusedWhileARunIsInterruptedAndNamesIt(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	w, a := r.create("docs-ws")
	interruptedRun(t, r, w, a, "r-int")
	if live, _ := r.store.LiveRuns(bg, w.EnvID); len(live) != 0 {
		t.Fatalf("setup: the run is live: %+v", live)
	}
	_, err := r.ws.Rebuild(bg, "docs-ws", "werner")
	var ce *domain.ConflictError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "r-int") || !strings.Contains(err.Error(), string(domain.RunInterrupted)) {
		t.Fatalf("err = %v, want a conflict that names run r-int and its state", err)
	}
	got, _ := r.store.Workspace(bg, "docs-ws")
	if got.EnvID != w.EnvID || envCount(t, r) != 1 {
		t.Errorf("a refused rebuild changed something: env %s, %d environments", got.EnvID, envCount(t, r))
	}
	if info, _ := r.rt.Adapter.Inspect(bg, string(w.EnvID)); info.State != domain.EnvRunning {
		t.Errorf("the old environment was stopped by a refused rebuild: %v", info.State)
	}
}

func TestNothingStartsTheOldEnvironmentWhileTheWorkspaceIsBeingRebuilt(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	w, a := r.create("docs-ws")
	pub := NewPublisher(r.svc, PublishConfig{Workspaces: r.ws})
	var (
		once                                 sync.Once
		recErr, exportErr, addErr, rebaseErr error
		oldState                             domain.EnvState
		rep                                  Report
	)
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 2 && cmd[0] == "git" && cmd[1] == "--version" {
			once.Do(func() { // the new environment is being checked, the old one is stopped
				interruptedRun(t, r, w, a, "r-late")
				rep, recErr = r.svc.Reconcile(bg)
				exportErr = pub.exportBranch(bg, Request{Agent: a.ID, Branch: a.Branch}, "r-late", false)
				_, addErr = r.ws.AddAgent(bg, "docs-ws", "extra", "x", "")
				rebaseErr = r.ws.Rebase(bg, a.ID)
				info, _ := r.rt.Adapter.Inspect(bg, string(w.EnvID))
				oldState = info.State
			})
		}
		return nil, "", 0, false
	}
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Fatal(err)
	}
	if recErr != nil || len(rep.Resumed) != 0 {
		t.Errorf("the reconciler resumed during the rebuild: %+v, %v", rep, recErr)
	}
	for name, err := range map[string]error{"export": exportErr, "AddAgent": addErr, "Rebase": rebaseErr} {
		var ce *domain.ConflictError
		if !errors.As(err, &ce) || !strings.Contains(err.Error(), "being rebuilt") {
			t.Errorf("%s during the rebuild: %v, want a conflict", name, err)
		}
	}
	if oldState == domain.EnvRunning {
		t.Error("the old environment was started during the rebuild")
	}
	// The run is still interrupted: the next pass takes it up, once the rebuild is over.
	got, _ := r.store.LoadTask(bg, "t-r-late")
	if run, _ := got.Run("r-late"); run.State != domain.RunInterrupted {
		t.Errorf("run = %s, want interrupted for the next pass", run.State)
	}
}

func TestARunIsNotSavedAgainstAnEnvironmentThatWasRebuiltMeanwhile(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	stale, a := r.create("docs-ws") // the record a start loaded before the swap
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Fatal(err)
	}
	agg := domain.NewTaskAggregate(domain.Task{ID: "t-stale", Repo: stale.Repo, Issue: "#4", State: domain.TaskQueued, AgentID: a.ID, CreatedAt: r.svc.clock.Now()})
	agg.AddEnvironment(domain.Environment{ID: stale.EnvID, Backend: "fake", State: domain.EnvRunning})
	if err := agg.StartRun(domain.Run{ID: "r-stale", WorkspaceID: stale.ID, AgentID: a.ID, EnvID: stale.EnvID}); err != nil {
		t.Fatal(err)
	}
	_, err := r.ws.saveStartingRun(bg, stale, agg)
	var ce *domain.ConflictError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v, want a conflict that says the environment changed", err)
	}
	if _, lerr := r.store.LoadTask(bg, "t-stale"); lerr == nil {
		t.Error("the run was saved against the old environment")
	}
}

func TestALeaseKeepsARebuildOutAndARebuildKeepsALeaseOut(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	ws, _ := r.create("docs-ws")
	release, err := r.svc.leaseEnvironment(ws)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ws.Rebuild(bg, "docs-ws", "werner")
	var ce *domain.ConflictError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "starting or using") {
		t.Fatalf("a rebuild under a lease: %v, want a conflict", err)
	}
	release()
	release() // a second release changes nothing
	if r.svc.rebuilding(ws.ID) {
		t.Error("a refused rebuild left the workspace marked")
	}
	var leaseErr error
	r.fake.OnExec = func(_ string, cmd []string) ([]byte, string, int, bool) {
		if len(cmd) == 2 && cmd[0] == "git" && cmd[1] == "--version" {
			var rel func()
			if rel, leaseErr = r.svc.leaseEnvironment(ws); rel != nil {
				rel()
			}
		}
		return nil, "", 0, false
	}
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Fatal(err)
	}
	if !errors.As(leaseErr, &ce) || !strings.Contains(leaseErr.Error(), "being rebuilt") {
		t.Errorf("a lease during the rebuild: %v, want a conflict", leaseErr)
	}
}

func TestTheLeaseIsReleasedWhenTheStartOrTheExportIsDone(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	ws, a := r.create("docs-ws")
	if err := r.ws.ensureEnvironment(bg, ws); err != nil {
		t.Fatal(err)
	}
	if n := r.svc.leases[ws.ID]; n != 0 {
		t.Errorf("leases after ensureEnvironment = %d", n)
	}
	pub := NewPublisher(r.svc, PublishConfig{Workspaces: r.ws})
	_ = pub.exportBranch(bg, Request{Agent: a.ID, Branch: a.Branch}, "r-none", false) // whatever it answers
	if n := r.svc.leases[ws.ID]; n != 0 {
		t.Errorf("leases after exportBranch = %d", n)
	}
	if err := r.ws.Rebase(bg, a.ID); err != nil {
		t.Logf("rebase: %v", err)
	}
	if n := r.svc.leases[ws.ID]; n != 0 {
		t.Errorf("leases after Rebase = %d", n)
	}
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Errorf("a rebuild after the operations: %v", err)
	}
}

func TestARebuildFromAStaleRecordDoesNotSwapFromAStaleEnvironment(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	stale, _ := r.create("docs-ws")
	if _, err := r.ws.Rebuild(bg, "docs-ws", "werner"); err != nil {
		t.Fatal(err)
	}
	cur, err := r.store.Workspace(bg, string(stale.ID))
	if err != nil || cur.EnvID == stale.EnvID {
		t.Fatalf("setup: the environment did not change (%v)", err)
	}
	got, err := r.ws.beginRebuild(bg, stale)
	if err != nil {
		t.Fatal(err)
	}
	defer r.ws.endRebuild(got)
	if got.EnvID != cur.EnvID {
		t.Errorf("beginRebuild returned env %s, want the fresh %s", got.EnvID, cur.EnvID)
	}
	// The compare-and-set the swap uses refuses the stale environment.
	ev := domain.NewWorkspaceRebuiltEvent(domain.WorkspaceRebuilt{ID: stale.ID, Name: stale.Name, OldEnv: stale.EnvID, NewEnv: "x"}, r.svc.clock.Now())
	if err := r.store.SwapWorkspaceEnv(bg, stale.ID, stale.EnvID, "x", ev); err == nil {
		t.Error("a swap from the stale environment succeeded")
	}
}
