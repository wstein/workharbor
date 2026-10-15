// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// allocation binds identity only during its single live native run request. It
// cannot recover a lost request, report resource completion or adopt by labels.
type allocation struct {
	mu                     sync.Mutex
	cid, root, data, image string
	scope                  Scope
	deadline               time.Time
	check                  func(context.Context) error
	begun, live, closed    bool
	target                 *Target
}

func reserveAllocation(ctx context.Context, root string, scope Scope, data, image string, check func(context.Context) error) (*allocation, error) {
	if check == nil {
		return nil, fail("forbidden")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if e := check(ctx); e != nil {
		return nil, e
	}
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(time.Now()) || !ownedDirectory(root) || !scope.valid() || !validData(data) || !strings.HasPrefix(image, "sha256:") || !hexID.MatchString(strings.TrimPrefix(image, "sha256:")) {
		return nil, fail("provider_refused")
	}
	private, e := os.MkdirTemp(root, "allocation-")
	if e != nil {
		return nil, fail("provider_unavailable")
	}
	a := &allocation{cid: filepath.Join(private, scope.RuntimeID+".cid"), root: private, scope: scope, data: data, image: image, deadline: deadline, check: check}
	if e = check(ctx); e != nil {
		os.RemoveAll(private)
		return nil, e
	}
	return a, nil
}
func (a *allocation) authorized(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if !time.Now().Before(a.deadline) || a.check == nil {
		return fail("runtime_unknown")
	}
	return a.check(ctx)
}
func (a *allocation) Begin(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.begun {
		return fail("runtime_unknown")
	}
	if e := a.authorized(ctx); e != nil {
		return e
	}
	if !ownedDirectory(a.root) {
		return fail("runtime_unknown")
	}
	if _, e := os.Lstat(a.cid); !os.IsNotExist(e) {
		return fail("runtime_unknown")
	}
	a.begun = true
	a.live = true
	return nil
}
func (a *allocation) Observe(ctx context.Context, candidates []Container) (Target, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || !a.live {
		return Target{}, fail("runtime_unknown")
	}
	if e := a.authorized(ctx); e != nil {
		return Target{}, e
	}
	if !ownedDirectory(a.root) {
		return Target{}, fail("runtime_unknown")
	}
	info, e := os.Lstat(a.cid)
	if e != nil || !ownedRegular(info) || info.Size() != 64 {
		return Target{}, fail("runtime_unknown")
	}
	f, e := os.Open(a.cid)
	if e != nil {
		return Target{}, fail("runtime_unknown")
	}
	actual, se := f.Stat()
	raw, re := io.ReadAll(io.LimitReader(f, 65))
	ce := f.Close()
	current, le := os.Lstat(a.cid)
	if se != nil || re != nil || ce != nil || le != nil || !os.SameFile(info, actual) || !os.SameFile(info, current) || !ownedRegular(current) || len(raw) != 64 || !hexID.Match(raw) {
		return Target{}, fail("runtime_unknown")
	}
	target := Target{Scope: a.scope, ContainerID: string(raw), ImageID: a.image, DataPath: a.data}
	if _, e = VerifyTarget(target, candidates); e != nil {
		return Target{}, e
	}
	if a.target != nil && *a.target != target {
		return Target{}, fail("runtime_conflict")
	}
	// End uses this same lock. Check/deadline and first binding are one live action.
	if e = a.authorized(ctx); e != nil {
		return Target{}, e
	}
	if a.target == nil {
		a.target = &target
	}
	return *a.target, nil
}
func (a *allocation) End() { a.mu.Lock(); a.live = false; a.mu.Unlock() }
func (a *allocation) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.live = false
	if a.closed {
		return nil
	}
	a.closed = true
	if !ownedDirectory(a.root) {
		return fail("runtime_unknown")
	}
	return os.RemoveAll(a.root)
}
