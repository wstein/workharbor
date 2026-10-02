package apple

import (
	"context"
	"errors"
	"io"
	"os"
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

func testHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return home
}

// preparedIn prepares spec with home as the user's home; bind sources below it
// pass the mount checks.
func preparedIn(t *testing.T, home string, spec runtime.Spec) runtime.PreparedSpec {
	t.Helper()
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
	a := &Adapter{owner: "o1", run: r.run, shim: "/tools/bin/whr-shim"}
	home := testHome(t)
	store := filepath.Join(home, "store")
	if err := os.Mkdir(store, 0o750); err != nil {
		t.Fatal(err)
	}
	spec := baseSpec()
	spec.Mounts = []runtime.Mount{
		{Kind: runtime.MountVolume, Source: "wh-home", Target: "/home/agent"},
		{Kind: runtime.MountBind, Source: store, Target: "/tools", ReadOnly: true},
	}
	if _, err := a.Provision(context.Background(), preparedIn(t, home, spec)); err != nil {
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

func TestTheSidecarGetsTheHostPrefixes(t *testing.T) {
	a := &Adapter{owner: "o1"}
	spec := baseSpec()
	spec.Egress = &runtime.Egress{Image: "fedora", Proxy: "/opt/whr-proxy", Allow: []string{"api.anthropic.com"}}
	if args := a.sidecarArgs("whr-1", spec); slices.Contains(args, "-deny-prefixes") {
		t.Errorf("a flag without prefixes: %v", args)
	}
	spec.Egress.DenyPrefixes = []string{"2001:db8:1::/64", "2001:db8:2::/56"}
	args := a.sidecarArgs("whr-1", spec)
	if i := slices.Index(args, "-deny-prefixes"); i < 0 || args[i+1] != "2001:db8:1::/64,2001:db8:2::/56" {
		t.Errorf("the sidecar does not carry the prefixes: %v", args)
	}
}

func TestCreateArgsPassTheEnvironmentSorted(t *testing.T) {
	a := &Adapter{owner: "o1"}
	spec := baseSpec()
	spec.Env = map[string]string{"ZED": "1", "GOFLAGS": "-mod=mod -x"}
	args := a.createArgs("whr-1", spec)
	i := slices.Index(args, "GOFLAGS=-mod=mod -x")
	if i < 1 || args[i-1] != "-e" || args[i+1] != "-e" || args[i+2] != "ZED=1" {
		t.Errorf("variables are not passed as sorted -e pairs: %v", args)
	}
	// Every flag comes before the image, so a value can never become one.
	if slices.Index(args, spec.Image) < slices.Index(args, "ZED=1") {
		t.Errorf("the image comes before a variable: %v", args)
	}
}

func TestBuildArgs(t *testing.T) {
	b := runtime.BuildSpec{Tag: "whr-env/o1:abc", ContextDir: "/var/ctx", Dockerfile: "/var/df/Dockerfile", Args: map[string]string{"B": "2", "A": "1"}}
	got := strings.Join(buildArgs(b), " ")
	want := "build --progress plain --tag whr-env/o1:abc --file /var/df/Dockerfile --build-arg A=1 --build-arg B=2 -- /var/ctx"
	if got != want {
		t.Errorf("buildArgs =\n%s\nwant\n%s", got, want)
	}
	for _, banned := range []string{"--ssh", "--secret", "--output", "--no-cache"} {
		if strings.Contains(got, banned) {
			t.Errorf("build passes %s", banned)
		}
	}
}

func TestBuildRefusesAnInvalidSpecBeforeRunning(t *testing.T) {
	rec := &recorder{}
	a := &Adapter{owner: "o1", run: rec.run}
	_, err := a.Build(context.Background(), runtime.BuildSpec{Tag: "-x", ContextDir: "relative", Dockerfile: "relative"})
	if !errors.Is(err, runtime.ErrInvalidBuild) || len(rec.calls) != 0 {
		t.Errorf("err = %v, calls = %v", err, rec.calls)
	}
}

// A new volume is given to the environment's user by whr-shim from the
// read-only tool store, as the entrypoint, on the internal network: no program
// of the image (which may come from the repository, D38) runs as root, and the
// helper has no way out.
func TestANewVolumeIsOwnedWithoutRunningTheImageAsRoot(t *testing.T) {
	a := &Adapter{owner: "o1", shim: "/tools/profiles/p/bin/whr-shim"}
	spec := baseSpec()
	tools := runtime.Mount{Kind: runtime.MountBind, Source: "/store", Target: "/tools", ReadOnly: true}
	spec.Mounts = []runtime.Mount{{Kind: runtime.MountBind, Source: "/work", Target: "/work"}, tools}
	args, err := a.ownVolumeArgs("whr-1", spec, "wh-home")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--entrypoint /tools/profiles/p/bin/whr-shim", "--network wh-net-1", "--cap-add CHOWN", "--cap-drop ALL", "--read-only", "-v wh-home:/v", "-v /store:/tools:ro", "chown -owner 1000:1000 /v"} {
		if !strings.Contains(joined, want) {
			t.Errorf("helper %q lacks %q", joined, want)
		}
	}
	if strings.Contains(joined, "/work") || strings.Contains(joined, "default") {
		t.Errorf("the helper gets another mount or the default network: %s", joined)
	}
	if args[len(args)-5] != spec.Image {
		t.Errorf("the image must come right before the shim's arguments: %v", args)
	}
	if _, err := (&Adapter{owner: "o1"}).ownVolumeArgs("whr-1", spec, "wh-home"); !errors.Is(err, ErrNoLauncher) {
		t.Errorf("without a launcher = %v, want ErrNoLauncher", err)
	}
	spec.Mounts = []runtime.Mount{{Kind: runtime.MountBind, Source: "/store", Target: "/tools"}} // writable: not the tool store
	if _, err := a.ownVolumeArgs("whr-1", spec, "wh-home"); !errors.Is(err, ErrNoLauncher) {
		t.Errorf("a writable mount for the launcher = %v, want ErrNoLauncher", err)
	}
}

func TestHasImageTellsAMissingImageFromAFailure(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
		fail bool
	}{
		"present":           {nil, true, false},
		"missing":           {&ExitError{Args: []string{"image", "inspect"}, Err: errors.New("exit status 1"), Stderr: "Error: image not found: whr-base/x:1"}, false, false},
		"the CLI is broken": {&ExitError{Args: []string{"image", "inspect"}, Err: errors.New("exit status 1"), Stderr: "Error: the system service is not running"}, false, true},
	}
	for name, c := range cases {
		a := &Adapter{owner: "o1", run: func(context.Context, io.Reader, ...string) ([]byte, []byte, error) { return nil, nil, c.err }}
		got, err := a.HasImage(context.Background(), "whr-base/fedora:abc")
		if got != c.want || (err != nil) != c.fail {
			t.Errorf("%s: got %v, err %v", name, got, err)
		}
	}
	if _, err := (&Adapter{}).HasImage(context.Background(), "--all"); !errors.Is(err, runtime.ErrInvalidBuild) {
		t.Errorf("a tag that looks like an option: err = %v", err)
	}
}

