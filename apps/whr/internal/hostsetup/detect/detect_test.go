// SPDX-License-Identifier: EUPL-1.2
package detect

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
)

type fakeFS struct {
	files      map[string]File
	links      map[string]string
	calls      []string
	denied     bool
	clock      *fakeClock
	tickPath   string
	tick       time.Duration
	cancelPath string
	cancel     context.CancelFunc
}

func (f *fakeFS) Lstat(p string) (File, error) {
	f.calls = append(f.calls, "lstat "+p)
	if f.cancel != nil && p == f.cancelPath {
		f.cancel()
	}
	if f.clock != nil && p == f.tickPath {
		f.clock.now = f.clock.now.Add(f.tick)
	}
	if f.denied {
		return File{}, fs.ErrPermission
	}
	v, ok := f.files[p]
	if !ok {
		// Model actual directory entries explicitly implied by existing fixture
		// files and fixed search roots; explicit symlinks override these parents.
		if p == "/" {
			return File{Kind: "directory"}, nil
		}
		candidates := strings.Split(FixedPATH, ":")
		for file := range f.files {
			candidates = append(candidates, filepath.Dir(file))
		}
		for _, candidate := range candidates {
			for dir := candidate; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
				if p == dir {
					return File{Kind: "directory"}, nil
				}
			}
		}
		return File{}, fs.ErrNotExist
	}
	return v, nil
}
func (f *fakeFS) Readlink(p string) (string, error) {
	f.calls = append(f.calls, "readlink "+p)
	v, ok := f.links[p]
	if !ok {
		return "", fs.ErrInvalid
	}
	return v, nil
}

type fakeExec struct {
	calls   []Probe
	output  Output
	err     error
	t       *testing.T
	clock   *fakeClock
	elapsed time.Duration
}

