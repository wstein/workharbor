// Package runtimetest has an in-memory runtime adapter and the conformance
// suite every real backend must pass (design §5.1). The fake is what the
// domain and the reconciler are tested against, so it can simulate what the
// Apple Container service does: a restart that stops every environment.
package runtimetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Defects switches off one guarantee of the fake, so tests can show that the
// conformance suite notices an adapter that lacks it.
type Defects struct {
	AcceptUnprepared bool // Provision accepts a spec that Prepare did not check
	ShareVolumes     bool // two running environments may hold one volume read-write
	LeaveResources   bool // Delete leaves the network and the sidecar behind
	DropMounts       bool // the runtime mounts nothing of what was prepared
	ListAll          bool // List ignores the owner
	TouchForeign     bool // ID methods act on environments it does not own
	DeleteRunning    bool // Delete removes a running environment
	RestartKeepsRun  bool // Restart leaves running environments running
	IgnoreStdin      bool // Exec never reads the request's Stdin
	CancelLeavesRun  bool // cancelling an exec returns, but the process in the guest keeps running
}

// Fake is an in-memory runtime.Adapter acting for one owner.
type Fake struct {
	// Defects is for tests of the suite only.
	Defects Defects
	// GitExit is the exit code of a `git` command in an environment, for a
	// test of a git step that fails. Zero (the default) succeeds.
	GitExit int
	// TrackedFiles is what the fake answers to the supervisor's count of the files a
	// checkout tracks (a `sh -c` that runs `git ls-files`). Guarded by the fake's lock.
	TrackedFiles int
	// OnExec, when set, is asked first about every command: a test that needs
	// real output (a git bundle made by the host standing in for the guest) says
	// what the command wrote and how it ended. handled false falls back to the
	// fake's own commands.
	OnExec func(env string, cmd []string) (stdout []byte, stderr string, code int, handled bool)

	// OnExecReq is OnExec with the whole request, so a test can read the
	// request's stdin or see its environment. It is asked first.
	OnExecReq func(env string, req runtime.ExecRequest) (stdout []byte, stderr string, code int, handled bool)

	owner string
	fsys  runtime.FS
	home  string

	mu       sync.Mutex
	envs     map[string]*fakeEnv
	volumes  map[string]bool   // named volumes that exist
	networks map[string]string // network name to the environment it belongs to
	sidecars map[string]bool
	execs    []ExecCall
	// terminals and sizes are what the fake's terminals were asked and told.
	terminals []TerminalCall
	sizes     map[string][][2]uint16
	next      int
	ip        int
}

type fakeEnv struct {
	id     string
	owner  string
	labels map[string]string
	image  string
	state  domain.EnvState
	addr   string
	logs   []byte
	procs  int // sleep commands running in the guest

	mounts  []runtime.Mount
	network string
	sidecar string
	allow   []string // the hosts the sidecar allows
	proxies int      // how many sidecars the environment has had: a new one has a new address
}

// NewFake returns a fake that acts for owner. Bind mounts are vetted against
// home on fsys, as a real adapter must.
func NewFake(owner, home string, fsys runtime.FS) *Fake {
	return &Fake{owner: owner, home: home, fsys: fsys, envs: map[string]*fakeEnv{}, volumes: map[string]bool{}, networks: map[string]string{}, sidecars: map[string]bool{}}
}

// Name implements runtime.Adapter.
func (f *Fake) Name() string { return "fake" }

// Capabilities implements runtime.Adapter.
func (f *Fake) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{
		Isolation:         runtime.GuestKernel,
		Arch:              "arm64",
		PersistentStorage: []string{"volumes", "bind mounts"},
		NetworkIsolation:  true,
	}
}

