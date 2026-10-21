// SPDX-License-Identifier: EUPL-1.2
package detect

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"time"
)

func TestBrewExecutableFixedPathsAndLinks(t *testing.T) {
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		t.Run(p, func(t *testing.T) {
			f := &fakeFS{files: map[string]File{p: {Kind: "regular", Executable: true}}}
			d := Detector{FS: f, Clock: &fakeClock{now: time.Unix(0, 0)}}
			yes, err := d.BrewExecutable(p)
			if !yes || err != nil {
				t.Fatalf("fixed executable unavailable: %v %v", yes, err)
			}
		})
	}
	for _, tc := range []struct {
		name, target string
		ancestor     bool
	}{
		{"relative", "../Homebrew/bin/brew", false}, {"absolute", "/opt/homebrew/Homebrew/bin/brew", false}, {"ancestor", "/opt/homebrew/Homebrew/bin", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := "/opt/homebrew/bin/brew"
			link := p
			if tc.ancestor {
				link = "/opt/homebrew/bin"
			}
			f := &fakeFS{files: map[string]File{link: {Kind: "symlink"}, "/opt/homebrew/Homebrew/bin/brew": {Kind: "regular", Executable: true}}, links: map[string]string{link: tc.target}}
			d := Detector{FS: f, Clock: &fakeClock{now: time.Unix(0, 0)}}
			yes, err := d.BrewExecutable(p)
			if !yes || err != nil {
				t.Fatalf("resolved executable unavailable: %v %v calls=%v", yes, err, f.calls)
			}
			found := false
			for _, call := range f.calls {
				if call == "lstat /opt/homebrew/Homebrew/bin/brew" {
					found = true
				}
			}
			if !found {
				t.Fatalf("target not observed: %v", f.calls)
			}
		})
	}
}
func TestBrewExecutableRefusesOtherPathsAndMissingPorts(t *testing.T) {
	f := &fakeFS{files: map[string]File{}}
	d := Detector{FS: f, Clock: &fakeClock{now: time.Unix(0, 0)}}
	for _, p := range []string{"brew", "/tmp/brew", "/opt/homebrew/bin/../bin/brew", "/opt/homebrew/bin/brew/", "/opt/homebrew/bin/brew\x00"} {
		if yes, err := d.BrewExecutable(p); yes || err == nil {
			t.Fatalf("accepted unapproved path %q", p)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("unapproved paths touched FS: %v", f.calls)
	}
	for _, empty := range []Detector{{}, {FS: f}, {Clock: d.Clock}} {
		if yes, err := empty.BrewExecutable("/opt/homebrew/bin/brew"); yes || err == nil {
			t.Fatal("missing ports reported usable")
		}
	}
}
func TestBrewExecutableMissingErrorsAndBudget(t *testing.T) {
	for _, tc := range []struct {
		name, kind   string
		exec, denied bool
		link         string
		tick         time.Duration
		wantErr      error
	}{
		{name: "absent"}, {name: "non-executable", kind: "regular"}, {name: "directory", kind: "directory"},
		{name: "special", kind: "special", wantErr: fs.ErrInvalid}, {name: "permission", denied: true, wantErr: fs.ErrPermission},
		{name: "cycle", kind: "symlink", link: "brew", wantErr: fs.ErrInvalid},
		{name: "late-success", kind: "regular", exec: true, tick: 5 * time.Second, wantErr: context.DeadlineExceeded},
		{name: "late-absence", tick: 5 * time.Second, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := "/opt/homebrew/bin/brew"
			clock := &fakeClock{now: time.Unix(0, 0)}
			f := &fakeFS{files: map[string]File{}, links: map[string]string{}, clock: clock, tickPath: p, tick: tc.tick, denied: tc.denied}
			if tc.kind != "" {
				f.files[p] = File{Kind: tc.kind, Executable: tc.exec}
			}
			if tc.link != "" {
				f.links[p] = tc.link
			}
			d := Detector{FS: f, Clock: clock}
			yes, err := d.BrewExecutable(p)
			if yes || !errors.Is(err, tc.wantErr) {
				t.Fatalf("availability/error mismatch: %v %v want %v; calls=%v", yes, err, tc.wantErr, f.calls)
			}
		})
	}
}
func TestBrewExecutableOnlyReadsDeclaredMetadata(t *testing.T) {
	p := "/opt/homebrew/bin/brew"
	f := &fakeFS{files: map[string]File{p: {Kind: "regular", Executable: true}}}
	clock := &fakeClock{now: time.Unix(0, 0)}
	d := Detector{FS: f, Clock: clock}
	_, _ = d.BrewExecutable(p)
	if !reflect.DeepEqual(f.calls, []string{"lstat /opt", "lstat /opt/homebrew", "lstat /opt/homebrew/bin", "lstat /opt/homebrew/bin/brew"}) {
		t.Fatalf("unexpected metadata reads: %v", f.calls)
	}
}
