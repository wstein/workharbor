package devcontainer

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseHonoursTheSubset(t *testing.T) {
	c, err := Parse([]byte(`{
	// a comment, and a trailing comma below
	"name": "x",
	"image": "golang:1.27.1", /* inline */
	"containerEnv": {"A": "b // not a comment",},
	"postCreateCommand": "go mod download",
	"customizations": {"workharbor": {"egress": ["proxy.golang.org"]}, "vscode": {"extensions": []}},
	"forwardPorts": [3000],
	"remoteEnv": {"X": "1"},
}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Image != "golang:1.27.1" || c.Env["A"] != "b // not a comment" {
		t.Errorf("image or env wrong: %+v", c)
	}
	if !reflect.DeepEqual(c.PostCreate, []Command{{Shell: "go mod download"}}) {
		t.Errorf("PostCreate = %+v", c.PostCreate)
	}
	if !reflect.DeepEqual(c.EgressRequests, []string{"proxy.golang.org"}) {
		t.Errorf("EgressRequests = %v", c.EgressRequests)
	}
	notes := strings.Join(c.Notes, "|")
	for _, want := range []string{"forwardPorts", "remoteEnv", "customizations.vscode"} {
		if !strings.Contains(notes, want) {
			t.Errorf("no note for %s: %v", want, c.Notes)
		}
	}
}

func TestParseRefusesWhatWidensTheEnvironment(t *testing.T) {
	for _, key := range []string{
		`"initializeCommand": "make"`, `"mounts": []`, `"runArgs": ["--net=host"]`, `"privileged": true`,
		`"capAdd": ["SYS_ADMIN"]`, `"securityOpt": ["seccomp=unconfined"]`, `"workspaceMount": "x"`,
		`"remoteUser": "root"`, `"containerUser": "0"`, `"remoteUser": "0:0"`,
	} {
		_, err := Parse([]byte(`{"image": "x", ` + key + `}`))
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want ErrRefused", key, err)
		}
	}
	if _, err := Parse([]byte(`{"image": "x", "remoteUser": "vscode"}`)); err != nil {
		t.Errorf("a non-root user must be honoured: %v", err)
	}
	_, err := Parse([]byte(`{"image": "x", "privileged": true, "runArgs": []}`))
	var re *RefusedError
	if !errors.As(err, &re) || len(re.Keys) != 2 {
		t.Errorf("every refused key must be listed: %v", err)
	}
}

func TestParseNeedsExactlyOneSource(t *testing.T) {
	for in, ok := range map[string]bool{
		`{}`: false, `{"image":"a","build":{"dockerfile":"D"}}`: false,
		`{"build":{"dockerfile":"Dockerfile"}}`: true, `{"image":"a"}`: true,
	} {
		if _, err := Parse([]byte(in)); (err == nil) != ok {
			t.Errorf("%s: err = %v", in, err)
		}
	}
}

func TestPostCreateForms(t *testing.T) {
	c, err := Parse([]byte(`{"image":"a","postCreateCommand":["go","mod","download"]}`))
	if err != nil || !reflect.DeepEqual(c.PostCreate, []Command{{Argv: []string{"go", "mod", "download"}}}) {
		t.Errorf("array form: %+v %v", c.PostCreate, err)
	}
	c, err = Parse([]byte(`{"image":"a","postCreateCommand":{"a":"x","b":"y"}}`))
	if err != nil || len(c.PostCreate) != 0 || !strings.Contains(strings.Join(c.Notes, ""), "object form") {
		t.Errorf("object form must be ignored with a note: %+v %v", c, err)
	}
}

// fakeGit answers ls-tree and show for "ref:path" keys, the way git does:
// ls-tree prints nothing for a missing path and fails only for a broken
// repository or an unknown ref.
type fakeGit struct {
	files  map[string]string // "ref:path" -> content
	modes  map[string]string // "ref:path" -> mode, 100644 by default
	broken bool
	calls  [][]string
}

func (g *fakeGit) Run(_ context.Context, args ...string) ([]byte, error) {
	g.calls = append(g.calls, args)
	if g.broken {
		return nil, errors.New("fatal: not a git repository")
	}
	switch args[0] {
	case "ls-tree": // ls-tree --end-of-options <ref> -- <path>
		key := args[2] + ":" + args[4]
		if _, ok := g.files[key]; !ok {
			return nil, nil
		}
		mode := g.modes[key]
		if mode == "" {
			mode = "100644"
		}
		return []byte(mode + " blob 0123456789abcdef\t" + args[4] + "\n"), nil
	case "show": // show --end-of-options <ref>:<path>
		if v, ok := g.files[args[2]]; ok {
			return []byte(v), nil
		}
	}
	return nil, errors.New("not found")
}

func TestReadUsesTheRefNotTheWorkingTree(t *testing.T) {
	g := &fakeGit{files: map[string]string{"main:.devcontainer.json": `{"image":"b"}`}}
	c, found, err := Read(context.Background(), g, "main")
	if err != nil || !found || c.Image != "b" {
		t.Fatalf("Read = %+v %v %v", c, found, err)
	}
	for _, call := range g.calls {
		if call[1] != "--end-of-options" {
			t.Errorf("git %v: the ref must follow --end-of-options", call)
		}
	}
	if _, found, err := Read(context.Background(), &fakeGit{}, "main"); found || err != nil {
		t.Errorf("a repository without the file: found=%v err=%v", found, err)
	}
	refused := &fakeGit{files: map[string]string{"main:.devcontainer/devcontainer.json": `{"privileged":true,"image":"x"}`}}
	if _, _, err := Read(context.Background(), refused, "main"); !errors.Is(err, ErrRefused) {
		t.Errorf("err = %v, want ErrRefused", err)
	}
}

// A repository that cannot be read is an error, never "no devcontainer", or
// the environment would silently be built without the requested image.
func TestReadReportsABrokenRepository(t *testing.T) {
	if _, found, err := Read(context.Background(), &fakeGit{broken: true}, "main"); err == nil || found {
		t.Errorf("a broken repository: found=%v err=%v", found, err)
	}
}

func TestReadRefusesALinkAndABadRef(t *testing.T) {
	g := &fakeGit{
		files: map[string]string{"main:.devcontainer/devcontainer.json": `{"image":"x"}`},
		modes: map[string]string{"main:.devcontainer/devcontainer.json": "120000"},
	}
	if _, _, err := Read(context.Background(), g, "main"); !errors.Is(err, ErrRefused) {
		t.Errorf("a symbolic link: err = %v, want ErrRefused", err)
	}
	for _, ref := range []string{"", "-p", "--output=/tmp/x", "main:x", "a..b", "main branch", "main\n"} {
		if _, _, err := Read(context.Background(), &fakeGit{}, ref); !errors.Is(err, ErrBadRef) {
			t.Errorf("ref %q: err = %v, want ErrBadRef", ref, err)
		}
	}
}

// build.dockerfile and build.context are resolved against the directory of
// the devcontainer.json and must stay inside the repository.
func TestBuildPathsStayInsideTheRepository(t *testing.T) {
	for in, ok := range map[string]bool{
		`{"build":{"dockerfile":"Dockerfile","context":".."}}`:             true,
		`{"build":{"dockerfile":"../tools/Dockerfile"}}`:                   true,
		`{"build":{"dockerfile":"Dockerfile","context":"../.."}}`:          false,
		`{"build":{"dockerfile":"../../etc/Dockerfile"}}`:                  false,
		`{"build":{"dockerfile":"a/../../../x"}}`:                          false,
		`{"build":{"dockerfile":"/etc/Dockerfile"}}`:                       false,
		`{"build":{"dockerfile":"Dockerfile","context":"/Users/someone"}}`: false,
	} {
		g := &fakeGit{files: map[string]string{"main:.devcontainer/devcontainer.json": in}}
		_, _, err := Read(context.Background(), g, "main")
		if ok != (err == nil) {
			t.Errorf("%s: err = %v", in, err)
		} else if !ok && !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want ErrRefused", in, err)
		}
	}
	// At the repository root, .devcontainer.json may not reach above it at all.
	g := &fakeGit{files: map[string]string{"main:.devcontainer.json": `{"build":{"dockerfile":"Dockerfile","context":".."}}`}}
	if _, _, err := Read(context.Background(), g, "main"); !errors.Is(err, ErrRefused) {
		t.Errorf("a root-level file with context ..: err = %v, want ErrRefused", err)
	}
}

// containerEnv may not set what the supervisor sets or the agent reads (D38).
func TestReservedEnvironmentIsRefused(t *testing.T) {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "NO_PROXY", "PATH", "LD_PRELOAD", "HOME", "CLAUDE_CONFIG_DIR", "ANTHROPIC_BASE_URL", "WHR_TASK", "OPENAI_API_KEY"} {
		_, err := Parse([]byte(`{"image":"x","containerEnv":{"` + name + `":"v"}}`))
		if !errors.Is(err, ErrRefused) {
			t.Errorf("containerEnv.%s: err = %v, want ErrRefused", name, err)
		}
	}
	if _, err := Parse([]byte(`{"image":"x","containerEnv":{"GOFLAGS":"-mod=mod","NODE_ENV":"test"}}`)); err != nil {
		t.Errorf("ordinary variables must be honoured: %v", err)
	}
}

func TestRootIsRefusedInAnySpelling(t *testing.T) {
	for user, root := range map[string]bool{"root": true, "0": true, "00": true, " 0 ": true, "0:1000": true, "root:root": true, "1000": false, "vscode": false, "rootless": false} {
		if got := isRoot(user); got != root {
			t.Errorf("isRoot(%q) = %v, want %v", user, got, root)
		}
	}
}

// The file in this repository must pass its own reader (D34, issue #77).
func TestThisRepositoryDevcontainer(t *testing.T) {
	data, err := os.ReadFile("../../.devcontainer/devcontainer.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.Dockerfile != "Dockerfile" || len(c.EgressRequests) != 2 {
		t.Errorf("unexpected config: %+v", c)
	}
	dockerfile, err := os.ReadFile("../../.devcontainer/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	goMod, _ := os.ReadFile("../../go.mod")
	var goVersion string
	for _, l := range strings.Split(string(goMod), "\n") {
		if v, ok := strings.CutPrefix(l, "go "); ok {
			goVersion = strings.TrimSpace(v)
		}
	}
	if !strings.Contains(string(dockerfile), "FROM golang:"+goVersion+"-") {
		t.Errorf("the Dockerfile does not start from go.mod's Go %s", goVersion)
	}
}
