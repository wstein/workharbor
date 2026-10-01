package runtimetest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Commands tells the suite how to run a few simple commands on a backend.
type Commands struct {
	Echo   func(text string) []string // writes text and a newline to stdout
	Stderr func(text string) []string // writes text and a newline to stderr
	Exit   func(code int) []string    // exits with code
	Sleep  []string                   // runs until cancelled
}

// Harness is what a backend gives the suite.
type Harness struct {
	Adapter runtime.Adapter
	Owner   string // the owner the adapter acts for
	// NewSpec returns a valid, hardened spec for this backend with Owner set.
	NewSpec func() runtime.Spec
	// AllowedMount is a host directory an environment may be given;
	// ForbiddenMounts are host paths that must be rejected.
	AllowedMount    string
	ForbiddenMounts []string
	Commands        Commands
	// Restart simulates a runtime service restart (every environment ends up
	// stopped). Optional.
	Restart func(ctx context.Context) error
	// NewForeign creates an environment that another tool owns and returns its
	// ID. Optional.
	NewForeign func(ctx context.Context) (string, error)
}

// ErrSkip is returned by a check that needs an optional part of the harness.
var ErrSkip = errors.New("skipped: the harness does not provide what this check needs")

// Check is one conformance requirement. It returns nil when the adapter meets
// it, so tests can also show that a defective adapter fails it.
type Check struct {
	Name string
	Fn   func(ctx context.Context, h Harness) error
}

// Checks returns the conformance requirements of design §5.1.
func Checks() []Check {
	return []Check{
		{"capabilities are reported", checkCapabilities},
		{"lifecycle is typed, idempotent and retry safe", checkLifecycle},
		{"delete needs a stopped environment", checkDeleteRunning},
		{"an unhardened spec is rejected and creates nothing", checkUnhardenedSpecs},
		{"forbidden mounts are rejected and create nothing", checkForbiddenMounts},
		{"list returns only the owner's environments", checkListByOwner},
		{"foreign environments are off limits", checkForeign},
		{"exec streams output, errors and the exit code", checkExec},
		{"delete is by exact ID only", checkDeleteExact},
		{"a service restart leaves every environment stopped", checkRestart},
	}
}

// Run runs the suite against a backend. newHarness is called once per check
// with a fresh backend.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()
	for _, c := range Checks() {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Fn(ctx, newHarness(t)); errors.Is(err, ErrSkip) {
				t.Skip(err)
			} else if err != nil {
				t.Error(err)
			}
		})
	}
}

// Failures runs every check and returns the ones that failed, by name. A
// skipped check is not a failure.
func Failures(ctx context.Context, newHarness func() Harness) map[string]error {
	failed := map[string]error{}
	for _, c := range Checks() {
		if err := c.Fn(ctx, newHarness()); err != nil && !errors.Is(err, ErrSkip) {
			failed[c.Name] = err
		}
	}
	return failed
}

func checkCapabilities(_ context.Context, h Harness) error {
	c := h.Adapter.Capabilities()
	if c.Isolation == "" || c.Arch == "" {
		return fmt.Errorf("capabilities must say the isolation boundary and the CPU architecture: %+v", c)
	}
	if h.Adapter.Name() == "" {
		return errors.New("an adapter must have a name")
	}
	return nil
}

func checkLifecycle(ctx context.Context, h Harness) error {
	a := h.Adapter
	spec := h.NewSpec()
	id, err := a.Provision(ctx, spec)
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	info, err := a.Inspect(ctx, id)
	if err != nil {
		return fmt.Errorf("inspect after provision: %w", err)
	}
	if info.ID != id || info.State != domain.EnvStopped || info.Addr != "" || info.Owner != h.Owner || info.Labels[runtime.OwnerLabel] != h.Owner {
		return fmt.Errorf("a provisioned environment must be stopped, labelled with its owner and have no address: %+v", info)
	}

	for i := range 2 { // start twice: idempotent
		if err := a.Start(ctx, id); err != nil {
			return fmt.Errorf("start #%d: %w", i+1, err)
		}
	}
	if info, err = a.Inspect(ctx, id); err != nil || info.State != domain.EnvRunning || info.Addr == "" {
		return fmt.Errorf("a running environment must report its address: %+v (%s)", info, show(err))
	}
	for i := range 2 {
		if err := a.Stop(ctx, id); err != nil {
			return fmt.Errorf("stop #%d: %w", i+1, err)
		}
	}
	if info, err = a.Inspect(ctx, id); err != nil || info.State != domain.EnvStopped || info.Addr != "" {
		return fmt.Errorf("a stopped environment has no address: %+v (%s)", info, show(err))
	}
	if err := a.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if _, err := a.Inspect(ctx, id); !errors.Is(err, runtime.ErrNotFound) {
		return fmt.Errorf("inspect after delete = %s, want ErrNotFound", show(err))
	}
	if err := a.Delete(ctx, id); err != nil {
		return fmt.Errorf("a second delete must succeed so a retry is safe: %w", err)
	}
	if err := a.Start(ctx, id); !errors.Is(err, runtime.ErrNotFound) {
		return fmt.Errorf("start of a deleted environment = %s, want ErrNotFound", show(err))
	}
	return nil
}

