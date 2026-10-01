package devcontainer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/runtime"
)

func baseSpec() runtime.Spec {
	return runtime.Spec{
		Image: "fedora", Owner: "o1", CPUs: 2, MemoryMB: 1024, DiskMB: 2048,
		Network: runtime.Network{Name: "wh-net-1", Internal: true},
		User:    "1000:1000", ReadOnlyRoot: true, CapDrop: []string{"ALL"}, Init: true, Tmpfs: []string{"/tmp"},
	}
}

func TestSpecKeepsTheSupervisorsHardening(t *testing.T) {
	env := Environment{Config: Config{Env: map[string]string{"GOFLAGS": "-mod=mod"}}}
	base := baseSpec()
	spec, err := env.Spec(base, "whr-env/o1:abc")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Image != "whr-env/o1:abc" || spec.Env["GOFLAGS"] != "-mod=mod" {
		t.Errorf("image or env missing: %+v", spec)
	}
	// Everything but the image and the variables is the supervisor's.
	spec.Image, spec.Env = base.Image, nil
	if !reflect.DeepEqual(spec, base) {
		t.Errorf("the environment changed more than the image and variables:\n got %+v\nwant %+v", spec, base)
	}
	if base.Env != nil {
		t.Error("the base spec was modified")
	}
	// The spec still has to be hardened: an image that is not a reference fails.
	if _, err := env.Spec(base, "--privileged"); !errors.Is(err, runtime.ErrInvalidSpec) {
		t.Errorf("err = %v, want ErrInvalidSpec", err)
	}
	// A reserved variable cannot get in even if the reader were bypassed.
	bad := Environment{Config: Config{Env: map[string]string{"HTTPS_PROXY": "http://evil"}}}
	if _, err := bad.Spec(base, "fedora"); !errors.Is(err, runtime.ErrInvalidSpec) {
		t.Errorf("a reserved variable: err = %v, want ErrInvalidSpec", err)
	}
}

func TestPostCreateRunsAsCommandsInsideTheEnvironment(t *testing.T) {
	env := Environment{Config: Config{PostCreate: []Command{{Shell: "go mod download"}, {Argv: []string{"make", "setup"}}, {}}}}
	got := env.PostCreate()
	want := []runtime.ExecRequest{{Cmd: []string{"sh", "-c", "go mod download"}}, {Cmd: []string{"make", "setup"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PostCreate = %+v", got)
	}
}

func TestPreviewPortsMergeForwardedAndHinted(t *testing.T) {
	env := Environment{Config: Config{ForwardPorts: []int{8080, 3000}, Hints: Hints{PreviewPorts: []int{3000, 9000}}}}
	if got := env.PreviewPorts(); !reflect.DeepEqual(got, []int{3000, 8080, 9000}) {
		t.Errorf("PreviewPorts = %v", got)
	}
}

func TestHostRequestsAreAskedNeverAssumed(t *testing.T) {
	env := Environment{
		Config:         Config{EgressRequests: []string{"proxy.golang.org", "example.org"}},
		SuggestedHosts: []string{"proxy.golang.org", "sum.golang.org", "registry.npmjs.org"},
	}
	got := env.HostRequests(map[string]bool{"registry.npmjs.org": true})
	want := []HostRequest{
		{"example.org", FromDevcontainer}, {"proxy.golang.org", FromDevcontainer}, {"sum.golang.org", FromLockfile},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HostRequests = %+v", got)
	}
	if got := env.HostRequests(map[string]bool{
		"example.org": true, "proxy.golang.org": true, "sum.golang.org": true, "registry.npmjs.org": true,
	}); len(got) != 0 {
		t.Errorf("answered hosts are not asked again: %+v", got)
	}
}

func TestTagChangesWithWhatTheBuildReads(t *testing.T) {
	a := Environment{Commit: strings.Repeat("a", 40), Dockerfile: "Dockerfile", Context: ".", BuildArgs: map[string]string{"A": "1"}}
	tag := a.Tag("o1")
	if !runtime.ValidImage(tag) || !strings.HasPrefix(tag, "whr-env/o1:") {
		t.Fatalf("tag = %q", tag)
	}
	for name, mutate := range map[string]func(*Environment){
		"commit": func(e *Environment) { e.Commit = strings.Repeat("b", 40) },
		"arg":    func(e *Environment) { e.BuildArgs = map[string]string{"A": "2"} },
		"file":   func(e *Environment) { e.Dockerfile = "Containerfile" },
		"dir":    func(e *Environment) { e.Context = "app" },
	} {
		b := a
		mutate(&b)
		if b.Tag("o1") == tag {
			t.Errorf("the tag does not depend on the %s", name)
		}
	}
	if a.Tag("o1") != tag {
		t.Error("the tag is not stable")
	}
}

// A Dockerfile and a build context of a devcontainer.json come from the commit
// of the default branch; a topic's change and a working tree edit are not in
// the staged files.
func TestStageBuildsFromTheDefaultBranchCommit(t *testing.T) {
	g := newGitRepo(t)
	g.write(".devcontainer/devcontainer.json", `{"build":{"dockerfile":"Dockerfile","context":"..","args":{"V":"1"}}}`, 0o644)
	g.write(".devcontainer/Dockerfile", "FROM fedora\nCOPY app/main.go /main.go\n", 0o644)
	g.write("app/main.go", "package main // reviewed", 0o644)
	g.commit("default")
	g.git("switch", "-q", "-c", "topic")
	g.write(".devcontainer/Dockerfile", "FROM evil\n", 0o644)
	g.write("app/main.go", "package main // the agent's", 0o644)
	g.commit("topic")
	g.git("switch", "-q", "main")
	g.write("app/main.go", "package main // uncommitted", 0o644)

	env, err := Resolve(context.Background(), g, "main", testOpts)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	b, res, err := env.Stage(context.Background(), g, "o1", work)
	if err != nil {
		t.Fatal(err)
	}
	df, _ := os.ReadFile(b.Dockerfile)
	src, _ := os.ReadFile(filepath.Join(b.ContextDir, "app", "main.go"))
	if !strings.Contains(string(df), "FROM fedora") || !strings.Contains(string(src), "reviewed") {
		t.Errorf("staged the wrong content:\nDockerfile %q\nmain.go %q", df, src)
	}
	if _, err := os.Stat(filepath.Join(b.ContextDir, ".git")); err == nil {
		t.Error("the context holds a .git directory")
	}
	if b.Args["V"] != "1" || res.Files < 3 {
		t.Errorf("args = %v, files = %d", b.Args, res.Files)
	}
	// An environment that only runs an image stages nothing.
	if _, _, err := (Environment{Image: "fedora"}).Stage(context.Background(), g, "o1", work); err == nil {
		t.Error("Stage of an image environment must fail")
	}
}