func (f *fakeExec) Run(_ context.Context, p Probe) (Output, error) {
	f.calls = append(f.calls, p)
	allowed := map[string]bool{"/opt/homebrew/Cellar/jq/1.7/bin/jq": true, "/usr/local/bin/jq": true, "/opt/homebrew/Cellar/jq-foreign/1.7/bin/jq": true, "/Library/Developer/CommandLineTools/usr/bin/git": true}
	if !allowed[p.Executable] || !reflect.DeepEqual(p.Args, []string{"--version"}) || p.Env["PATH"] != FixedPATH || p.Env["LC_ALL"] != "C" || len(p.Env) != 2 || p.Timeout <= 0 || p.Timeout > 5*time.Second {
		f.t.Fatalf("unexpected fake probe: %#v", p)
	}
	if f.clock != nil {
		f.clock.now = f.clock.now.Add(f.elapsed)
	}
	return f.output, f.err
}

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }
func fixture(t *testing.T) (Detector, *fakeFS, *fakeExec, catalogue.Item) {
	f := &fakeFS{files: map[string]File{"/opt/homebrew/bin/jq": {Kind: "symlink"}, "/opt/homebrew/Cellar/jq/1.7/bin/jq": {Kind: "regular", Executable: true}}, links: map[string]string{"/opt/homebrew/bin/jq": "../Cellar/jq/1.7/bin/jq"}}
	e := &fakeExec{t: t, output: Output{Stdout: "jq-1.7\n"}}
	d := Detector{FS: f, Exec: e, Clock: &fakeClock{now: time.Unix(0, 0)}}
	item := catalogue.Item{ID: "jq", Kind: "tool", SelectedByDefault: true, Tool: catalogue.Tool{Command: "jq", VersionArgs: []string{"--version"}, VersionRegex: `([0-9]+\.[0-9]+)`, MinVersion: "1.0", Source: catalogue.Source{Manager: "homebrew", Type: "formula", Name: "jq", Tap: "homebrew/core"}}}
	return d, f, e, item
}
func TestDetectStates(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*fakeFS, *fakeExec, *catalogue.Item)
	}{
		{"ok", "present_ok", func(*fakeFS, *fakeExec, *catalogue.Item) {}},
		{"absent", "absent", func(f *fakeFS, _ *fakeExec, _ *catalogue.Item) { f.files = map[string]File{} }},
		{"outdated", "present_outdated", func(_ *fakeFS, _ *fakeExec, i *catalogue.Item) { i.Tool.MinVersion = "2.0" }},
		{"equal", "present_ok", func(_ *fakeFS, _ *fakeExec, i *catalogue.Item) { i.Tool.MinVersion = "1.7.0" }},
		{"other_source", "present_other_source", func(f *fakeFS, _ *fakeExec, _ *catalogue.Item) {
			f.files = map[string]File{"/usr/local/bin/jq": {Kind: "regular", Executable: true}}
		}},
		{"unparseable", "present_unverifiable", func(_ *fakeFS, e *fakeExec, _ *catalogue.Item) { e.output.Stdout = "not a version" }},
		{"failed_probe", "present_unverifiable", func(_ *fakeFS, e *fakeExec, _ *catalogue.Item) { e.err = errors.New("probe failed") }},
		{"denied", "unknown", func(f *fakeFS, _ *fakeExec, _ *catalogue.Item) { f.denied = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, f, e, i := fixture(t)
			c.change(f, e, &i)
			got := d.Detect(context.Background(), i)
			if got.State != c.want {
				t.Fatalf("state=%s want%s", got.State, c.want)
			}
			if c.name == "ok" && !got.Managed {
				t.Fatal("Cellar symlink not recognized")
			}
		})
	}
}
func TestDetectIsReadOnlyAndNeverRunsBrew(t *testing.T) {
	d, f, e, i := fixture(t)
	_ = d.Detect(context.Background(), i)
	if len(e.calls) != 1 {
		t.Fatalf("probe calls=%d want1", len(e.calls))
	}
	p := e.calls[0]
	if p.Executable != "/opt/homebrew/Cellar/jq/1.7/bin/jq" || !reflect.DeepEqual(p.Args, []string{"--version"}) || p.Env["PATH"] != FixedPATH || p.Timeout != 5*time.Second {
		t.Fatalf("unexpected fixed probe: %#v", p)
	}
	if len(p.Env) != 2 || p.Env["LC_ALL"] != "C" {
		t.Fatal("unreviewed environment passed")
	}
	i.Tool.Command = "brew"
	e.calls = nil
	f.files["/opt/homebrew/bin/brew"] = File{Kind: "regular", Executable: true}
	_ = d.Detect(context.Background(), i)
	if len(e.calls) != 0 {
		t.Fatal("brew executed")
	}
	d, f, e, i = fixture(t)
	f.links["/opt/homebrew/bin/jq"] = "brew"
	f.files["/opt/homebrew/bin/brew"] = File{Kind: "regular", Executable: true}
	if got := d.Detect(context.Background(), i); got.State != "unknown" || len(e.calls) != 0 {
		t.Fatal("brew alias executed")
	}

}
func TestDetectCLTGitShimNotExecuted(t *testing.T) {
	d, f, e, i := fixture(t)
	i.ID = "git"
	i.Tool.Command = "git"
	f.files = map[string]File{"/usr/bin/git": {Kind: "regular", Executable: true}}
	e.calls = nil
	got := d.Detect(context.Background(), i)
	if got.State != "absent" || len(e.calls) != 0 {
		t.Fatalf("shim probe: %#v calls%d", got, len(e.calls))
	}
	d, f, e, i = fixture(t)
	i.ID = "git"
	i.Tool.Command = "git"
	f.files = map[string]File{"/opt/homebrew/bin/git": {Kind: "symlink"}, "/usr/bin/git": {Kind: "regular", Executable: true}}
	f.links = map[string]string{"/opt/homebrew/bin/git": "/usr/bin/git"}
	got = d.Detect(context.Background(), i)
	if got.State != "absent" || len(e.calls) != 0 {
		t.Fatal("shim alias executed")
	}

}