func checkDeleteRunning(ctx context.Context, h Harness) error {
	a := h.Adapter
	id, err := a.Provision(ctx, h.NewSpec())
	if err != nil {
		return err
	}
	if err := a.Start(ctx, id); err != nil {
		return err
	}
	if err := a.Delete(ctx, id); !errors.Is(err, runtime.ErrRunning) {
		return fmt.Errorf("delete of a running environment = %s, want ErrRunning", show(err))
	}
	if _, err := a.Inspect(ctx, id); err != nil {
		return fmt.Errorf("a refused delete removed the environment: %w", err)
	}
	return nil
}

// show describes an error that may be nil, for a failure message.
func show(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}

func count(ctx context.Context, h Harness) (int, error) {
	infos, err := h.Adapter.List(ctx, h.Owner)
	return len(infos), err
}

func checkUnhardenedSpecs(ctx context.Context, h Harness) error {
	mutations := map[string]func(*runtime.Spec){
		"root user":       func(s *runtime.Spec) { s.User = "0" },
		"no user":         func(s *runtime.Spec) { s.User = "" },
		"no init":         func(s *runtime.Spec) { s.Init = false },
		"no cap-drop":     func(s *runtime.Spec) { s.CapDrop = nil },
		"no owner":        func(s *runtime.Spec) { s.Owner = "" },
		"another owner":   func(s *runtime.Spec) { s.Owner = "someone-else" },
		"no disk quota":   func(s *runtime.Spec) { s.DiskMB = 0 },
		"relative target": func(s *runtime.Spec) { s.Tmpfs = []string{"tmp"} },
	}
	before, err := count(ctx, h)
	if err != nil {
		return err
	}
	for name, mutate := range mutations {
		spec := h.NewSpec()
		mutate(&spec)
		id, err := h.Adapter.Provision(ctx, spec)
		if err == nil {
			return fmt.Errorf("%s: an unhardened spec was provisioned as %s", name, id)
		}
		if !errors.Is(err, runtime.ErrInvalidSpec) {
			return fmt.Errorf("%s: error = %s, want ErrInvalidSpec", name, show(err))
		}
	}
	if after, err := count(ctx, h); err != nil || after != before {
		return fmt.Errorf("rejected specs created environments: %d before, %d after (%s)", before, after, show(err))
	}
	return nil
}

func checkForbiddenMounts(ctx context.Context, h Harness) error {
	before, err := count(ctx, h)
	if err != nil {
		return err
	}
	for _, path := range h.ForbiddenMounts {
		spec := h.NewSpec()
		spec.Mounts = append(spec.Mounts, runtime.Mount{Kind: runtime.MountBind, Source: path, Target: "/mnt/forbidden", ReadOnly: true})
		if _, err := h.Adapter.Provision(ctx, spec); !errors.Is(err, runtime.ErrForbiddenMount) {
			return fmt.Errorf("bind mount of %s: error = %s, want ErrForbiddenMount (read-only makes no difference)", path, show(err))
		}
	}
	if after, err := count(ctx, h); err != nil || after != before {
		return fmt.Errorf("rejected mounts created environments: %d before, %d after (%s)", before, after, show(err))
	}

	spec := h.NewSpec()
	spec.Mounts = append(spec.Mounts,
		runtime.Mount{Kind: runtime.MountBind, Source: h.AllowedMount, Target: "/work"},
		runtime.Mount{Kind: runtime.MountVolume, Source: "wh-conformance-cache", Target: "/cache"})
	if _, err := h.Adapter.Provision(ctx, spec); err != nil {
		return fmt.Errorf("an allowed bind mount and a volume were rejected: %w", err)
	}
	return nil
}

