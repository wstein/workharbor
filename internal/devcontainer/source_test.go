package devcontainer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

var testOpts = Options{BaseImage: "registry.fedoraproject.org/fedora:44", ToolchainImages: DefaultToolchainImages}

func resolve(t *testing.T, files map[string]string, modes map[string]string) (Environment, error) {
	t.Helper()
	g := &fakeGit{files: map[string]string{}, modes: map[string]string{}}
	for k, v := range files {
		g.files[strings.Repeat("a", 40)+":"+k] = v
	}
	for k, v := range modes {
		g.modes[strings.Repeat("a", 40)+":"+k] = v
	}
	return Resolve(context.Background(), g, "main", testOpts)
}

func TestResolveUsesTheDevcontainerFirst(t *testing.T) {
	env, err := resolve(t, map[string]string{
		".devcontainer/devcontainer.json": `{"build":{"dockerfile":"Dockerfile","context":"..","args":{"V":"1"}},"customizations":{"workharbor":{"egress":["proxy.golang.org"]}}}`,
		"Dockerfile":                      "FROM scratch",
		"go.mod":                          "module x\n\ngo 1.27.1\n",
		"go.sum":                          "",
		"package-lock.json":               "{}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if env.Origin != OriginDevcontainer || !env.Built() {
		t.Fatalf("origin = %s, built = %v", env.Origin, env.Built())
	}
	// build.dockerfile is relative to the devcontainer.json, the context too.
	if env.Dockerfile != ".devcontainer/Dockerfile" || env.Context != "." || env.BuildArgs["V"] != "1" {
		t.Errorf("Dockerfile=%q Context=%q args=%v", env.Dockerfile, env.Context, env.BuildArgs)
	}
	if env.Commit != strings.Repeat("a", 40) || env.Toolchain != nil {
		t.Errorf("commit = %q toolchain = %v", env.Commit, env.Toolchain)
	}
	want := []string{"proxy.golang.org", "registry.npmjs.org", "sum.golang.org"}
	if !reflect.DeepEqual(env.SuggestedHosts, want) {
		t.Errorf("SuggestedHosts = %v, want %v", env.SuggestedHosts, want)
	}
}

func TestResolveDevcontainerImage(t *testing.T) {
	env, err := resolve(t, map[string]string{".devcontainer.json": `{"image":"docker.io/library/golang:1.27.1"}`}, nil)
	if err != nil || env.Built() || env.Image != "docker.io/library/golang:1.27.1" {
		t.Errorf("env = %+v err = %v", env, err)
	}
	if _, err := resolve(t, map[string]string{".devcontainer.json": `{"image":"--privileged"}`}, nil); !errors.Is(err, ErrRefused) {
		t.Errorf("an image that looks like an option: err = %v, want ErrRefused", err)
	}
}

func TestResolveDockerfileThenContainerfile(t *testing.T) {
	env, err := resolve(t, map[string]string{"Containerfile": "FROM scratch"}, nil)
	if err != nil || env.Origin != OriginDockerfile || env.Dockerfile != "Containerfile" || env.Context != "." {
		t.Errorf("Containerfile: %+v %v", env, err)
	}
	env, err = resolve(t, map[string]string{"Containerfile": "x", "Dockerfile": "FROM scratch"}, nil)
	if err != nil || env.Dockerfile != "Dockerfile" {
		t.Errorf("Dockerfile wins: %+v %v", env, err)
	}
	// A symbolic link as the Dockerfile is refused, not skipped.
	_, err = resolve(t, map[string]string{"Dockerfile": "/etc/passwd"}, map[string]string{"Dockerfile": "120000"})
	if !errors.Is(err, ErrRefused) {
		t.Errorf("a symbolic Dockerfile: err = %v, want ErrRefused", err)
	}
}

func TestResolveDefaultImageFromToolchain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
		tool  Toolchain
	}{
		{"go.mod", map[string]string{"go.mod": "module x\n\ngo 1.27.1\n"}, "docker.io/library/golang:1.27.1", Toolchain{"go", "1.27.1", "go.mod"}},
		{"tool-versions", map[string]string{".tool-versions": "# c\nnodejs 22.3.0\n"}, "docker.io/library/node:22.3.0", Toolchain{"node", "22.3.0", ".tool-versions"}},
		{"mise", map[string]string{"mise.toml": "[env]\nA = \"1\"\n[tools]\npython = \"3.12\"\n"}, "docker.io/library/python:3.12", Toolchain{"python", "3.12", "mise.toml"}},
		{"nvmrc", map[string]string{".nvmrc": "v20.1.0\n"}, "docker.io/library/node:20.1.0", Toolchain{"node", "20.1.0", ".nvmrc"}},
		{"python-version", map[string]string{".python-version": "3.13.1\n"}, "docker.io/library/python:3.13.1", Toolchain{"python", "3.13.1", ".python-version"}},
		{"go.mod first", map[string]string{"go.mod": "go 1.26", ".nvmrc": "20"}, "docker.io/library/golang:1.26", Toolchain{"go", "1.26", "go.mod"}},
	} {
		env, err := resolve(t, tc.files, nil)
		if err != nil || env.Origin != OriginDefault || env.Image != tc.want || env.Toolchain == nil || *env.Toolchain != tc.tool {
			t.Errorf("%s: env = %+v err = %v", tc.name, env, err)
		}
	}
}

// A version is spliced into an image reference, so only digits and dots pass.
func TestResolveRefusesAToolchainVersionThatIsNotAVersion(t *testing.T) {
	env, err := resolve(t, map[string]string{".nvmrc": "lts/*\n", ".python-version": "3.12 --privileged"}, nil)
	if err != nil || env.Toolchain != nil || env.Image != testOpts.BaseImage {
		t.Errorf("a non-numeric version must fall back to the base image: %+v %v", env, err)
	}
}

func TestResolveNeedsABaseImage(t *testing.T) {
	g := &fakeGit{}
	if _, err := Resolve(context.Background(), g, "main", Options{}); err == nil {
		t.Error("a repository with no environment and no configured base image must fail")
	}
}

func TestResolveReadsOneCommit(t *testing.T) {
	g := &fakeGit{files: map[string]string{strings.Repeat("a", 40) + ":.devcontainer.json": `{"image":"x"}`}}
	if _, err := Resolve(context.Background(), g, "main", testOpts); err != nil {
		t.Fatal(err)
	}
	for _, call := range g.calls {
		for _, a := range call {
			if a == "main" {
				t.Errorf("git %v reads the moving ref after it was resolved to a commit", call)
			}
		}
	}
	if _, err := Resolve(context.Background(), g, "--upload-pack=x", testOpts); !errors.Is(err, ErrBadRef) {
		t.Errorf("a bad ref: err = %v", err)
	}
}