// Provision implements runtime.Adapter. It creates the environment's internal
// network, the volumes it names and the egress sidecar.
func (f *Fake) Provision(_ context.Context, prep runtime.PreparedSpec) (string, error) {
	if !prep.Prepared() && !f.Defects.AcceptUnprepared {
		return "", runtime.ErrNotPrepared
	}
	spec := prep.Spec()
	if spec.Owner != f.owner {
		return "", &runtime.SpecError{Problems: []string{fmt.Sprintf("owner %q is not this adapter's owner %q", spec.Owner, f.owner)}}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if spec.Network.Name != "" {
		if _, taken := f.networks[spec.Network.Name]; taken {
			return "", &runtime.SpecError{Problems: []string{fmt.Sprintf("network %q is already in use: a network is never shared between environments", spec.Network.Name)}}
		}
	}
	f.next++
	id := "fake-" + strconv.Itoa(f.next)
	labels := map[string]string{runtime.OwnerLabel: spec.Owner}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	env := &fakeEnv{id: id, owner: spec.Owner, labels: labels, image: spec.Image, state: domain.EnvStopped, network: spec.Network.Name}
	if !f.Defects.DropMounts {
		env.mounts = append(env.mounts, spec.Mounts...)
	}
	if env.network != "" {
		f.networks[env.network] = id
	}
	if spec.Egress != nil {
		env.sidecar = id + "-proxy"
		f.sidecars[env.sidecar] = true
		env.allow = sortedCopy(spec.Egress.Allow)
	}
	for _, m := range spec.Mounts {
		if m.Kind == runtime.MountVolume {
			f.volumes[m.Source] = true
		}
	}
	f.envs[id] = env
	return id, nil
}

// own returns the environment if it exists and belongs to this adapter. The
// caller holds the lock.
func (f *Fake) own(id string) (*fakeEnv, error) {
	e, ok := f.envs[id]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	if e.owner != f.owner && !f.Defects.TouchForeign {
		return nil, runtime.ErrNotOwned
	}
	return e, nil
}

// Start implements runtime.Adapter. Every start gives a new address.
func (f *Fake) Start(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		return err
	}
	if e.state != domain.EnvRunning {
		if !f.Defects.ShareVolumes {
			for _, m := range e.mounts {
				if m.Kind == runtime.MountVolume && !m.ReadOnly && f.heldByRunning(m.Source, e.id) {
					return runtime.ErrVolumeBusy
				}
			}
		}
		f.ip++
		e.addr = "192.168.64." + strconv.Itoa(f.ip)
		e.state = domain.EnvRunning
	}
	return nil
}

// Stop implements runtime.Adapter.
func (f *Fake) Stop(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		return err
	}
	e.state, e.addr = domain.EnvStopped, ""
	return nil
}

// Delete implements runtime.Adapter. Deleting an environment that is gone
// succeeds, so a retry is safe.
func (f *Fake) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		return err
	}
	if e.state == domain.EnvRunning && !f.Defects.DeleteRunning {
		return runtime.ErrRunning
	}
	if !f.Defects.LeaveResources {
		delete(f.networks, e.network)
		delete(f.sidecars, e.sidecar)
	}
	delete(f.envs, id)
	return nil
}

// heldByRunning reports whether another running environment holds a volume
// read-write. The caller holds the lock.
func (f *Fake) heldByRunning(volume, except string) bool {
	for _, o := range f.envs {
		if o.id == except || o.state != domain.EnvRunning {
			continue
		}
		for _, m := range o.mounts {
			if m.Kind == runtime.MountVolume && m.Source == volume && !m.ReadOnly {
				return true
			}
		}
	}
	return false
}

// RemoveVolume implements runtime.Adapter.
func (f *Fake) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.envs {
		if o.state != domain.EnvRunning {
			continue
		}
		for _, m := range o.mounts {
			if m.Kind == runtime.MountVolume && m.Source == name {
				return runtime.ErrVolumeBusy
			}
		}
	}
	delete(f.volumes, name)
	return nil
}

// Resources implements runtime.Adapter.
func (f *Fake) Resources(_ context.Context, id string) (runtime.Resources, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		return runtime.Resources{}, err
	}
	res := runtime.Resources{Network: e.network, Sidecar: e.sidecar}
	for _, m := range e.mounts {
		if m.Kind == runtime.MountVolume {
			res.Volumes = append(res.Volumes, m.Source)
		}
	}
	return res, nil
}

// Inventory implements runtime.Adapter.
func (f *Fake) Inventory(context.Context) (runtime.Inventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var inv runtime.Inventory
	for n := range f.networks {
		inv.Networks = append(inv.Networks, n)
	}
	for v := range f.volumes {
		inv.Volumes = append(inv.Volumes, v)
	}
	for sc := range f.sidecars {
		inv.Sidecars = append(inv.Sidecars, sc)
	}
	sort.Strings(inv.Networks)
	sort.Strings(inv.Volumes)
	sort.Strings(inv.Sidecars)
	return inv, nil
}