func checkListByOwner(ctx context.Context, h Harness) error {
	a := h.Adapter
	mine := map[string]bool{}
	for range 2 {
		spec := h.NewSpec()
		spec.Labels = map[string]string{"workharbor.task": "t-1"}
		id, err := a.Provision(ctx, spec)
		if err != nil {
			return err
		}
		mine[id] = true
	}
	var foreign string
	if h.NewForeign != nil {
		var err error
		if foreign, err = h.NewForeign(ctx); err != nil {
			return err
		}
	}

	infos, err := a.List(ctx, h.Owner)
	if err != nil {
		return err
	}
	if len(infos) != len(mine) {
		return fmt.Errorf("List(%q) returned %d environments, want exactly the %d provisioned", h.Owner, len(infos), len(mine))
	}
	for _, info := range infos {
		if !mine[info.ID] || info.ID == foreign {
			return fmt.Errorf("List(%q) returned %s, which is not this owner's", h.Owner, info.ID)
		}
		if info.Owner != h.Owner || info.Labels["workharbor.task"] != "t-1" {
			return fmt.Errorf("listed environment lost its labels: %+v", info)
		}
	}
	other, err := a.List(ctx, "nobody-owns-this")
	if err != nil {
		return err
	}
	for _, info := range other {
		if mine[info.ID] {
			return fmt.Errorf("List(\"nobody-owns-this\") returned %s", info.ID)
		}
	}
	return nil
}

func checkForeign(ctx context.Context, h Harness) error {
	if h.NewForeign == nil {
		return ErrSkip
	}
	id, err := h.NewForeign(ctx)
	if err != nil {
		return err
	}
	a := h.Adapter
	calls := map[string]error{
		"start":  a.Start(ctx, id),
		"stop":   a.Stop(ctx, id),
		"delete": a.Delete(ctx, id),
	}
	_, calls["inspect"] = a.Inspect(ctx, id)
	_, calls["exec"] = a.Exec(ctx, id, runtime.ExecRequest{Cmd: h.Commands.Echo("x")})
	_, calls["logs"] = a.Logs(ctx, id)
	for name, err := range calls {
		if !errors.Is(err, runtime.ErrNotOwned) {
			return fmt.Errorf("%s of an environment another tool owns = %s, want ErrNotOwned", name, show(err))
		}
	}
	return nil
}

func checkExec(ctx context.Context, h Harness) error {
	a := h.Adapter
	id, err := a.Provision(ctx, h.NewSpec())
	if err != nil {
		return err
	}
	if _, err := a.Exec(ctx, id, runtime.ExecRequest{Cmd: h.Commands.Echo("x")}); !errors.Is(err, runtime.ErrNotRunning) {
		return fmt.Errorf("exec in a stopped environment = %s, want ErrNotRunning", show(err))
	}
	if err := a.Start(ctx, id); err != nil {
		return err
	}

	st, err := a.Exec(ctx, id, runtime.ExecRequest{Cmd: h.Commands.Echo("hello")})
	if err != nil {
		return err
	}
	out, errOut, code, err := runtime.Collect(st)
	if err != nil || code != 0 || strings.TrimSpace(string(out)) != "hello" || len(errOut) != 0 {
		return fmt.Errorf("echo: stdout %q stderr %q exit %d err %s", out, errOut, code, show(err))
	}
	if st, err = a.Exec(ctx, id, runtime.ExecRequest{Cmd: h.Commands.Stderr("oops")}); err != nil {
		return err
	}
	if out, errOut, _, _ = runtime.Collect(st); strings.TrimSpace(string(errOut)) != "oops" || len(out) != 0 {
		return fmt.Errorf("stderr must be a separate stream: stdout %q stderr %q", out, errOut)
	}
	if st, err = a.Exec(ctx, id, runtime.ExecRequest{Cmd: h.Commands.Exit(7)}); err != nil {
		return err
	}
	if _, _, code, _ = runtime.Collect(st); code != 7 {
		return fmt.Errorf("exit code = %d, want 7", code)
	}

	cctx, cancel := context.WithCancel(ctx)
	if st, err = a.Exec(cctx, id, runtime.ExecRequest{Cmd: h.Commands.Sleep}); err != nil {
		cancel()
		return err
	}
	cancel()
	done := make(chan error, 1)
	go func() { _, err := st.Wait(); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			return errors.New("a cancelled exec must report an error")
		}
	case <-time.After(10 * time.Second):
		return errors.New("cancelling the context did not end a running exec")
	}
	return nil
}

