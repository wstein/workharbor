package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type prepRig struct {
	home, project, cache string
	opts                 PrepareOptions
}

func newPrepRig(t *testing.T) prepRig {
	t.Helper()
	home := realDir(t)
	r := prepRig{home: home, project: filepath.Join(home, "src", "app"), cache: filepath.Join(home, "cache")}
	mkdirs(t, r.project, filepath.Join(home, ".ssh"), filepath.Join(r.cache, "repo.git", "objects"), filepath.Join(home, "bin"))
	if err := os.WriteFile(filepath.Join(home, "bin", "proxy"), []byte("x"), 0o700); err != nil { //nolint:gosec // a stand-in binary
		t.Fatal(err)
	}
	r.opts = PrepareOptions{
		FS: OSFS{}, Home: home, CacheRoots: []string{r.cache},
		Owns: func(v string) bool { return strings.HasPrefix(v, "task1-") },
	}
	return r
}

func goodSpec() Spec {
	return Spec{
		Image: "debian", Owner: "wh", CPUs: 1, MemoryMB: 512, DiskMB: 1024,
		Network: Network{Name: "net1", Internal: true}, ReadOnlyRoot: true,
		User: "1000:1000", Init: true, CapDrop: []string{"ALL"},
	}
}

// The review of 6a48473 found a spec with the default network, a writable
// root, another task's volume and a bind of ~/.ssh validated. Each is refused.
func TestPrepareMakesHardeningRequired(t *testing.T) {
	r := newPrepRig(t)
	ssh := filepath.Join(r.home, ".ssh")
	tests := map[string]struct {
		mod  func(*Spec)
		want error
	}{
		"the default network":        {func(s *Spec) { s.Network = Network{} }, ErrInvalidSpec},
		"an external network":        {func(s *Spec) { s.Network.Internal = false }, ErrInvalidSpec},
		"a writable root":            {func(s *Spec) { s.ReadOnlyRoot = false }, ErrInvalidSpec},
		"another task's volume":      {func(s *Spec) { s.Mounts = []Mount{{Kind: MountVolume, Source: "task2-home", Target: "/home/agent"}} }, ErrInvalidSpec},
		"a bind of ~/.ssh":           {func(s *Spec) { s.Mounts = []Mount{{Source: ssh, Target: "/mnt"}} }, ErrForbiddenMount},
		"a read-only bind of ~/.ssh": {func(s *Spec) { s.Mounts = []Mount{{Source: ssh, Target: "/mnt", ReadOnly: true}} }, ErrForbiddenMount},
		"no user":                    {func(s *Spec) { s.User = "" }, ErrInvalidSpec},
	}
	for name, tc := range tests {
		spec := goodSpec()
		tc.mod(&spec)
		p, err := Prepare(r.opts, spec)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Prepare = %v, want %v", name, err, tc.want)
		}
		if p.Prepared() {
			t.Errorf("%s: a refused spec came back prepared", name)
		}
	}
	// With no Owns, no volume is anyone's.
	spec := goodSpec()
	spec.Mounts = []Mount{{Kind: MountVolume, Source: "task1-home", Target: "/home/agent"}}
	noOwner := r.opts
	noOwner.Owns = nil
	if _, err := Prepare(noOwner, spec); !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("a volume with no ownership check = %v, want ErrInvalidSpec", err)
	}
	if _, err := Prepare(r.opts, spec); err != nil {
		t.Errorf("an owned volume: %v", err)
	}
}

func TestOnlyPrepareMakesAPreparedSpec(t *testing.T) {
	if (PreparedSpec{}).Prepared() {
		t.Error("the zero value is prepared")
	}
	r := newPrepRig(t)
	p, err := Prepare(r.opts, goodSpec())
	if err != nil || !p.Prepared() {
		t.Fatalf("Prepare = %v, %v", p, err)
	}
	// What Spec returns is a copy: changing it changes nothing in the prepared spec.
	cp := p.Spec()
	cp.CapDrop[0], cp.User = "NONE", "0"
	if again := p.Spec(); again.CapDrop[0] != "ALL" || again.User != "1000:1000" {
		t.Errorf("the prepared spec was changed through a copy: %+v", again)
	}
}

