//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/detect"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/pkgmgr"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/plan"
)

var _ pkgmgr.FS = (*Host)(nil)

func TestNativeBrewExecutableRootedMetadataOnly(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "opt/homebrew/Homebrew/bin/brew")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "opt/homebrew/bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inert fixture bytes; never run"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Homebrew/bin/brew", filepath.Join(root, "opt/homebrew/bin/brew")); err != nil {
		t.Fatal(err)
	}
	host, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	yes, err := host.BrewExecutable("/opt/homebrew/bin/brew")
	if !yes || err != nil {
		t.Fatalf("rooted relative link unavailable: %v %v", yes, err)
	}
	if err = os.Chmod(target, 0600); err != nil {
		t.Fatal(err)
	}
	if yes, err = host.BrewExecutable("/opt/homebrew/bin/brew"); yes || err != nil {
		t.Fatalf("nonexecutable accepted: %v %v", yes, err)
	}
	if yes, err = host.BrewExecutable("/usr/local/bin/brew"); yes || err != nil {
		t.Fatalf("missing prefix outcome: %v %v", yes, err)
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	if yes, err = host.BrewExecutable("/opt/homebrew/bin/brew"); yes || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed root outcome: %v %v", yes, err)
	}
}
func TestNativeBrewExecutableDoesNotFollowOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "brew")
	if err := os.WriteFile(target, []byte("inert outside fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "opt/homebrew/bin")
	if err := os.MkdirAll(prefix, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(prefix, "brew")); err != nil {
		t.Fatal(err)
	}
	host, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if yes, err := host.BrewExecutable("/opt/homebrew/bin/brew"); yes || err != nil {
		t.Fatalf("followed host-absolute alias outside virtual root: %v %v", yes, err)
	}
	if yes, err := host.BrewExecutable(target); yes || err == nil {
		t.Fatalf("accepted noncanonical path: %v %v", yes, err)
	}
}

type availabilityExec struct{ calls []pkgmgr.Command }

func (e *availabilityExec) Start(_ context.Context, c pkgmgr.Command) (pkgmgr.Process, error) {
	e.calls = append(e.calls, c)
	done := make(chan pkgmgr.Completion, 1)
	done <- pkgmgr.Completion{Exit: 0}
	return availabilityProcess{done: done}, nil
}

type availabilityProcess struct{ done chan pkgmgr.Completion }

func (p availabilityProcess) Done() <-chan pkgmgr.Completion { return p.done }
func (availabilityProcess) Interrupt() error                 { panic("unexpected fake interrupt") }
func (availabilityProcess) Kill() error                      { panic("unexpected fake kill") }

type availabilityClock struct{}

func (availabilityClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type availabilityDetector struct{ calls int }

func (d *availabilityDetector) Detect(_ context.Context, i catalogue.Item) detect.Result {
	d.calls++
	state := "absent"
	if d.calls > 1 {
		state = "present_ok"
	}
	return detect.Result{Item: i.ID, State: state}
}

type availabilityBook struct{}

func (availabilityBook) Begin(context.Context, string) (pkgmgr.Journal, error) {
	return availabilityBook{}, nil
}
func (availabilityBook) Record(context.Context, pkgmgr.Event) error { return nil }
func (availabilityBook) Close(context.Context) error                { return nil }

type availabilityCancelFS struct {
	host   *Host
	cancel context.CancelFunc
	calls  int
}

func (f *availabilityCancelFS) BrewExecutable(p string) (bool, error) {
	f.calls++
	yes, err := f.host.BrewExecutable(p)
	f.cancel()
	return yes, err
}
func TestNativeBrewExecutablePackageEngineFallbackAndCancellation(t *testing.T) {
	for _, mode := range []string{"fallback", "missing", "closed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if mode != "missing" {
				target := filepath.Join(root, "usr/local/bin/brew")
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("inert fixture"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			host, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			if mode == "closed" {
				if err = host.Close(); err != nil {
					t.Fatal(err)
				}
			}
			item := catalogue.Item{ID: "jq", SelectedByDefault: true, Tool: catalogue.Tool{Source: catalogue.Source{Manager: "homebrew", Type: "formula", Tap: "homebrew/core", Name: "jq"}}}
			input := plan.Input{Platform: "darwin-arm64", Catalogue: &catalogue.Catalogue{SchemaVersion: 1, Items: []catalogue.Item{item}}, Results: []detect.Result{{Item: "jq", State: "absent"}}}
			sealed, err := plan.Build(input)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			exec := &availabilityExec{}
			engine := pkgmgr.Engine{FS: host, Exec: exec, Clock: availabilityClock{}, Detector: &availabilityDetector{}, Bookkeeper: availabilityBook{}}
			cancellingFS := &availabilityCancelFS{host: host, cancel: cancel}
			if mode == "cancelled" {
				engine.FS = cancellingFS
			}
			got, err := engine.Apply(ctx, pkgmgr.Request{Input: input, ExpectedHash: sealed.Hash, Policy: pkgmgr.Policy{Mode: "setup", Yes: true, UID: 501}, Environment: map[string]string{"HOME": "/tmp/fake-home"}})
			if err != nil || len(got.Steps) != 1 {
				t.Fatalf("engine outcome %+v %v", got, err)
			}
			step := got.Steps[0]
			if mode == "fallback" {
				if step.Status != "applied" || len(exec.calls) != 1 || !reflect.DeepEqual(exec.calls[0].Argv, []string{"/usr/local/bin/brew", "install", "jq"}) {
					t.Fatalf("fallback outcome: %+v calls=%v", step, exec.calls)
				}
			} else {
				reason := map[string]string{"missing": "package_manager_missing", "closed": "package_manager_unverifiable", "cancelled": "cancelled"}[mode]
				if step.Reason != reason || len(exec.calls) != 0 {
					t.Fatalf("unsafe engine outcome: %+v calls=%v", step, exec.calls)
				}
				if mode == "cancelled" && cancellingFS.calls != 1 {
					t.Fatal("continued availability probes after cancellation")
				}
			}
		})
	}
}
