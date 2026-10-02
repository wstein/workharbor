package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// consoles gives the rig a console whose spec is the fake runtime's, with a
// home volume like the real one.
func (r *wsRig) consoles() *Consoles {
	return NewConsoles(r.svc, ConsoleConfig{
		Spec: func([]domain.Workspace) runtime.Spec {
			spec := r.rt.NewSpec()
			spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountVolume, Source: "wh-conformance-console-home", Target: "/home/whr"})
			return spec
		},
		Prepare: r.rt.Prepare,
	})
}

func (r *wsRig) consoleEnvs() []runtime.Info {
	r.t.Helper()
	infos, err := r.rt.Adapter.List(bg, r.rt.Owner)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []runtime.Info
	for _, in := range infos {
		if in.Labels[ConsoleLabel] == "1" {
			out = append(out, in)
		}
	}
	return out
}

func TestTheConsoleOpensOnceAndIsReusedForTheSameWritableSet(t *testing.T) {
	r := newWsRig(t)
	c := r.consoles()
	if st, err := c.Status(bg); err != nil || st != nil {
		t.Fatalf("no console yet: %+v %v", st, err)
	}
	first, err := c.Open(bg, nil)
	if err != nil || first.Reused || first.EnvID == "" || len(first.ReadWrite) != 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	envs := r.consoleEnvs()
	if len(envs) != 1 || envs[0].State != domain.EnvRunning || envs[0].Labels[ConsoleRWLabel] != "" {
		t.Fatalf("console envs = %+v", envs)
	}
	again, err := c.Open(bg, nil)
	if err != nil || !again.Reused || again.EnvID != first.EnvID || len(r.consoleEnvs()) != 1 {
		t.Errorf("a second open must reuse the console: %+v, %v", again, err)
	}
	if st, _ := c.Status(bg); st == nil || st.EnvID != first.EnvID {
		t.Errorf("status = %+v", st)
	}
	// A console that was stopped is started again, not replaced.
	if err := r.rt.Adapter.Stop(bg, first.EnvID); err != nil {
		t.Fatal(err)
	}
	if back, err := c.Open(bg, nil); err != nil || back.EnvID != first.EnvID || r.consoleEnvs()[0].State != domain.EnvRunning {
		t.Errorf("a stopped console: %+v, %v", back, err)
	}
}

func TestAnotherWritableSetNeedsTheConsoleClosedFirst(t *testing.T) {
	r := newWsRig(t)
	w, _ := r.create("docs-ws")
	c := r.consoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	_, err := c.Open(bg, []string{"docs-ws"})
	var ce *domain.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a conflict: the open console's mounts are not changed under its shells", err)
	}
	if err := c.Close(bg); err != nil || len(r.consoleEnvs()) != 0 {
		t.Fatalf("close: %v, envs %+v", err, r.consoleEnvs())
	}
	rw, err := c.Open(bg, []string{"docs-ws"})
	if err != nil || !reflect.DeepEqual(rw.ReadWrite, []string{"docs-ws"}) {
		t.Fatalf("rw = %+v, %v", rw, err)
	}
	if envs := r.consoleEnvs(); len(envs) != 1 || envs[0].Labels[ConsoleRWLabel] != string(w.ID) {
		t.Errorf("console envs = %+v", envs)
	}
	// By ID too, and the same set again is a reuse.
	if again, err := c.Open(bg, []string{string(w.ID), "docs-ws"}); err != nil || !again.Reused {
		t.Errorf("the same workspace by ID and name: %+v, %v", again, err)
	}
	if st, _ := c.Status(bg); st == nil || !reflect.DeepEqual(st.ReadWrite, []string{"docs-ws"}) {
		t.Errorf("status = %+v", st)
	}
	if _, err := c.Open(bg, []string{"nope"}); err == nil {
		t.Error("an unknown workspace was accepted")
	}
}

// The home volume holds the human's dotfiles and history, so closing the console
// leaves it, and the reconciler does not touch a console it did not make.
func TestClosingTheConsoleKeepsTheHomeVolumeAndTheReconcilerLeavesItAlone(t *testing.T) {
	r := newWsRig(t)
	c := r.consoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	if rep, err := r.svc.Reconcile(bg); err != nil || len(rep.Errors) != 0 {
		t.Fatalf("reconcile: %+v, %v", rep, err)
	}
	if envs := r.consoleEnvs(); len(envs) != 1 || envs[0].State != domain.EnvRunning {
		t.Fatalf("the reconciler changed the console: %+v", envs)
	}
	if err := c.Close(bg); err != nil {
		t.Fatal(err)
	}
	inv, err := r.rt.Adapter.Inventory(bg)
	if err != nil || len(inv.Volumes) != 1 || inv.Volumes[0] != "wh-conformance-console-home" || len(inv.Networks) != 0 {
		t.Errorf("inventory after close = %+v, %v: the volume stays, the network goes", inv, err)
	}
	// Closing again, and with none open, is not an error; the next open mounts the same home.
	if err := c.Close(bg); err != nil {
		t.Errorf("a second close: %v", err)
	}
	if _, err := c.Open(bg, nil); err != nil {
		t.Errorf("reopen: %v", err)
	}
}

func TestAConsoleThatCannotBePreparedLeavesNothing(t *testing.T) {
	r := newWsRig(t)
	c := NewConsoles(r.svc, ConsoleConfig{
		Spec: func([]domain.Workspace) runtime.Spec { return r.rt.NewSpec() },
		Prepare: func(s runtime.Spec) (runtime.PreparedSpec, error) {
			s.Network.Internal = false // not hardened: Prepare refuses it
			return r.rt.Prepare(s)
		},
	})
	if _, err := c.Open(bg, nil); err == nil {
		t.Fatal("an unhardened console was opened")
	}
	if len(r.consoleEnvs()) != 0 {
		t.Error("an environment was left behind")
	}
	if inv, _ := r.rt.Adapter.Inventory(bg); len(inv.Networks)+len(inv.Sidecars)+len(inv.Volumes) != 0 {
		t.Errorf("resources left behind: %+v", inv)
	}
}