// The adapter mounts the resolved path that was checked, so a source swapped for
// a symlink after the check is not followed.
func TestAMountSwappedForASymlinkAfterTheCheckIsNotFollowed(t *testing.T) {
	r := newPrepRig(t)
	link := filepath.Join(realDir(t), "work")
	symlink(t, r.project, link)
	spec := goodSpec()
	spec.Mounts = []Mount{{Source: link, Target: "/work"}}
	p, err := Prepare(r.opts, spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Spec().Mounts[0].Source; got != r.project {
		t.Fatalf("the prepared source = %q, want the resolved %q", got, r.project)
	}

	// After the check the link is pointed at the secrets directory.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(r.home, ".ssh"), link)
	if got := p.Spec().Mounts[0].Source; got != r.project {
		t.Errorf("after the swap the mount is %q: the adapter would follow the new link", got)
	}
}

func TestPrepareMountsCacheObjectsReadOnlyAtTheirHostPath(t *testing.T) {
	r := newPrepRig(t)
	objects := filepath.Join(r.cache, "repo.git", "objects")
	spec := goodSpec()
	spec.Alternates = []string{objects}
	p, err := Prepare(r.opts, spec)
	if err != nil {
		t.Fatal(err)
	}
	mounts := p.Spec().Mounts
	if len(mounts) != 1 || mounts[0] != (Mount{Kind: MountBind, Source: objects, Target: objects, ReadOnly: true}) {
		t.Errorf("mounts = %+v, want one read-only bind at the host path", mounts)
	}

	for name, alt := range map[string]string{
		"outside a cache root": r.project,
		"a secrets directory":  filepath.Join(r.home, ".ssh"),
		"missing":              filepath.Join(r.cache, "nope", "objects"),
		"relative":             "objects",
	} {
		spec := goodSpec()
		spec.Alternates = []string{alt}
		if _, err := Prepare(r.opts, spec); err == nil {
			t.Errorf("cache objects %s (%q) were accepted", name, alt)
		}
	}
}

func TestPrepareChecksTheEgressSidecar(t *testing.T) {
	r := newPrepRig(t)
	proxy := filepath.Join(r.home, "bin", "proxy")
	spec := goodSpec()
	spec.Egress = &Egress{Image: "debian", Proxy: proxy, Allow: []string{"api.anthropic.com", "proxy.golang.org"}}
	if p, err := Prepare(r.opts, spec); err != nil || p.Spec().Egress.Proxy != proxy {
		t.Fatalf("a good sidecar: %v", err)
	}
	for name, mod := range map[string]func(*Egress){
		"no image":            func(e *Egress) { e.Image = "" },
		"a relative proxy":    func(e *Egress) { e.Proxy = "proxy" },
		"a proxy in ~/.ssh":   func(e *Egress) { e.Proxy = filepath.Join(r.home, ".ssh") },
		"a raw IP":            func(e *Egress) { e.Allow = []string{"1.1.1.1"} },
		"a wildcard":          func(e *Egress) { e.Allow = []string{"*.example.com"} },
		"a host with a path":  func(e *Egress) { e.Allow = []string{"example.com/x"} },
		"a single-label host": func(e *Egress) { e.Allow = []string{"localhost"} },
		"an empty entry":      func(e *Egress) { e.Allow = []string{""} },
	} {
		s := goodSpec()
		e := Egress{Image: "debian", Proxy: proxy, Allow: []string{"example.com"}}
		mod(&e)
		s.Egress = &e
		if _, err := Prepare(r.opts, s); err == nil {
			t.Errorf("%s: the sidecar was accepted", name)
		}
	}
}

func TestResolveMountReturnsTheCheckedPath(t *testing.T) {
	r := newPrepRig(t)
	link := filepath.Join(realDir(t), "work")
	symlink(t, r.project, link)
	got, err := ResolveMount(OSFS{}, r.home, link)
	if err != nil || got != r.project {
		t.Errorf("ResolveMount(%q) = %q, %v; want %q", link, got, err, r.project)
	}
	if got, err := ResolveMount(OSFS{}, r.home, filepath.Join(r.home, ".ssh")); err == nil || got != "" {
		t.Errorf("a forbidden source returned %q, %v", got, err)
	}
}

// A link without ':' that resolves to a path with one is judged by the path
// that would be mounted.
func TestPrepareRefusesAResolvedSourceWithAColon(t *testing.T) {
	r := newPrepRig(t)
	odd := filepath.Join(r.home, "src", "a:b")
	mkdirs(t, odd)
	link := filepath.Join(r.home, "src", "plain")
	if err := os.Symlink(odd, link); err != nil {
		t.Fatal(err)
	}
	spec := goodSpec()
	spec.Mounts = []Mount{{Source: link, Target: "/work"}}
	if _, err := Prepare(r.opts, spec); !errors.Is(err, ErrInvalidSpec) || !strings.Contains(err.Error(), "':'") {
		t.Errorf("Prepare = %v, want a refusal of the ':'", err)
	}
}

// swapFS answers the first resolution of one path truthfully and every later one
// with another place: a source swapped for a symlink between two lookups.
type swapFS struct {
	OSFS
	path  string
	to    string
	calls int
}

func (f *swapFS) EvalSymlinks(p string) (string, error) {
	if filepath.Clean(p) == f.path {
		f.calls++
		if f.calls > 1 {
			return f.to, nil
		}
	}
	return f.OSFS.EvalSymlinks(p)
}

// A mount source is resolved once. The roots are checked against the path the
// deny-list passed, which is the path that is mounted: a link swapped between two
// lookups (the audit's TOCTOU) cannot pass the checks and mount something else.
func TestAMountSourceIsResolvedOnceSoASwappedLinkCannotPassTheRootCheck(t *testing.T) {
	r := newPrepRig(t)
	root := filepath.Join(r.home, "ws")
	inside := filepath.Join(root, "docs")
	outside := filepath.Join(r.home, "elsewhere")
	mkdirs(t, inside, outside)
	r.opts.Roots = []string{root}
	fsys := &swapFS{OSFS: OSFS{}, path: inside, to: outside}
	r.opts.FS = fsys
	spec := goodSpec()
	spec.Mounts = []Mount{{Source: inside, Target: "/ws"}}
	p, err := Prepare(r.opts, spec)
	if err != nil {
		t.Fatal(err)
	}
	if fsys.calls != 1 {
		t.Errorf("the source was resolved %d times, want once", fsys.calls)
	}
	if got := p.Spec().Mounts; len(got) != 1 || got[0].Source != inside {
		t.Errorf("mounted %+v, want the path that was checked, %s", got, inside)
	}
	// The same for the checker that Prepare shares its rule with.
	fsys = &swapFS{OSFS: OSFS{}, path: inside, to: outside}
	if err := CheckMountsWithin(fsys, r.home, []string{root}, []Mount{{Source: inside, Target: "/ws"}}); err != nil {
		t.Fatal(err)
	}
	if fsys.calls != 1 {
		t.Errorf("CheckMountsWithin resolved the source %d times, want once", fsys.calls)
	}
}
