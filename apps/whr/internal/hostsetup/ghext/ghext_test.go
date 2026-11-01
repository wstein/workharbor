// SPDX-License-Identifier: EUPL-1.2
package ghext

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeGH struct {
	out            *bytes.Buffer
	availErr       error
	lists          []string // consumed per ExtensionList call, last one repeats
	listErr        error
	installErr     error
	calls          []string
	shownAtInstall string
}

func (f *fakeGH) Available(context.Context) error {
	f.calls = append(f.calls, "available")
	return f.availErr
}
func (f *fakeGH) ExtensionList(context.Context) (string, error) {
	f.calls = append(f.calls, "list")
	if f.listErr != nil {
		return "", f.listErr
	}
	if len(f.lists) == 0 {
		return "", nil
	}
	s := f.lists[0]
	if len(f.lists) > 1 {
		f.lists = f.lists[1:]
	}
	return s, nil
}
func (f *fakeGH) ExtensionInstall(_ context.Context, repo string) error {
	f.calls = append(f.calls, "install "+repo)
	if f.out != nil {
		f.shownAtInstall = f.out.String()
	}
	return f.installErr
}
func (f *fakeGH) installs() int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "install") {
			n++
		}
	}
	return n
}

const stackLine = "gh stack\tgithub/gh-stack\tv0.3.0\n"

func run(f *fakeGH, opt Options) (Result, string) {
	var out bytes.Buffer
	f.out = &out
	opt.Out = &out
	if !opt.Selected {
		// callers set Selected explicitly; helper default is selected
	}
	return Run(context.Background(), f, opt), out.String()
}

func yes(string) (bool, error) { return true, nil }

func TestItemPreselectedAndSkippable(t *testing.T) {
	it := Describe()
	if it.ID != "gh-stack" || !it.SelectedByDefault || !it.Skippable || it.Requires != "gh" {
		t.Fatalf("item = %+v", it)
	}
}

func TestDeselectedDoesNothing(t *testing.T) {
	f := &fakeGH{}
	r, _ := run(f, Options{Selected: false, Interactive: true, Confirm: yes})
	if r.State != "skipped" || len(f.calls) != 0 {
		t.Fatalf("state %q calls %v", r.State, f.calls)
	}
}

func TestExistingInstallIsDetectedAndNotInstalledTwice(t *testing.T) {
	f := &fakeGH{lists: []string{stackLine}}
	asked := false
	r, out := run(f, Options{Selected: true, Interactive: true, Confirm: func(string) (bool, error) { asked = true; return true, nil }})
	if r.State != "present" || r.Version != "v0.3.0" || f.installs() != 0 || asked {
		t.Fatalf("r=%+v calls=%v asked=%v", r, f.calls, asked)
	}
	if !strings.Contains(out, "v0.3.0") {
		t.Fatalf("version not reported: %q", out)
	}
}

func TestOtherExtensionsAndForeignStackRepoDoNotCount(t *testing.T) {
	list := "gh dash\tdlvhdr/gh-dash\tv4.0.0\ngh stack\tsomeone/gh-stack\tv1.0.0\n"
	f := &fakeGH{lists: []string{list, list + stackLine}}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "installed" || f.installs() != 1 {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
}

func TestStepIsShownBeforeInstallAndVersionReported(t *testing.T) {
	f := &fakeGH{lists: []string{"", stackLine}}
	r, out := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "installed" || r.Version != "v0.3.0" {
		t.Fatalf("r=%+v", r)
	}
	if !strings.Contains(f.shownAtInstall, "gh extension install github/gh-stack") || !strings.Contains(f.shownAtInstall, "gh profile") {
		t.Fatalf("step not shown before install: %q", f.shownAtInstall)
	}
	if !strings.Contains(out, "v0.3.0") {
		t.Fatalf("version missing: %q", out)
	}
}

func TestDeclinedConfirmationInstallsNothing(t *testing.T) {
	f := &fakeGH{lists: []string{""}}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: func(string) (bool, error) { return false, nil }})
	if r.State != "declined" || f.installs() != 0 {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
}

func TestConfirmationErrorInstallsNothing(t *testing.T) {
	f := &fakeGH{lists: []string{""}}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: func(string) (bool, error) { return false, errors.New("EOF") }})
	if r.State != "cancelled" || f.installs() != 0 {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
}

func TestNonInteractiveNeedsExplicitFlag(t *testing.T) {
	f := &fakeGH{lists: []string{""}}
	r, out := run(f, Options{Selected: true, Interactive: false, Confirm: yes})
	if r.State != "blocked" || r.Reason != "confirmation_required" || f.installs() != 0 || !strings.Contains(out, "gh extension install github/gh-stack") {
		t.Fatalf("r=%+v calls=%v out=%q", r, f.calls, out)
	}
	f = &fakeGH{lists: []string{"", stackLine}}
	called := false
	r, _ = run(f, Options{Selected: true, Interactive: false, Yes: true, Confirm: func(string) (bool, error) { called = true; return false, nil }})
	if r.State != "installed" || f.installs() != 1 || called {
		t.Fatalf("r=%+v calls=%v called=%v", r, f.calls, called)
	}
}

