package runtimetest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// newFake returns a fake and a function that makes a fresh prepared spec for
// it each time, since a network is never shared between environments.
func newFake(t *testing.T) (*Fake, func() runtime.PreparedSpec) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := NewFake("wh-test", home, runtime.OSFS{})
	n := 0
	return f, func() runtime.PreparedSpec {
		n++
		prep, err := runtime.Prepare(runtime.PrepareOptions{FS: runtime.OSFS{}, Home: home}, runtime.Spec{
			Image: "debian", Owner: "wh-test", CPUs: 1, MemoryMB: 512, DiskMB: 1024,
			Network: runtime.Network{Name: fmt.Sprintf("wh-test-net-%d", n), Internal: true}, ReadOnlyRoot: true,
			User: "1000:1000", Init: true, CapDrop: []string{"ALL"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return prep
	}
}

func TestRestartStopsEveryEnvironmentAndNothingComesBack(t *testing.T) {
	ctx := context.Background()
	f, spec := newFake(t)
	var ids []string
	for range 3 {
		id, err := f.Provision(ctx, spec())
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Start(ctx, id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	f.Restart()
	for _, id := range ids {
		info, err := f.Inspect(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if info.State != domain.EnvStopped || info.Addr != "" {
			t.Errorf("%s after a restart: state %s, addr %q; want stopped and no address", id, info.State, info.Addr)
		}
	}
	time.Sleep(10 * time.Millisecond)
	if info, _ := f.Inspect(ctx, ids[0]); info.State != domain.EnvStopped {
		t.Error("an environment came back by itself after a restart")
	}
	if err := f.Start(ctx, ids[0]); err != nil {
		t.Errorf("a stopped environment must start again: %v", err)
	}
}

func TestEveryStartGivesANewAddress(t *testing.T) {
	ctx := context.Background()
	f, spec := newFake(t)
	id, _ := f.Provision(ctx, spec())
	_ = f.Start(ctx, id)
	first, _ := f.Inspect(ctx, id)
	_ = f.Stop(ctx, id)
	_ = f.Start(ctx, id)
	second, _ := f.Inspect(ctx, id)
	if first.Addr == "" || second.Addr == "" || first.Addr == second.Addr {
		t.Errorf("addresses %q then %q; want two different ones", first.Addr, second.Addr)
	}
}

func TestForeignEnvironmentsAreOffLimits(t *testing.T) {
	ctx := context.Background()
	f, _ := newFake(t)
	foreign := f.AddForeign("another-tool")
	for name, err := range map[string]error{
		"start":   f.Start(ctx, foreign),
		"stop":    f.Stop(ctx, foreign),
		"delete":  f.Delete(ctx, foreign),
		"inspect": func() error { _, e := f.Inspect(ctx, foreign); return e }(),
	} {
		if !errors.Is(err, runtime.ErrNotOwned) {
			t.Errorf("%s of a foreign environment = %v, want ErrNotOwned", name, err)
		}
	}
	if f.Count() != 1 {
		t.Errorf("the foreign environment was removed: %d left", f.Count())
	}
}

func TestExecCommands(t *testing.T) {
	ctx := context.Background()
	f, spec := newFake(t)
	id, _ := f.Provision(ctx, spec())
	if _, err := f.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"echo", "hi"}}); !errors.Is(err, runtime.ErrNotRunning) {
		t.Fatalf("exec in a stopped environment = %v, want ErrNotRunning", err)
	}
	_ = f.Start(ctx, id)

	st, err := f.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"echo", "hello", "world"}})
	if err != nil {
		t.Fatal(err)
	}
	out, _, code, err := runtime.Collect(st)
	if err != nil || code != 0 || string(out) != "hello world\n" {
		t.Errorf("echo = %q, %d, %v", out, code, err)
	}

	st, _ = f.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"err", "bad"}})
	out, errOut, _, _ := runtime.Collect(st)
	if len(out) != 0 || string(errOut) != "bad\n" {
		t.Errorf("err wrote stdout %q and stderr %q", out, errOut)
	}

	st, _ = f.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"exit", "7"}})
	if _, _, code, _ := runtime.Collect(st); code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
	st, _ = f.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"nope"}})
	if _, _, code, _ := runtime.Collect(st); code != 127 {
		t.Errorf("unknown command exit code = %d, want 127", code)
	}

	cctx, cancel := context.WithCancel(ctx)
	st, _ = f.Exec(cctx, id, runtime.ExecRequest{Cmd: []string{"sleep"}})
	cancel()
	if _, err := st.Wait(); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled exec = %v, want context.Canceled", err)
	}
}

func TestDeleteNeedsAStoppedEnvironmentAndIsRetrySafe(t *testing.T) {
	ctx := context.Background()
	f, spec := newFake(t)
	id, _ := f.Provision(ctx, spec())
	_ = f.Start(ctx, id)
	if err := f.Delete(ctx, id); !errors.Is(err, runtime.ErrRunning) {
		t.Fatalf("delete of a running environment = %v, want ErrRunning", err)
	}
	_ = f.Stop(ctx, id)
	if err := f.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(ctx, id); err != nil {
		t.Errorf("a second delete must succeed so a retry is safe: %v", err)
	}
	if _, err := f.Inspect(ctx, id); !errors.Is(err, runtime.ErrNotFound) {
		t.Errorf("inspect after delete = %v, want ErrNotFound", err)
	}
}