func (f *Fake) info(e *fakeEnv) runtime.Info {
	labels := make(map[string]string, len(e.labels))
	for k, v := range e.labels {
		labels[k] = v
	}
	info := runtime.Info{ID: e.id, Owner: e.owner, Labels: labels, Image: e.image, ImageDigest: fakeDigest(e.image), Mounts: append([]runtime.Mount(nil), e.mounts...), State: e.state, Addr: e.addr}
	if e.sidecar != "" {
		info.EgressAllow = append([]string(nil), e.allow...)
	}
	if e.sidecar != "" && e.state == domain.EnvRunning {
		info.Proxy = "http://" + e.addr + ":" + strconv.Itoa(3128+e.proxies)
	}
	return info
}

// Inspect implements runtime.Adapter.
func (f *Fake) Inspect(_ context.Context, id string) (runtime.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		return runtime.Info{}, err
	}
	return f.info(e), nil
}

// List implements runtime.Adapter.
func (f *Fake) List(_ context.Context, owner string) ([]runtime.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []runtime.Info
	for i := 1; i <= f.next; i++ {
		if e, ok := f.envs["fake-"+strconv.Itoa(i)]; ok && (f.Defects.ListAll || e.labels[runtime.OwnerLabel] == owner) {
			out = append(out, f.info(e))
		}
	}
	return out, nil
}

// Logs implements runtime.Adapter.
func (f *Fake) Logs(_ context.Context, id string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), e.logs...), nil
}

// Endpoints implements runtime.Adapter. The fake offers no ssh or browser.
func (f *Fake) Endpoints(_ context.Context, id string) ([]runtime.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.own(id); err != nil {
		return nil, err
	}
	return nil, nil
}

// Restart simulates a service restart: every environment ends up stopped, as
// Apple Container measured in spike #2 (design §5.3). Nothing comes back by
// itself.
func (f *Fake) Restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Defects.RestartKeepsRun {
		return
	}
	for _, e := range f.envs {
		e.state, e.addr = domain.EnvStopped, ""
	}
}

// AddForeign adds an environment owned by someone else, as another tool on
// the same machine would have made, and returns its ID.
func (f *Fake) AddForeign(owner string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := "fake-" + strconv.Itoa(f.next)
	f.envs[id] = &fakeEnv{
		id: id, owner: owner, labels: map[string]string{runtime.OwnerLabel: owner},
		image: "foreign", state: domain.EnvRunning, addr: "192.168.64.250",
	}
	return id
}

// Count returns how many environments exist, whoever owns them.
func (f *Fake) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.envs)
}

// logLine appends to an environment's logs.
func (f *Fake) logLine(id, line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.envs[id]; ok {
		e.logs = append(e.logs, []byte(strings.TrimRight(line, "\n")+"\n")...)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// UpdateEgress implements runtime.EgressUpdater: the sidecar is replaced by one
// with the new allowlist, as a real adapter does, so the environment's proxy
// address changes.
func (f *Fake) UpdateEgress(_ context.Context, id string, prep runtime.PreparedSpec) error {
	if !prep.Prepared() {
		return runtime.ErrNotPrepared
	}
	spec := prep.Spec()
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.own(id)
	if err != nil {
		return err
	}
	if spec.Egress == nil || e.sidecar == "" || spec.Network.Name != e.network {
		return &runtime.SpecError{Problems: []string{"the spec must carry an Egress and the environment's own network"}}
	}
	e.allow = sortedCopy(spec.Egress.Allow)
	e.proxies++ // a new sidecar is a new address
	return nil
}

// ExecCall is one exec the fake was asked to run, with the whole request.
type ExecCall struct {
	Env string // the environment's ID
	Req runtime.ExecRequest
}

// Execs returns every exec the fake was asked to run, in order.
func (f *Fake) Execs() []ExecCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ExecCall(nil), f.execs...)
}

// fakeDigest stands in for the digest a real runtime records for an image: a
// SHA-256 of the reference, so two references have two digests and one has one.
func fakeDigest(image string) string {
	sum := sha256.Sum256([]byte(image))
	return "sha256:" + hex.EncodeToString(sum[:])
}