func TestMissingGHIsAClearMessage(t *testing.T) {
	f := &fakeGH{availErr: ErrGHMissing}
	r, out := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "blocked" || r.Reason != "gh_missing" || !strings.Contains(r.Message, "gh") || f.installs() != 0 || !strings.Contains(out, r.Message) {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
	if !reflect.DeepEqual(f.calls, []string{"available"}) {
		t.Fatalf("calls after missing gh: %v", f.calls)
	}
}

func TestNetworkFailureIsAClearMessage(t *testing.T) {
	f := &fakeGH{lists: []string{""}, installErr: &NetworkError{Detail: "dial tcp: no such host"}}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "failed" || r.Reason != "network_unavailable" || !strings.Contains(r.Message, "network") {
		t.Fatalf("r=%+v", r)
	}
}

func TestOtherInstallFailureKeepsReason(t *testing.T) {
	f := &fakeGH{lists: []string{""}, installErr: errors.New("boom")}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "failed" || r.Reason != "install_failed" || !strings.Contains(r.Message, "boom") {
		t.Fatalf("r=%+v", r)
	}
}

func TestListFailureStopsBeforeInstall(t *testing.T) {
	f := &fakeGH{listErr: errors.New("exit 1")}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "failed" || r.Reason != "detect_failed" || f.installs() != 0 {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
}

func TestInstalledButNotDetectedAfterwardsFails(t *testing.T) {
	f := &fakeGH{lists: []string{""}}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "failed" || r.Reason != "not_detected_after_install" {
		t.Fatalf("r=%+v", r)
	}
}

func TestSameResultOnHostAndInContainer(t *testing.T) {
	do := func() (Result, string) {
		f := &fakeGH{lists: []string{"", stackLine}}
		return run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	}
	r1, o1 := do()
	t.Setenv("REMOTE_CONTAINERS", "true")
	t.Setenv("container", "docker")
	t.Setenv("HOME", t.TempDir())
	r2, o2 := do()
	if r1 != r2 || o1 != o2 {
		t.Fatalf("differs: %+v/%q vs %+v/%q", r1, o1, r2, o2)
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		name, list, ver string
		ok              bool
	}{
		{"tab", stackLine, "v0.3.0", true},
		{"spaces", "gh stack  github/gh-stack  v0.3.0\n", "v0.3.0", true},
		{"pinned marker", "gh stack\tgithub/gh-stack\tv0.3.0\tpinned\n", "v0.3.0", true},
		{"no version", "gh stack\tgithub/gh-stack\n", "", true},
		{"sha version", "gh stack\tgithub/gh-stack\t1a2b3c4d\n", "1a2b3c4d", true},
		{"case", "gh stack\tGitHub/GH-Stack\tv0.3.0\n", "v0.3.0", true},
		{"absent", "gh dash\tdlvhdr/gh-dash\tv4\n", "", false},
		{"empty", "", "", false},
		{"substring repo", "x\tgithub/gh-stack-extra\tv1\n", "", false},
	}
	for _, c := range cases {
		v, ok := ParseVersion(c.list, Repo)
		if v != c.ver || ok != c.ok {
			t.Errorf("%s: got %q,%v want %q,%v", c.name, v, ok, c.ver, c.ok)
		}
	}
}

func TestExecGHArgvAndMissingBinary(t *testing.T) {
	var got [][]string
	g := ExecGH{
		LookPath: func(n string) (string, error) { return "/x/" + n, nil },
		Run: func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
			got = append(got, append([]string{name}, args...))
			return []byte(stackLine), nil, nil
		},
	}
	if err := g.Available(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out, err := g.ExtensionList(context.Background()); err != nil || out != stackLine {
		t.Fatalf("%q %v", out, err)
	}
	if err := g.ExtensionInstall(context.Background(), Repo); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"gh", "extension", "list"}, {"gh", "extension", "install", "github/gh-stack"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv %v want %v", got, want)
	}
	g.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := g.Available(context.Background()); !errors.Is(err, ErrGHMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestExecGHClassifiesNetworkFailures(t *testing.T) {
	for _, stderr := range []string{
		"Post \"https://api.github.com/graphql\": lookup api.github.com: no such host",
		"fatal: unable to access 'https://github.com/': Could not resolve host: github.com",
		"read tcp 140.82.112.3:443: i/o timeout",
		"connect: network is unreachable",
		"dial tcp 140.82.112.3:443: connect: connection refused",
	} {
		g := ExecGH{Run: func(context.Context, string, ...string) ([]byte, []byte, error) {
			return nil, []byte(stderr), errors.New("exit status 1")
		}}
		var ne *NetworkError
		if err := g.ExtensionInstall(context.Background(), Repo); !errors.As(err, &ne) {
			t.Errorf("%q: not a network error: %v", stderr, err)
		}
	}
	g := ExecGH{Run: func(context.Context, string, ...string) ([]byte, []byte, error) {
		return nil, []byte("extension already installed"), errors.New("exit status 1")
	}}
	var ne *NetworkError
	err := g.ExtensionInstall(context.Background(), Repo)
	if err == nil || errors.As(err, &ne) || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("err = %v", err)
	}
}