func checkDeleteExact(ctx context.Context, h Harness) error {
	a := h.Adapter
	var ids []string
	for range 2 {
		id, err := a.Provision(ctx, h.NewSpec())
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, pattern := range []string{"*", "--all", "", ids[0][:len(ids[0])-1], strings.ToUpper(ids[0])} {
		_ = a.Delete(ctx, pattern) // the result may be success or an error; what matters is the effect
	}
	for _, id := range ids {
		if _, err := a.Inspect(ctx, id); err != nil {
			return fmt.Errorf("a delete by pattern removed %s: %w", id, err)
		}
	}
	return nil
}

func checkRestart(ctx context.Context, h Harness) error {
	if h.Restart == nil {
		return ErrSkip
	}
	a := h.Adapter
	var ids []string
	for range 2 {
		id, err := a.Provision(ctx, h.NewSpec())
		if err != nil {
			return err
		}
		if err := a.Start(ctx, id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := h.Restart(ctx); err != nil {
		return err
	}
	for _, id := range ids {
		info, err := a.Inspect(ctx, id)
		if err != nil || info.State != domain.EnvStopped || info.Addr != "" {
			return fmt.Errorf("after a service restart %s must be stopped with no address: %+v, %s", id, info, show(err))
		}
	}
	if infos, err := a.List(ctx, h.Owner); err != nil || len(infos) != len(ids) {
		return fmt.Errorf("a restart must not forget environments: %d listed, %d expected (%s)", len(infos), len(ids), show(err))
	}
	if err := a.Start(ctx, ids[0]); err != nil {
		return fmt.Errorf("start after a restart: %w", err)
	}
	st, err := a.Exec(ctx, ids[0], runtime.ExecRequest{Cmd: h.Commands.Echo("back")})
	if err != nil {
		return fmt.Errorf("exec after a restart: %w", err)
	}
	if out, _, _, _ := runtime.Collect(st); strings.TrimSpace(string(out)) != "back" {
		return fmt.Errorf("exec after a restart wrote %q", out)
	}
	return nil
}

// NewFakeHarness returns a harness for the in-memory fake, the first
// backend to pass the suite.
func NewFakeHarness(t *testing.T) Harness {
	t.Helper()
	return fakeHarness(t, Defects{})
}

func fakeHarness(t *testing.T, defects Defects) Harness {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(home, "src", "app")
	for _, dir := range []string{project, filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	const owner = "wh-conformance"
	f := NewFake(owner, home, runtime.OSFS{})
	f.Defects = defects
	return Harness{
		Adapter: f,
		Owner:   owner,
		NewSpec: func() runtime.Spec {
			return runtime.Spec{
				Image: "debian:stable-slim", Owner: owner, CPUs: 2, MemoryMB: 1024, DiskMB: 10240,
				Network: runtime.Network{Name: "wh-net", Internal: true},
				User:    "1000:1000", ReadOnlyRoot: true, CapDrop: []string{"ALL"}, Init: true, Tmpfs: []string{"/tmp"},
			}
		},
		AllowedMount:    project,
		ForbiddenMounts: []string{home, filepath.Join(home, ".ssh"), filepath.Dir(home), "/etc"},
		Commands: Commands{
			Echo:   func(s string) []string { return []string{"echo", s} },
			Stderr: func(s string) []string { return []string{"err", s} },
			Exit:   func(n int) []string { return []string{"exit", fmt.Sprint(n)} },
			Sleep:  []string{"sleep"},
		},
		Restart:    func(context.Context) error { f.Restart(); return nil },
		NewForeign: func(context.Context) (string, error) { return f.AddForeign("another-tool"), nil },
	}
}
