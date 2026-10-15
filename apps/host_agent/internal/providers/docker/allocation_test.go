// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func allocationFixture(t *testing.T) (*allocation, context.Context, Container) {
	t.Helper()
	g, p := gatewayFixture(t, Resources)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	a, e := reserveAllocation(ctx, bridgeRoot(t), g.Policy.Scope, g.Policy.DataPath, g.Target.ImageID, func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	return a, ctx, p.candidates[0]
}
func TestAllocationCIDIsAbsentPrivateAndOnlyLiveExactIdentityBinds(t *testing.T) {
	a, ctx, c := allocationFixture(t)
	if _, e := os.Lstat(a.cid); !os.IsNotExist(e) {
		t.Fatal("CID precreated", e)
	}
	info, e := os.Stat(filepath.Dir(a.cid))
	if e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("private reservation", e)
	}
	if e = a.Begin(ctx); e != nil {
		t.Fatal(e)
	}
	if e = a.Begin(ctx); e == nil {
		t.Fatal("duplicate allocation admitted")
	}
	if e = os.WriteFile(a.cid, []byte(c.ID), 0644); e != nil {
		t.Fatal(e)
	}
	target, e := a.Observe(ctx, []Container{c})
	if e != nil || target.ContainerID != c.ID {
		t.Fatal("live exact target", target, e)
	}
	c.ID = strings.Repeat("c", 64)
	os.WriteFile(a.cid, []byte(c.ID), 0644)
	if _, e = a.Observe(ctx, []Container{c}); e == nil {
		t.Fatal("first target replaced")
	}
	a.End()
	if _, e = a.Observe(ctx, []Container{c}); e == nil {
		t.Fatal("closed allocation adopted")
	}
}
func TestAllocationUnknownCIDForeignDuplicatesAndRevocationNeverBind(t *testing.T) {
	for _, kind := range []string{"missing", "partial", "newline", "symlink", "duplicate", "foreign", "revoked", "expired"} {
		t.Run(kind, func(t *testing.T) {
			a, ctx, c := allocationFixture(t)
			if e := a.Begin(ctx); e != nil {
				t.Fatal(e)
			}
			raw := c.ID
			candidates := []Container{c}
			switch kind {
			case "partial":
				raw = c.ID[:10]
			case "newline":
				raw += "\n"
			case "duplicate":
				candidates = append(candidates, c)
			case "foreign":
				candidates[0].Labels["workharbor.owner"] = "foreign"
			}
			if kind != "missing" {
				if kind == "symlink" {
					p := filepath.Join(filepath.Dir(a.cid), "foreign")
					os.WriteFile(p, []byte(raw), 0600)
					os.Symlink(p, a.cid)
				} else {
					os.WriteFile(a.cid, []byte(raw), 0644)
				}
			}
			if kind == "revoked" {
				a.check = func(context.Context) error { return errors.New("revoked") }
			}
			if kind == "expired" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			if _, e := a.Observe(ctx, candidates); e == nil {
				t.Fatal("unknown/foreign identity adopted")
			}
		})
	}
}
func TestAllocationEndWinsBeforeObservationAndDeniedReservationHasNoEffect(t *testing.T) {
	a, ctx, c := allocationFixture(t)
	if e := a.Begin(ctx); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(a.cid, []byte(c.ID), 0644)
	a.End()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, e := a.Observe(ctx, []Container{c}); e == nil {
				t.Error("bound after End")
			}
		})
	}
	wg.Wait()
	root := bridgeRoot(t)
	_, e := reserveAllocation(ctx, root, cScope(), "/data", "sha256:"+strings.Repeat("b", 64), func(context.Context) error { return errors.New("denied") })
	if e == nil {
		t.Fatal("denied reservation")
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("unauthorized filesystem effect")
	}
}
func cScope() Scope { return policyFixture(Resources).Scope }

func TestAllocationObserveAndEndSerializeWithoutLateBinding(t *testing.T) {
	a, ctx, c := allocationFixture(t)
	if e := a.Begin(ctx); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(a.cid, []byte(c.ID), 0644)
	entered, release := make(chan struct{}), make(chan struct{})
	first := true
	a.check = func(context.Context) error {
		if first {
			first = false
			close(entered)
			<-release
		}
		return nil
	}
	observed := make(chan error, 1)
	go func() { _, e := a.Observe(ctx, []Container{c}); observed <- e }()
	<-entered
	ended := make(chan struct{})
	go func() { a.End(); close(ended) }()
	select {
	case <-ended:
		t.Fatal("End crossed active atomic observation")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if e := <-observed; e != nil {
		t.Fatal(e)
	}
	<-ended
	if _, e := a.Observe(ctx, []Container{c}); e == nil {
		t.Fatal("observation after End")
	}
}
