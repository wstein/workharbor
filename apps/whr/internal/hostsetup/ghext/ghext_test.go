// SPDX-License-Identifier: EUPL-1.2
package ghext

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
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
		{"measured gh 2.103.3 gh-stack 0.3.0", "gh stack\tgithub/gh-stack\tv0.3.0\n", "v0.3.0", true},
		{"space separated is not the gh format", "gh stack  github/gh-stack  v0.3.0\n", "", false},
		{"crlf", "gh stack\tgithub/gh-stack\tv0.3.0\r\n", "v0.3.0", true},
		{"crlf without version", "gh stack\tgithub/gh-stack\r\n", "", true},
		{"several extensions", "gh dash\tdlvhdr/gh-dash\tv4.0.0\ngh stack\tgithub/gh-stack\tv0.3.0\n", "v0.3.0", true},
		{"blank lines", "\n\ngh stack\tgithub/gh-stack\tv0.3.0\n\n", "v0.3.0", true},
		{"suffix repo is another repo", "x\tother/github/gh-stack\tv1\n", "", false},
		{"repo in name column only", "github/gh-stack\tgh stack\tv0.3.0\n", "", false},
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
		Run: func(_ context.Context, _ []string, name string, args ...string) ([]byte, []byte, error) {
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
	// Each text matches exactly one marker, so removing that marker fails its case.
	for _, stderr := range []string{
		"Post \"https://api.github.com/graphql\": lookup api.github.com: no such host",
		"fatal: unable to access 'https://github.com/': Could not resolve host: github.com",
		"read tcp 140.82.112.3:443: i/o timeout",
		"connect: network is unreachable",
		"connect: connection refused",
		"net/http: TLS handshake timeout",
		"fatal: unable to access: Temporary failure in name resolution",
		"dial tcp 140.82.112.3:443: something odd",
		"error connecting to does-not-exist.invalid",
		"check your internet connection or https://githubstatus.com",
	} {
		g := ExecGH{Run: func(context.Context, []string, string, ...string) ([]byte, []byte, error) {
			return nil, []byte(stderr), errors.New("exit status 1")
		}}
		var ne *NetworkError
		if err := g.ExtensionInstall(context.Background(), Repo); !errors.As(err, &ne) {
			t.Errorf("%q: not a network error: %v", stderr, err)
		}
	}
	g := ExecGH{Run: func(context.Context, []string, string, ...string) ([]byte, []byte, error) {
		return nil, []byte("extension already installed"), errors.New("exit status 1")
	}}
	var ne *NetworkError
	err := g.ExtensionInstall(context.Background(), Repo)
	if err == nil || errors.As(err, &ne) || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestInteractiveWithoutConfirmFuncInstallsNothing(t *testing.T) {
	f := &fakeGH{lists: []string{""}}
	r, _ := run(f, Options{Selected: true, Interactive: true, Confirm: nil})
	if r.State != "blocked" || r.Reason != "confirmation_required" || f.installs() != 0 {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
}

func TestYesDoesNotOverrideDeselection(t *testing.T) {
	f := &fakeGH{lists: []string{""}}
	r, _ := run(f, Options{Selected: false, Yes: true, Interactive: true, Confirm: yes})
	if r.State != "skipped" || len(f.calls) != 0 {
		t.Fatalf("r=%+v calls=%v", r, f.calls)
	}
}

func TestPresentWithoutVersionSaysVersionUnknown(t *testing.T) {
	f := &fakeGH{lists: []string{"gh stack\tgithub/gh-stack\n"}}
	r, out := run(f, Options{Selected: true, Interactive: true, Confirm: yes})
	if r.State != "present" || r.Version != "" || !strings.Contains(out, "version unknown") {
		t.Fatalf("r=%+v out=%q", r, out)
	}
}

func TestCleanEnvPinsGitHubHostAndDropsRepo(t *testing.T) {
	got := cleanEnv([]string{"PATH=/bin", "GH_HOST=evil.example", "GH_REPO=a/b", "HOME=/h", "GH_TOKEN=t"})
	want := []string{"PATH=/bin", "HOME=/h", "GH_TOKEN=t", "GH_HOST=github.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env %v want %v", got, want)
	}
}

func TestExecGHPassesCleanEnvToTheCommand(t *testing.T) {
	var env []string
	g := ExecGH{
		Environ: func() []string { return []string{"GH_HOST=evil.example", "GH_REPO=a/b", "X=1"} },
		Run: func(_ context.Context, e []string, _ string, _ ...string) ([]byte, []byte, error) {
			env = e
			return nil, nil, nil
		},
	}
	if err := g.ExtensionInstall(context.Background(), Repo); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(env, []string{"X=1", "GH_HOST=github.com"}) {
		t.Fatalf("env = %v", env)
	}
}

// fakeGHScript puts a stand-in `gh` shell script first on PATH. It is never the real gh.
func fakeGHScript(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
}

func TestDefaultRunnerUsesCleanEnvironment(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.txt")
	fakeGHScript(t, `echo "$GH_HOST|$GH_REPO|$*" > `+out)
	t.Setenv("GH_HOST", "evil.example")
	t.Setenv("GH_REPO", "a/b")
	if err := (ExecGH{}).ExtensionInstall(context.Background(), Repo); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if got := strings.TrimSpace(string(b)); got != "github.com||extension install github/gh-stack" {
		t.Fatalf("child saw %q", got)
	}
}

func TestDefaultRunnerStopsOnTimeoutEvenIfChildHoldsPipes(t *testing.T) {
	fakeGHScript(t, "sleep 30 &\nsleep 30")
	g := ExecGH{ListTimeout: 200 * time.Millisecond, InstallTimeout: 200 * time.Millisecond}
	start := time.Now()
	_, err := g.ExtensionList(context.Background())
	if err == nil || time.Since(start) > 10*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
	start = time.Now()
	if err := g.ExtensionInstall(context.Background(), Repo); err == nil || time.Since(start) > 10*time.Second {
		t.Fatalf("install err=%v after %v", err, time.Since(start))
	}
}

func TestDefaultRunnerStopsOnContextCancel(t *testing.T) {
	fakeGHScript(t, "sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := (ExecGH{}).ExtensionList(ctx); err == nil || time.Since(start) > 10*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}
