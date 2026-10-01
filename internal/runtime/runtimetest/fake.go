// Package runtimetest has an in-memory runtime adapter and the conformance
// suite every real backend must pass (design §5.1). The fake is what the
// domain and the reconciler are tested against, so it can simulate what the
// Apple Container service does: a restart that stops every environment.
package runtimetest

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
)

// Fake is an in-memory runtime.Adapter acting for one owner.
type Fake struct {
	owner string
	fsys  runtime.FS
	home  string

	mu   sync.Mutex
	envs map[string]*fakeEnv
	next int
	ip   int
}

type fakeEnv struct {
	id     string
	owner  string
	labels map[string]string
	image  string
	state  domain.EnvState
	addr   string
	logs   []byte
}

// NewFake returns a fake that acts for owner. Bind mounts are vetted against
// home on fsys, as a real adapter must.
func NewFake(owner, home string, fsys runtime.FS) *Fake {
	return &Fake{owner: owner, home: home, fsys: fsys, envs: map[string]*fakeEnv{}}
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

// Provision implements runtime.Adapter.
func (f *Fake) Provision(_ context.Context, spec runtime.Spec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	if spec.Owner != f.owner {
		return "", &runtime.SpecError{Problems: []string{fmt.Sprintf("owner %q is not this adapter's owner %q", spec.Owner, f.owner)}}
	}
	if err := spec.CheckMounts(f.fsys, f.home); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := "fake-" + strconv.Itoa(f.next)
	labels := map[string]string{runtime.OwnerLabel: spec.Owner}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	f.envs[id] = &fakeEnv{id: id, owner: spec.Owner, labels: labels, image: spec.Image, state: domain.EnvStopped}
	return id, nil
}

// own returns the environment if it exists and belongs to this adapter. The
// caller holds the lock.
func (f *Fake) own(id string) (*fakeEnv, error) {
	e, ok := f.envs[id]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	if e.owner != f.owner {
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
	if e.state == domain.EnvRunning {
		return runtime.ErrRunning
	}
	delete(f.envs, id)
	return nil
}

func (f *Fake) info(e *fakeEnv) runtime.Info {
	labels := make(map[string]string, len(e.labels))
	for k, v := range e.labels {
		labels[k] = v
	}
	return runtime.Info{ID: e.id, Owner: e.owner, Labels: labels, Image: e.image, State: e.state, Addr: e.addr}
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
		if e, ok := f.envs["fake-"+strconv.Itoa(i)]; ok && e.labels[runtime.OwnerLabel] == owner {
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
