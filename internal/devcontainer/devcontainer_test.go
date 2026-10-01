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

type fakeGit map[string]string

func (g fakeGit) Run(_ context.Context, args ...string) ([]byte, error) {
	if v, ok := g[args[1]]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("not found")
}

func TestReadUsesTheRefNotTheWorkingTree(t *testing.T) {
	g := fakeGit{"main:.devcontainer.json": `{"image":"b"}`}
	c, found, err := Read(context.Background(), g, "main")
	if err != nil || !found || c.Image != "b" {
		t.Fatalf("Read = %+v %v %v", c, found, err)
	}
	if _, found, err := Read(context.Background(), fakeGit{}, "main"); found || err != nil {
		t.Errorf("a repository without the file: found=%v err=%v", found, err)
	}
	if _, _, err := Read(context.Background(), fakeGit{"main:.devcontainer/devcontainer.json": `{"privileged":true,"image":"x"}`}, "main"); !errors.Is(err, ErrRefused) {
		t.Errorf("err = %v, want ErrRefused", err)
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