func TestRemoveHelperTriesAgainUntilTheRuntimeLetsGo(t *testing.T) {
	var deletes int
	a := &Adapter{owner: "o1", run: func(_ context.Context, _ io.Reader, args ...string) ([]byte, []byte, error) {
		if args[0] == "delete" {
			deletes++
			if deletes < 3 {
				return nil, []byte("container is stopping"), errors.New("exit status 1")
			}
		}
		return nil, nil, nil
	}}
	a.removeHelper(context.Background(), "whr-1-own-v")
	if deletes != 3 {
		t.Errorf("%d delete calls, want 3 (two refused, one done)", deletes)
	}
	// A helper that is already gone is done at once.
	deletes = 0
	b := &Adapter{owner: "o1", run: func(_ context.Context, _ io.Reader, _ ...string) ([]byte, []byte, error) {
		deletes++
		return nil, []byte("Error: container not found"), errors.New("exit status 1")
	}}
	b.removeHelper(context.Background(), "whr-1-own-v")
	if deletes != 1 {
		t.Errorf("%d delete calls for a helper that is gone, want 1", deletes)
	}
}

// A helper of ownVolume that was left behind refers to the environment's network,
// which then cannot be deleted: Delete removes it first.
func TestDeleteRemovesAHelperThatWasLeftBehind(t *testing.T) {
	list := `[
	{"configuration":{"id":"whr-ab","labels":{"workharbor.owner":"o1","workharbor.role":"environment","workharbor.network":"net-ab"}},"status":{"state":"stopped"}},
	{"configuration":{"id":"whr-ab-own-vol","labels":{"workharbor.owner":"o1","workharbor.role":"volume","workharbor.env":"whr-ab"}},"status":{"state":"stopped"}},
	{"configuration":{"id":"whr-zz-own-vol","labels":{"workharbor.owner":"o1","workharbor.role":"volume","workharbor.env":"whr-zz"}},"status":{"state":"stopped"}}]`
	var calls [][]string
	a := &Adapter{owner: "o1", run: func(_ context.Context, _ io.Reader, args ...string) ([]byte, []byte, error) {
		calls = append(calls, args)
		if slices.Contains(args, "list") {
			return []byte(list), nil, nil
		}
		return nil, nil, nil
	}}
	if err := a.Delete(context.Background(), "whr-ab"); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	for _, c := range calls {
		if c[0] == "delete" {
			deleted = append(deleted, c[len(c)-1])
		}
		if c[0] == "network" && c[1] == "delete" && !slices.Contains(deleted, "whr-ab-own-vol") {
			t.Errorf("the network was deleted before the helper that refers to it: %v", deleted)
		}
	}
	if !slices.Contains(deleted, "whr-ab-own-vol") || slices.Contains(deleted, "whr-zz-own-vol") {
		t.Errorf("deleted %v: want this environment's helper and not another's", deleted)
	}
}
