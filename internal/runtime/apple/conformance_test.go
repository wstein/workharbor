//go:build applecontainer

package apple

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/runtime/runtimetest"
)

// Run on a Mac with Apple Container: go test -tags applecontainer ./internal/runtime/apple
// The fedora image must be present (`container image pull fedora`). WHR_TEST_IMAGE runs the suite
// on another base image instead (design D43: Fedora and Ubuntu LTS), which needs sh, sleep and cat.
func newHarness(t *testing.T) runtimetest.Harness {
	t.Helper()
	if _, err := exec.LookPath("container"); err != nil {
		t.Skip("the container CLI is not installed")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A directory the tools sit in. It is a bind mount, so it must be under a
	// root the spec may use; the home is the temp dir.
	bin := filepath.Join(home, "bin")
	for _, dir := range []string{bin, filepath.Join(home, "src", "app"), filepath.Join(home, ".ssh"), filepath.Join(home, "cache", "repo.git", "objects")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"whr-proxy", "whr-shim"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(bin, tool), "../../../cmd/"+tool) //nolint:gosec,noctx // a test building the guest tools
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", tool, err, out)
		}
	}
	owner := fmt.Sprintf("wh-conf-%d", time.Now().UnixNano()%1_000_000)
	a, err := New(owner, WithShim("/tools/whr-shim"))
	if err != nil {
		t.Fatal(err)
	}
	image := os.Getenv("WHR_TEST_IMAGE")
	if image == "" {
		image = "fedora"
	}
	nets := 0
	t.Cleanup(func() { cleanOwner(t, a) })
	return runtimetest.Harness{
		Adapter: a, Owner: owner,
		Prepare: func(spec runtime.Spec) (runtime.PreparedSpec, error) {
			return runtime.Prepare(runtime.PrepareOptions{
				FS: runtime.OSFS{}, Home: home, CacheRoots: []string{filepath.Join(home, "cache")},
				Owns: func(v string) bool { return strings.HasPrefix(v, "wh-conformance-") },
			}, spec)
		},
		ProxyBinary:  filepath.Join(bin, "whr-proxy"),
		CacheObjects: filepath.Join(home, "cache", "repo.git", "objects"),
		NewSpec: func() runtime.Spec {
			nets++
			return runtime.Spec{
				Image: image, Owner: owner, CPUs: 2, MemoryMB: 1024, DiskMB: 2048,
				Network: runtime.Network{Name: fmt.Sprintf("%s-net-%d", owner, nets), Internal: true},
				User:    "1000:1000", ReadOnlyRoot: true, CapDrop: []string{"ALL"}, Init: true, Tmpfs: []string{"/tmp"},
				Mounts: []runtime.Mount{{Kind: runtime.MountBind, Source: bin, Target: "/tools", ReadOnly: true}},
			}
		},
		AllowedMount:    filepath.Join(home, "src", "app"),
		ForbiddenMounts: []string{home, filepath.Join(home, ".ssh"), filepath.Dir(home), "/etc"},
		Commands: runtimetest.Commands{
			Echo:   func(s string) []string { return []string{"echo", s} },
			Stderr: func(s string) []string { return []string{"sh", "-c", "echo " + s + " >&2"} },
			Exit:   func(n int) []string { return []string{"sh", "-c", fmt.Sprintf("exit %d", n)} },
			Sleep:  []string{"sleep", "600"},
			Cat:    []string{"cat"},
			Alive:  []string{"sh", "-c", "for p in /proc/[0-9]*; do [ \"$(cat $p/comm 2>/dev/null)\" = sleep ] && [ \"$(tr '\\0' ' ' < $p/cmdline)\" = 'sleep 600 ' ] && exit 0; done; exit 1"},
		},
	}
}

// cleanOwner removes whatever a failed check left behind, by exact name.
func cleanOwner(t *testing.T, a *Adapter) {
	t.Helper()
	bg := context.Background()
	all, _ := a.containers(bg)
	for _, c := range all {
		if c.Configuration.Labels[runtime.OwnerLabel] == a.owner {
			_, _, _ = a.run(bg, nil, "stop", c.Configuration.ID)
			_, _, _ = a.run(bg, nil, "delete", c.Configuration.ID)
		}
	}
	inv, _ := a.Inventory(bg)
	for _, n := range inv.Networks {
		_, _, _ = a.run(bg, nil, "network", "delete", n)
	}
	for _, v := range inv.Volumes {
		_, _, _ = a.run(bg, nil, "volume", "delete", v)
	}
}

func TestAppleContainerPassesTheSuite(t *testing.T) {
	runtimetest.Run(t, newHarness)
}

// TestEgressThroughTheSidecar is the egress conformance of design §7.2: the
// agent's only way out is the proxy. A direct connection fails, an allowlisted
// host answers through the proxy, a raw IP is refused (403) and the guest
// resolves no names itself.
func TestEgressThroughTheSidecar(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	spec := h.NewSpec()
	spec.Egress = &runtime.Egress{Image: "fedora", Proxy: h.ProxyBinary, Allow: []string{"example.com"}}
	id, err := h.Adapter.Provision(ctx, mustPrepare(t, h, spec))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.Adapter.Stop(context.Background(), id)
		_ = h.Adapter.Delete(context.Background(), id)
	})
	if err := h.Adapter.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	info, err := h.Adapter.Inspect(ctx, id)
	if err != nil || info.Proxy == "" {
		t.Fatalf("no proxy address: %+v, %v", info, err)
	}
	run := func(cmd ...string) (string, int) {
		t.Helper()
		st, err := h.Adapter.Exec(ctx, id, runtime.ExecRequest{Cmd: []string{"sh", "-c", strings.Join(cmd, " ")}})
		if err != nil {
			t.Fatal(err)
		}
		out, errOut, code, _ := runtime.Collect(st)
		return string(out) + string(errOut), code
	}
	// http_connect is the proxy's answer to CONNECT; http_code stays 000 when it refuses.
	curl := "curl -s -o /dev/null -w '%{http_connect}' --max-time 15"
	if out, code := run(curl, "https://example.com"); code == 0 {
		t.Errorf("a direct connection worked: %s", out)
	}
	if out, code := run("getent hosts example.com"); code == 0 {
		t.Errorf("the guest resolved a name itself: %s", out)
	}
	if out, _ := run(curl, "-x", info.Proxy, "https://example.com"); out != "200" {
		t.Errorf("allowlisted host through the proxy = %q, want 200", out)
	}
	if out, _ := run(curl, "-x", info.Proxy, "https://1.1.1.1"); !strings.Contains(out, "403") {
		t.Errorf("a raw IP through the proxy = %q, want 403", out)
	}
	if out, _ := run(curl, "-x", info.Proxy, "https://github.com"); !strings.Contains(out, "403") {
		t.Errorf("a host off the allowlist = %q, want 403", out)
	}
}

func mustPrepare(t *testing.T, h runtimetest.Harness, spec runtime.Spec) runtime.PreparedSpec {
	t.Helper()
	p, err := h.Prepare(spec)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