func TestDetectBudgetsSymlinksAndSources(t *testing.T) {
	t.Run("hostile user PATH ignored", func(t *testing.T) {
		t.Setenv("PATH", "/tmp/untrusted:.:/opt/fake")
		d, f, e, i := fixture(t)
		got := d.Detect(context.Background(), i)
		if got.State != "present_ok" || len(e.calls) != 1 || e.calls[0].Env["PATH"] != FixedPATH {
			t.Fatal("user PATH influenced probe")
		}
		for _, call := range f.calls {
			if strings.Contains(call, "untrusted") || strings.Contains(call, "/opt/fake") {
				t.Fatal("user PATH searched")
			}
		}
	})
	t.Run("symlink cycle", func(t *testing.T) {
		d, f, e, i := fixture(t)
		f.links["/opt/homebrew/bin/jq"] = "jq"
		got := d.Detect(context.Background(), i)
		if got.State != "unknown" || len(e.calls) != 0 {
			t.Fatal("cyclic path executed")
		}
	})
	t.Run("special candidate", func(t *testing.T) {
		d, f, e, i := fixture(t)
		f.files["/opt/homebrew/bin/jq"] = File{Kind: "special"}
		if d.Detect(context.Background(), i).State != "unknown" || len(e.calls) != 0 {
			t.Fatal("special path executed")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		d, _, e, i := fixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if d.Detect(ctx, i).State != "unknown" || len(e.calls) != 0 {
			t.Fatal("cancelled probe ran")
		}
	})
	t.Run("stdout bound", func(t *testing.T) {
		d, _, e, i := fixture(t)
		e.output.Stdout = strings.Repeat("x", 65537)
		if d.Detect(context.Background(), i).State != "present_unverifiable" {
			t.Fatal("unbounded output accepted")
		}
	})
	t.Run("nonzero probe", func(t *testing.T) {
		d, _, e, i := fixture(t)
		e.output.Exit = 1
		if d.Detect(context.Background(), i).State != "present_unverifiable" {
			t.Fatal("failed probe passed")
		}
	})
	t.Run("numeric version not lexical", func(t *testing.T) {
		d, _, e, i := fixture(t)
		e.output.Stdout = "jq-1.10"
		i.Tool.MinVersion = "1.9"
		if d.Detect(context.Background(), i).State != "present_ok" {
			t.Fatal("lexical version comparison")
		}
	})
	t.Run("cask layout", func(t *testing.T) {
		d, f, _, i := fixture(t)
		i.Tool.Source.Type = "cask"
		f.files["/opt/homebrew/Caskroom/jq"] = File{Kind: "directory"}
		if got := d.Detect(context.Background(), i); got.State != "present_ok" || !got.Managed {
			t.Fatal("cask layout not recognized")
		}
	})
	t.Run("Cellar name prefix cannot alias", func(t *testing.T) {
		d, f, _, i := fixture(t)
		f.links["/opt/homebrew/bin/jq"] = "../Cellar/jq-foreign/1.7/bin/jq"
		f.files["/opt/homebrew/Cellar/jq-foreign/1.7/bin/jq"] = File{Kind: "regular", Executable: true}
		if d.Detect(context.Background(), i).State != "present_other_source" {
			t.Fatal("foreign source name accepted")
		}
	})
	t.Run("installed CLT real git", func(t *testing.T) {
		d, f, e, i := fixture(t)
		i.ID = "git"
		i.Tool.Command = "git"
		f.files = map[string]File{"/usr/bin/git": {Kind: "regular", Executable: true}, "/Library/Developer/CommandLineTools/usr/bin/git": {Kind: "regular", Executable: true}}
		_ = d.Detect(context.Background(), i)
		if len(e.calls) != 1 || e.calls[0].Executable != "/Library/Developer/CommandLineTools/usr/bin/git" {
			t.Fatal("real Git not selected without executing shim")
		}
	})
}

func TestDetectProbeDeadlineIsNeverOK(t *testing.T) {
	d, _, e, i := fixture(t)
	e.clock = d.Clock.(*fakeClock)
	e.elapsed = 6 * time.Second
	if got := d.Detect(context.Background(), i); got.State != "present_unverifiable" {
		t.Fatalf("late probe reported %s", got.State)
	}
}

func TestDetectSourceLookupConsumesProbeBudget(t *testing.T) {
	d, f, _, i := fixture(t)
	i.Tool.Source.Type = "cask"
	f.files["/opt/homebrew/Caskroom/jq"] = File{Kind: "directory"}
	f.clock = d.Clock.(*fakeClock)
	f.tickPath = "/opt/homebrew/Caskroom/jq"
	f.tick = 6 * time.Second
	if got := d.Detect(context.Background(), i); got.State != "present_unverifiable" {
		t.Fatalf("late source evidence reported %s", got.State)
	}
}

func TestDetectAncestorDirectoriesResolvedBeforeProbe(t *testing.T) {
	t.Run("Homebrew opt directory", func(t *testing.T) {
		d, f, e, i := fixture(t)
		f.links["/opt/homebrew/bin/jq"] = "../opt/jq/bin/jq"
		f.files["/opt/homebrew/opt/jq"] = File{Kind: "symlink"}
		f.links["/opt/homebrew/opt/jq"] = "../Cellar/jq/1.7"
		f.files["/opt/homebrew/opt/jq/bin/jq"] = File{Kind: "regular", Executable: true}
		got := d.Detect(context.Background(), i)
		if got.State != "present_ok" || !got.Managed || got.Path != "/opt/homebrew/Cellar/jq/1.7/bin/jq" || len(e.calls) != 1 {
			t.Fatalf("ancestor source not resolved: %#v", got)
		}
	})
	t.Run("Git shim directory alias", func(t *testing.T) {
		d, f, e, i := fixture(t)
		i.ID = "git"
		i.Tool.Command = "git"
		f.files = map[string]File{"/opt/homebrew/bin": {Kind: "symlink"}, "/opt/homebrew/bin/git": {Kind: "regular", Executable: true}, "/usr/bin/git": {Kind: "regular", Executable: true}}
		f.links = map[string]string{"/opt/homebrew/bin": "/usr/bin"}
		got := d.Detect(context.Background(), i)
		if got.State != "absent" || len(e.calls) != 0 {
			t.Fatalf("directory alias dispatched shim: %#v", got)
		}
	})
	t.Run("directory cycle", func(t *testing.T) {
		d, f, e, i := fixture(t)
		f.files["/opt/homebrew/bin"] = File{Kind: "symlink"}
		f.links["/opt/homebrew/bin"] = "bin"
		got := d.Detect(context.Background(), i)
		if got.State != "unknown" || len(e.calls) != 0 {
			t.Fatal("ancestor cycle accepted")
		}
	})
}

func TestDetectTerminalBudgetAndCancellation(t *testing.T) {
	t.Run("final missing metadata consumes deadline", func(t *testing.T) {
		d, f, e, i := fixture(t)
		f.files = map[string]File{}
		f.clock = d.Clock.(*fakeClock)
		f.tickPath = "/sbin/jq"
		f.tick = 6 * time.Second
		got := d.Detect(context.Background(), i)
		if got.State != "unknown" || len(e.calls) != 0 {
			t.Fatalf("late negative evidence authorized install: %#v", got)
		}
	})
	t.Run("cancel before dispatch", func(t *testing.T) {
		d, f, e, i := fixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.cancel = cancel
		f.cancelPath = "/opt/homebrew/Cellar/jq/1.7/bin/jq"
		got := d.Detect(ctx, i)
		if got.State != "unknown" || len(e.calls) != 0 {
			t.Fatal("probe dispatched after metadata cancelled context")
		}
	})
	t.Run("cancel during cask metadata", func(t *testing.T) {
		d, f, e, i := fixture(t)
		i.Tool.Source.Type = "cask"
		f.files["/opt/homebrew/Caskroom/jq"] = File{Kind: "directory"}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.cancel = cancel
		f.cancelPath = "/opt/homebrew/Caskroom/jq"
		got := d.Detect(ctx, i)
		if got.State != "present_unverifiable" || len(e.calls) != 1 {
			t.Fatalf("cancelled source evidence accepted: %#v", got)
		}
	})
}

type fakeMetadata struct {
	value   string
	err     error
	calls   []time.Duration
	clock   *fakeClock
	elapsed time.Duration
	cancel  context.CancelFunc
}

func (f *fakeMetadata) OrbStackAppVersion(ctx context.Context, budget time.Duration) (string, error) {
	f.calls = append(f.calls, budget)
	if f.clock != nil {
		f.clock.now = f.clock.now.Add(f.elapsed)
	}
	if f.cancel != nil {
		f.cancel()
	}
	return f.value, f.err
}
func TestDetectOrbStackMetadataPresenceSourceAndBounds(t *testing.T) {
	initial, err := catalogue.LoadInitial()
	if err != nil {
		t.Fatal(err)
	}
	approved := initial.Items[0]
	if approved.ID != "orbstack" || approved.Tool.MinVersion != "2.2.3" || approved.Tool.VersionMetadata != catalogue.OrbStackAppMetadata {
		t.Fatal("wrong approved production item")
	}
	cases := []struct {
		name, want string
		change     func(*fakeFS, *fakeMetadata, *catalogue.Item)
		calls      int
	}{
		{"app_2_2_4_above_floor", "present_ok", func(*fakeFS, *fakeMetadata, *catalogue.Item) {}, 1},
		{"below_app_floor", "present_outdated", func(_ *fakeFS, m *fakeMetadata, _ *catalogue.Item) { m.value = "2.2.2\n" }, 1},
		{"metadata_without_presence", "absent", func(f *fakeFS, _ *fakeMetadata, _ *catalogue.Item) { f.files = map[string]File{} }, 0},
		{"registration_without_app", "absent", func(f *fakeFS, _ *fakeMetadata, _ *catalogue.Item) { delete(f.files, OrbStackInfoPlist) }, 0},
		{"no_registration", "present_other_source", func(f *fakeFS, _ *fakeMetadata, _ *catalogue.Item) {
			delete(f.files, "/opt/homebrew/Caskroom/orbstack")
		}, 1},
		{"app_parent_alias", "unknown", func(f *fakeFS, _ *fakeMetadata, _ *catalogue.Item) {
			f.files["/Applications/OrbStack.app"] = File{Kind: "symlink"}
		}, 0},
		{"plist_alias", "unknown", func(f *fakeFS, _ *fakeMetadata, _ *catalogue.Item) {
			f.files[OrbStackInfoPlist] = File{Kind: "symlink"}
		}, 0},
		{"source_parent_alias", "present_unverifiable", func(f *fakeFS, _ *fakeMetadata, _ *catalogue.Item) {
			f.files["/opt/homebrew/Caskroom"] = File{Kind: "symlink"}
		}, 1},
		{"unparseable", "present_unverifiable", func(_ *fakeFS, m *fakeMetadata, _ *catalogue.Item) { m.value = "private-value" }, 1},
		{"output_cap", "present_unverifiable", func(_ *fakeFS, m *fakeMetadata, _ *catalogue.Item) { m.value = strings.Repeat("1", 65537) }, 1},
		{"metadata_error", "present_unverifiable", func(_ *fakeFS, m *fakeMetadata, _ *catalogue.Item) { m.err = errors.New("private-value") }, 1},
		{"late_metadata", "present_unverifiable", func(_ *fakeFS, m *fakeMetadata, _ *catalogue.Item) { m.elapsed = 5 * time.Second }, 1},
		{"source_deadline", "present_unverifiable", func(f *fakeFS, m *fakeMetadata, _ *catalogue.Item) {
			f.clock = m.clock
			f.tickPath = "/opt/homebrew/Caskroom/orbstack"
			f.tick = 5 * time.Second
		}, 1},
		{"cancelled_metadata", "present_unverifiable", func(*fakeFS, *fakeMetadata, *catalogue.Item) {}, 1},
		{"already_cancelled", "unknown", func(*fakeFS, *fakeMetadata, *catalogue.Item) {}, 0},
		{"unknown_route", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.VersionMetadata = "arbitrary" }, 0},
		{"conflicting_command", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.Command = "orb" }, 0},
		{"other_cask", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.Source.Name = "other" }, 0},
		{"other_item_id", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.ID = "jq" }, 0},
		{"other_item_kind", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Kind = "tool" }, 0},
		{"conflicting_version_args", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.VersionArgs = []string{"version"} }, 0},
		{"other_source_manager", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.Source.Manager = "other" }, 0},
		{"other_source_type", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.Source.Type = "formula" }, 0},
		{"other_source_tap", "unknown", func(_ *fakeFS, _ *fakeMetadata, i *catalogue.Item) { i.Tool.Source.Tap = "other/tap" }, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, f, exec, _ := fixture(t)
			f.files = map[string]File{OrbStackInfoPlist: {Kind: "regular"}, "/opt/homebrew/Caskroom/orbstack": {Kind: "directory"}}
			item := approved
			metadata := &fakeMetadata{value: "2.2.4\n", clock: d.Clock.(*fakeClock)}
			d.Metadata = metadata
			tc.change(f, metadata, &item)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.name == "cancelled_metadata" {
				metadata.cancel = cancel
			}
			if tc.name == "already_cancelled" {
				cancel()
			}
			got := d.Detect(ctx, item)
			if got.State != tc.want || len(metadata.calls) != tc.calls || len(exec.calls) != 0 {
				t.Fatalf("metadata result=%+v calls=%v exec=%d want=%s/%d", got, metadata.calls, len(exec.calls), tc.want, tc.calls)
			}
			for _, budget := range metadata.calls {
				if budget <= 0 || budget > 5*time.Second {
					t.Fatalf("invalid budget: %v", budget)
				}
			}
			if tc.want == "present_ok" && (!got.Managed || got.Version != "2.2.4" || got.Path != OrbStackInfoPlist) {
				t.Fatalf("wrong application evidence: %+v", got)
			}
			if strings.Contains(got.Reason, "private-value") {
				t.Fatal("raw metadata error leaked")
			}
		})
	}
}
