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
	r := newWsRig(t)
	if _, err := r.ws.Rebuild(bg, "nope", "werner"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown workspace: %v", err)
	}
}
