package devcontainer

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
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
	if !reflect.DeepEqual(c.ForwardPorts, []int{3000}) {
		t.Errorf("ForwardPorts = %v", c.ForwardPorts)
	}
	notes := strings.Join(c.Notes, "|")
	for _, want := range []string{"remoteEnv", "customizations.vscode"} {
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

// fakeGit answers rev-parse, ls-tree and cat-file the way git does: ls-tree
// prints nothing for a missing path and fails only for a broken repository or
// an unknown ref. A blob's object id is its "ref:path" key.
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
	case "rev-parse": // rev-parse --verify --quiet --end-of-options <ref>^{commit}
		return []byte(strings.Repeat("a", 40) + "\n"), nil
	case "ls-tree": // ls-tree -z --full-tree [-r] --end-of-options <ref> [-- <path>]
		rest := args[3:]
		recursive := rest[0] == "-r"
		if recursive {
			rest = rest[1:]
		}
		ref, prefix := rest[1], ""
		if len(rest) > 3 {
			prefix = rest[3]
		}
		var out []byte
		keys := make([]string, 0, len(g.files))
		for k := range g.files {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			kref, p, _ := strings.Cut(key, ":")
			if kref != ref {
				continue
			}
			switch {
			case prefix == "" && recursive, p == prefix:
			case recursive && strings.HasPrefix(p, prefix+"/"):
			case prefix == "" && !strings.Contains(p, "/"):
			default:
				continue
			}
			mode := g.modes[key]
			if mode == "" {
				mode = "100644"
			}
			out = append(out, mode+" blob "+key+"\t"+p+"\x00"...)
		}
		return out, nil
	case "cat-file": // cat-file blob <oid>
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
		if !slices.Contains(call, "--end-of-options") && call[0] != "cat-file" {
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
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "NO_PROXY", "PATH", "LD_PRELOAD", "HOME", "CLAUDE_CONFIG_DIR", "ANTHROPIC_BASE_URL", "WHR_TASK", "OPENAI_API_KEY", "BUILDKIT_SYNTAX", "BUILDKIT_INLINE_CACHE"} {
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

func TestBuildArgsAndFeatures(t *testing.T) {
	c, err := Parse([]byte(`{"build":{"dockerfile":"Dockerfile","args":{"GO_VERSION":"1.27"}},
		"features":{"ghcr.io/devcontainers/features/node:1":{"version":"22"},"ghcr.io/devcontainers/features/git:1":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.BuildArgs["GO_VERSION"] != "1.27" {
		t.Errorf("BuildArgs = %v", c.BuildArgs)
	}
	want := []string{"ghcr.io/devcontainers/features/git:1", "ghcr.io/devcontainers/features/node:1"}
	if !reflect.DeepEqual(c.Features, want) {
		t.Errorf("Features = %v", c.Features)
	}
	// the file's order is kept, with the options as strings
	if len(c.FeatureRequests) != 2 || c.FeatureRequests[0].ID != "ghcr.io/devcontainers/features/node:1" || c.FeatureRequests[0].Options["version"] != "22" || c.FeatureRequests[1].ID != "ghcr.io/devcontainers/features/git:1" {
		t.Errorf("FeatureRequests = %+v", c.FeatureRequests)
	}
	// A build argument may not carry what the supervisor sets: HTTPS_PROXY is a
	// predefined build argument, so it would redirect the build's traffic.
	_, err = Parse([]byte(`{"build":{"dockerfile":"D","args":{"HTTPS_PROXY":"http://evil"}}}`))
	if !errors.Is(err, ErrRefused) {
		t.Errorf("build.args.HTTPS_PROXY: err = %v, want ErrRefused", err)
	}
}

func TestForwardPorts(t *testing.T) {
	c, err := Parse([]byte(`{"image":"x","forwardPorts":[8080, "3000", "db:5432", 0, 70000, 8080]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.ForwardPorts, []int{3000, 8080}) {
		t.Errorf("ForwardPorts = %v", c.ForwardPorts)
	}
	if got := len(c.Notes); got != 3 {
		t.Errorf("want 3 notes (host:port, 0, 70000), got %v", c.Notes)
	}
}

func TestWorkharborHints(t *testing.T) {
	c, err := Parse([]byte(`{"image":"x","customizations":{"workharbor":{
		"check":"make check","previewPorts":[3000],"agent":"claude","tools":["Read","Edit"],
		"egress":["Proxy.Golang.org","proxy.golang.org","*.evil.com","10.0.0.1","host:443","localhost"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Hints{Check: "make check", PreviewPorts: []int{3000}, Agent: "claude", Tools: []string{"Read", "Edit"}}
	if !reflect.DeepEqual(c.Hints, want) {
		t.Errorf("Hints = %+v", c.Hints)
	}
	if !reflect.DeepEqual(c.EgressRequests, []string{"proxy.golang.org"}) {
		t.Errorf("only a real host name may be requested, once: %v", c.EgressRequests)
	}
	joined := strings.Join(c.Notes, "|")
	if n := strings.Count(joined, "is not a host name"); n != 3 {
		t.Errorf("want 3 refused hosts noted, got %d: %v", n, c.Notes)
	}
	// A wildcard is refused with a note of its own: a repository asks for one exact
	// host, and only the supervisor's configuration may allow a wildcard (§7.2).
	if n := strings.Count(joined, `"*.evil.com" is a wildcard`); n != 1 {
		t.Errorf("want the wildcard named in a note, got %d: %v", n, c.Notes)
	}
}

func TestParseRefusesAnImageUnderTheSupervisorsBuiltHost(t *testing.T) {
	for _, image := range []string{"whr.invalid/whr-env/o1:abc", "WHR.INVALID/whr-base/o1:abc", " whr.invalid/x", "whr\\t.invalid/x", "'whr.invalid'/x"} {
		_, err := Parse([]byte(`{"image": "` + image + `"}`))
		var re *RefusedError
		if !errors.As(err, &re) || !strings.Contains(err.Error(), "image") {
			t.Errorf("image %q: err = %v, want a refusal naming image", image, err)
		}
	}
	if _, err := Parse([]byte(`{"image": "ghcr.io/x/whr.invalid:1"}`)); err != nil {
		t.Errorf("an unrelated image is refused: %v", err)
	}
}

func TestRefuseBuiltFromScansTheWholeText(t *testing.T) {
	const img = "whr.invalid/whr-env/x:y"
	refused := map[string]string{
		"from":            "FROM " + img + "\n",
		"platform":        "from --platform=linux/arm64 WHR.invalid/whr-base/o1:abc AS x\n",
		"later stage":     "FROM fedora AS base\nFROM " + img + "\nCOPY --from=base /a /a\n",
		"crlf continued":  "FROM fedora\r\nFROM \\\r\n  whr.invalid/x\r\n",
		"arg braces":      "ARG B=" + img + "\nFROM ${B}\n",
		"arg dollar":      "ARG B=" + img + "\nFROM $B\n",
		"backtick escape": "# escape=`\nFROM `\n  " + img + "\n",
		"backslash split": "FROM whr.\\\ninvalid/x\n",
		"backtick split":  "# escape=`\nFROM whr.`\ninvalid/x\n",
		"copy from":       "FROM fedora\nCOPY --from=" + img + " /a /b\n",
		"run mount":       "FROM fedora\nRUN --mount=type=bind,from=" + img + ",target=/m true\n",
		"upper case":      "FROM WHR.INVALID/whr-env/x:y\n",
		"double quoted":   "FROM \"whr.invalid/x\"\n",
		"single quoted":   "FROM 'whr'.'invalid'/x\n",
		"in a comment":    "# FROM whr.invalid/x\nFROM fedora\n",
	}
	for name, df := range refused {
		if err := RefuseBuiltFrom([]byte(df)); err == nil {
			t.Errorf("%s: accepted %q", name, df)
		}
	}
	legit := "FROM golang:1.24 AS build\nRUN go build -o /out .\nFROM scratch\nCOPY --from=build /out /out\n"
	for _, df := range []string{"FROM fedora\n", "FROM --platform=linux/arm64 ghcr.io/x/y:1\n", legit} {
		if err := RefuseBuiltFrom([]byte(df)); err != nil {
			t.Errorf("refused %q: %v", df, err)
		}
	}
}

// KNOWN LIMITATION, not a feature: a name assembled from ARG pieces is not in
// the text, so the scan accepts it. This test documents the gap (design §5.1,
// the D38 bullet); it must be changed, not deleted, if the gap is ever closed.
func TestRefuseBuiltFromKnownGapNameAssembledFromArgs(t *testing.T) {
	df := "ARG H=whr\nARG D=invalid\nFROM ${H}.${D}/x\n"
	if err := RefuseBuiltFrom([]byte(df)); err != nil {
		t.Fatalf("the gap is closed (good): update the docs and this test: %v", err)
	}
}

func TestParseRefusesBuildArgsNamingTheBuiltHost(t *testing.T) {
	for _, v := range []string{"whr.invalid/whr-env/x:y", "WHR.INVALID/x", "whr.\\\ninvalid/x", "'whr.invalid'/x", "a b whr . invalid"} {
		doc := `{"build": {"dockerfile": "Dockerfile", "args": {"B": ` + strconv.Quote(v) + `}}}`
		_, err := Parse([]byte(doc))
		var re *RefusedError
		if !errors.As(err, &re) || !strings.Contains(err.Error(), "build.args.B") {
			t.Errorf("value %q: err = %v, want a refusal naming build.args.B", v, err)
		}
	}
	if _, err := Parse([]byte(`{"build": {"dockerfile": "Dockerfile", "args": {"B": "golang:1.24"}}}`)); err != nil {
		t.Errorf("a plain build arg is refused: %v", err)
	}
}

func TestRefuseSyntaxDirective(t *testing.T) {
	digest := strings.Repeat("a", 64)
	accepted := map[string]string{
		"none":                "FROM fedora\n",
		"official no tag":     "# syntax=docker/dockerfile\nFROM fedora\n",
		"official with tag":   "# syntax=docker/dockerfile:1.7\nFROM fedora\n",
		"tag and digest":      "# syntax=docker/dockerfile:1.7@sha256:" + digest + "\nFROM fedora\n",
		"spaces around":       "# syntax = docker/dockerfile:1\nFROM fedora\n",
		"after instruction":   "FROM fedora\n# syntax=whr.invalid/x\nRUN true\n",
		"after other comment": "# a note\n# syntax=docker/dockerfile:1\nFROM fedora\n",
	}
	for name, df := range accepted {
		if err := RefuseSyntaxDirective([]byte(df)); err != nil {
			t.Errorf("%s: refused %q: %v", name, df, err)
		}
	}
	refused := map[string]string{
		"custom image":    "# syntax=example.com/frontend:1\nFROM fedora\n",
		"docker.io evil":  "# syntax=docker.io/evil/frontend\nFROM fedora\n",
		"built host":      "# syntax=whr.invalid/whr-env/x:y\nFROM fedora\n",
		"tag with space":  "# syntax=docker/dockerfile:1 x\nFROM fedora\n",
		"no space":        "#syntax=evil/x\nFROM fedora\n",
		"upper case key":  "# SYNTAX=evil/x\nFROM fedora\n",
		"spaced equals":   "# syntax = evil/x\nFROM fedora\n",
		"digest only":     "# syntax=docker/dockerfile@sha256:" + digest + "\nFROM fedora\n",
		"short digest":    "# syntax=docker/dockerfile:1@sha256:abc\nFROM fedora\n",
		"look-alike":      "# syntax=docker/dockerfile-evil\nFROM fedora\n",
		"crlf":            "# syntax=evil/x\r\nFROM fedora\r\n",
		"after a comment": "# a note\n\n# syntax=evil/x\nFROM fedora\n",
		"bom":             "\xef\xbb\xbf# syntax=evil/x\nFROM fedora\n",
	}
	for name, df := range refused {
		err := RefuseSyntaxDirective([]byte(df))
		if err == nil || !strings.Contains(err.Error(), "syntax directive") {
			t.Errorf("%s: accepted %q (err = %v)", name, df, err)
		}
	}
}
