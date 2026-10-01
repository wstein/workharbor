package apple

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/runtime"
)

// recorder is a stand-in for the container CLI: it records the commands and
// answers the list commands with an empty JSON array.
type recorder struct{ calls [][]string }

func (r *recorder) run(_ context.Context, _ io.Reader, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, args)
	if slices.Contains(args, "list") {
		return []byte("[]"), nil, nil
	}
	return nil, nil, nil
}

func prepared(t *testing.T, spec runtime.Spec) runtime.PreparedSpec {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prep, err := runtime.Prepare(runtime.PrepareOptions{
		FS: runtime.OSFS{}, Home: home,
		Owns: func(v string) bool { return strings.HasPrefix(v, "wh-") },
	}, spec)
	if err != nil {
		t.Fatal(err)
	}
	return prep
}

func baseSpec() runtime.Spec {
	return runtime.Spec{
		Image: "fedora", Owner: "o1", CPUs: 2, MemoryMB: 1024, DiskMB: 2048,
		Network: runtime.Network{Name: "wh-net-1", Internal: true},
		User:    "1000:1000", ReadOnlyRoot: true, CapDrop: []string{"ALL"}, Init: true, Tmpfs: []string{"/tmp"},
	}
}

func TestProvisionIsHardened(t *testing.T) {
	r := &recorder{}
	a := &Adapter{owner: "o1", run: r.run}
	spec := baseSpec()
	spec.Mounts = []runtime.Mount{{Kind: runtime.MountVolume, Source: "wh-home", Target: "/home/agent"}}
	if _, err := a.Provision(context.Background(), prepared(t, spec)); err != nil {
		t.Fatal(err)
	}
	var create []string
	for _, c := range r.calls {
		joined := strings.Join(c, " ")
		for _, bad := range []string{"--ssh", "--rm", "--privileged", "--publish", "-p "} {
			if strings.Contains(joined, bad) {
				t.Errorf("%q passed %s", joined, bad)
			}
		}
		if c[0] == "create" {
			create = c
		}
	}
	for _, want := range []string{"--read-only", "--init", "--internal"} {
		if want == "--internal" {
			if !slices.ContainsFunc(r.calls, func(c []string) bool { return c[0] == "network" && slices.Contains(c, want) }) {
				t.Error("the network was not created --internal")
			}
			continue
		}
		if !slices.Contains(create, want) {
			t.Errorf("create lacks %s: %v", want, create)
		}
	}
	if i := slices.Index(create, "--cap-drop"); i < 0 || create[i+1] != "ALL" {
		t.Errorf("create does not drop all capabilities: %v", create)
	}
	if i := slices.Index(create, "--network"); i < 0 || create[i+1] != "wh-net-1" || slices.Contains(create, "default") {
		t.Errorf("the environment must be on its internal network only: %v", create)
	}
}

func TestProvisionRefusesWhatWasNotPrepared(t *testing.T) {
	r := &recorder{}
	a := &Adapter{owner: "o1", run: r.run}
	if _, err := a.Provision(context.Background(), runtime.PreparedSpec{}); !errors.Is(err, runtime.ErrNotPrepared) {
		t.Fatalf("err = %v, want ErrNotPrepared", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("the runtime was called: %v", r.calls)
	}
}

func TestSidecarSitsOnBothNetworksAndHoldsTheProxyReadOnly(t *testing.T) {
	r := &recorder{}
	a := &Adapter{owner: "o1", run: r.run}
	home := t.TempDir()
	proxy := filepath.Join(home, "whr-proxy")
	spec := baseSpec()
	spec.Egress = &runtime.Egress{Image: "fedora", Proxy: proxy, Allow: []string{"api.anthropic.com"}}
	prep, err := runtime.Prepare(runtime.PrepareOptions{FS: runtime.OSFS{}, Home: filepath.Join(home, "x")}, spec)
	if err != nil {
		t.Skipf("prepare needs a real proxy path: %v", err)
	}
	if _, err := a.Provision(context.Background(), prep); err != nil {
		t.Fatal(err)
	}
	var side []string
	for _, c := range r.calls {
		if c[0] == "create" && slices.Contains(c, "workharbor.role=sidecar") {
			side = c
		}
	}
	if side == nil {
		t.Fatal("no sidecar was created")
	}
	joined := strings.Join(side, " ")
	if !strings.Contains(joined, "--network default") || !strings.Contains(joined, "--network wh-net-1") {
		t.Errorf("the sidecar must be on the default and the internal network: %s", joined)
	}
	if !strings.Contains(joined, ":/whr-proxy:ro") {
		t.Errorf("the proxy must be mounted read-only: %s", joined)
	}
}

func TestDeleteUsesExactNames(t *testing.T) {
	r := &recorder{}
	a := &Adapter{owner: "o1", run: r.run}
	if err := a.Delete(context.Background(), "whr-ab"); err != nil {
		t.Fatal(err) // not found: a retried delete is fine
	}
	for _, c := range r.calls {
		if slices.Contains(c, "--all") && c[0] != "list" {
			t.Errorf("a bulk command was run: %v", c)
		}
	}
}

func TestTheAdaptersOwnLabelsCannotBeReplaced(t *testing.T) {
	a := &Adapter{owner: "o1"}
	args := strings.Join(a.labelArgs(roleEnv, "whr-1", map[string]string{
		runtime.OwnerLabel: "o2", roleLabel: "sidecar", envLabel: "whr-2", "whr.task": "t1",
	}), " ")
	for _, want := range []string{"workharbor.owner=o1", "workharbor.role=environment", "workharbor.env=whr-1", "whr.task=t1"} {
		if !strings.Contains(args, want) {
			t.Errorf("labels %q lack %s", args, want)
		}
	}
	spec := baseSpec()
	spec.Labels = map[string]string{netLabel: "someone-elses-net"}
	create := strings.Join(a.createArgs("whr-1", spec), " ")
	if strings.Contains(create, "someone-elses-net") {
		t.Errorf("a spec label replaced the network label: %s", create)
	}
}

func TestTheSidecarHasLimits(t *testing.T) {
	a := &Adapter{owner: "o1"}
	spec := baseSpec()
	spec.Egress = &runtime.Egress{Image: "fedora", Proxy: "/opt/whr-proxy", Allow: []string{"api.anthropic.com"}}
	args := a.sidecarArgs("whr-1", spec)
	if i := slices.Index(args, "--cpus"); i < 0 || args[i+1] != "1" {
		t.Errorf("the sidecar has no CPU limit: %v", args)
	}
	if i := slices.Index(args, "--memory"); i < 0 || args[i+1] != "256M" {
		t.Errorf("the sidecar has no memory limit: %v", args)
	}
}
