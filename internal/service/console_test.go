package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// consoles gives the rig a console whose spec is the fake runtime's, with a
// home volume like the real one.
func (r *wsRig) consoles() *Consoles {
	return NewConsoles(r.svc, ConsoleConfig{
		Spec: func([]domain.Workspace) runtime.Spec {
			spec := r.rt.NewSpec()
			spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountVolume, Source: "whtmp-conformance-console-home", Target: "/home/whr"})
			if r.egress {
				spec.Egress = &runtime.Egress{Image: spec.Image, Proxy: r.rt.ProxyBinary, Allow: []string{"github.com"}}
			}
			return spec
		},
		Prepare: r.rt.Prepare,
		Dir:     func(w domain.Workspace) string { return "/workspaces/ws/" + w.Name },
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	if err != nil || len(inv.Volumes) != 1 || inv.Volumes[0] != "whtmp-conformance-console-home" || len(inv.Networks) != 0 {
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
	t.Parallel()
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

func TestAShellStartsInTheWorkspaceWithTheClientsTerminalAndIsAudited(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	r.egress = true
	r.create("docs-ws")
	c := r.consoles()
	if _, err := c.Shell(bg, ShellRequest{Actor: "werner"}); err == nil {
		t.Fatal("a shell without an open console")
	} else if !errors.As(err, new(*domain.ConflictError)) {
		t.Errorf("err = %v, want a conflict: open it first", err)
	}
	if _, err := c.Open(bg, []string{"docs-ws"}); err != nil {
		t.Fatal(err)
	}
	tm, err := c.Shell(bg, ShellRequest{Workspace: "docs-ws", Term: "xterm-kitty", Cols: 132, Rows: 43, Actor: "werner"})
	if err != nil {
		t.Fatal(err)
	}
	calls := r.fake.Terminals()
	if len(calls) != 1 {
		t.Fatalf("terminals = %+v", calls)
	}
	q := calls[0].Req
	if !reflect.DeepEqual(q.Cmd, []string{"/bin/zsh", "-l"}) || q.Dir != "/workspaces/ws/docs-ws" || q.Cols != 132 || q.Rows != 43 {
		t.Errorf("request = %+v", q)
	}
	for _, want := range []string{"HOME=/home/whr", "USER=whr", "TERM=xterm-kitty", "WHR_CONSOLE=1"} {
		if !slices.Contains(q.Env, want) {
			t.Errorf("env lacks %s: %v", want, q.Env)
		}
	}
	// The console reaches the network through the sidecar only: its address is passed on.
	var proxy bool
	for _, e := range q.Env {
		if strings.HasPrefix(e, "HTTPS_PROXY=http://") {
			proxy = true
		}
		for _, secret := range []string{"TOKEN", "KEY", "SECRET", "ANTHROPIC", "CLAUDE"} {
			if strings.Contains(strings.ToUpper(strings.SplitN(e, "=", 2)[0]), secret) {
				t.Errorf("the console shell has a secret-looking variable: %s", e)
			}
		}
	}
	if !proxy {
		t.Errorf("the shell has no proxy: %v", q.Env)
	}
	evs, err := r.store.EventsSince(bg, domain.SupervisorStream, 0, 10)
	if err != nil || len(evs) != 1 || evs[0].Kind != domain.EventConsole || evs[0].Tier != domain.TierAudit {
		t.Fatalf("audit = %+v, %v", evs, err)
	}
	var got domain.ConsoleOpened
	if err := json.Unmarshal(evs[0].Payload, &got); err != nil || got.Actor != "werner" || got.Workspace != "docs-ws" || !reflect.DeepEqual(got.ReadWrite, []string{"docs-ws"}) {
		t.Errorf("payload = %+v, %v", got, err)
	}
	_ = tm.Close()

	// A TERM that is not a terminal name is replaced, because it comes from the client.
	for _, bad := range []string{"$(touch /tmp/x)", "xterm; rm -rf /", "", "a b", strings.Repeat("x", 80), "-x"} {
		tm, err := c.Shell(bg, ShellRequest{Term: bad, Actor: "werner"})
		if err != nil {
			t.Fatal(err)
		}
		_ = tm.Close()
		env := r.fake.Terminals()[len(r.fake.Terminals())-1].Req.Env
		if !slices.Contains(env, "TERM=xterm-256color") {
			t.Errorf("TERM %q reached the shell: %v", bad, env)
		}
	}
	if _, err := c.Shell(bg, ShellRequest{Workspace: "nope", Actor: "werner"}); err == nil {
		t.Error("an unknown workspace was accepted")
	}
}

func TestTheConsoleServesAtMostEightShellsAtOnce(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	c := r.consoles()
	if _, err := c.Open(bg, nil); err != nil {
		t.Fatal(err)
	}
	var open []runtime.Terminal
	for range maxShells {
		tm, err := c.Shell(bg, ShellRequest{Actor: "werner"})
		if err != nil {
			t.Fatal(err)
		}
		open = append(open, tm)
	}
	if _, err := c.Shell(bg, ShellRequest{Actor: "werner"}); !errors.As(err, new(*domain.ConflictError)) {
		t.Fatalf("the ninth shell: %v", err)
	}
	_ = open[0].Close()
	_ = open[0].Close() // closing twice gives the place back once
	tm, err := c.Shell(bg, ShellRequest{Actor: "werner"})
	if err != nil {
		t.Fatalf("a place was given back: %v", err)
	}
	if _, err := c.Shell(bg, ShellRequest{Actor: "werner"}); err == nil {
		t.Error("closing a shell twice freed two places")
	}
	_ = tm.Close()
	for _, o := range open[1:] {
		_ = o.Close()
	}
}

// slowTerminal is a shell whose close takes a while, as the guest's kill grace does.
type slowTerminal struct {
	runtime.Terminal
	wait time.Duration
}

func (s slowTerminal) Close() error { time.Sleep(s.wait); return nil }

func TestShuttingDownClosesTheShellsInParallel(t *testing.T) {
	t.Parallel()
	r := newWsRig(t)
	c := r.consoles()
	const n, each = 6, 300 * time.Millisecond
	for range n {
		ct := &countedTerminal{Terminal: slowTerminal{wait: each}, release: func() {}}
		c.mu.Lock()
		if c.open == nil {
			c.open = map[*countedTerminal]struct{}{}
		}
		c.open[ct] = struct{}{}
		c.mu.Unlock()
	}
	start := time.Now()
	c.CloseShells()
	if took := time.Since(start); took >= n*each/2 {
		t.Errorf("closing %d shells took %s: they were closed one after another", n, took)
	}
}
