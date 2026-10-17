// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	goodManifest = `{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`
	goodDevc     = `{"name":"t","containerUser":"0:0","remoteUser":"1000:1000","image":"docker.io/library/buildpack-deps@sha256:4163ac2c58660c533f3cf3d178551287ddd0ac18269ef84f3a382d0698c89254"}`
)

func put(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goodDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	put(t, dir, "workharbor.json", goodManifest)
	put(t, dir, ".devcontainer/devcontainer.json", goodDevc)
	put(t, dir, "projects/.gitkeep", "")
	return dir
}

func TestValidateExitCodes(t *testing.T) {
	good := goodDir(t)
	bad := goodDir(t)
	put(t, bad, "workharbor.json", `{"schema_version":1.0}`)
	empty := t.TempDir()
	file := filepath.Join(t.TempDir(), "f")
	put(t, filepath.Dir(file), "f", "x")
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"valid", []string{"validate", good}, 0},
		{"valid long form", []string{"workspace", "validate", good}, 0},
		{"valid ws alias", []string{"ws", "validate", good}, 0},
		{"invalid configuration", []string{"validate", bad}, 68},
		{"no manifest", []string{"validate", empty}, 68},
		{"missing directory", []string{"validate", filepath.Join(good, "absent")}, 65},
		{"path is a file", []string{"validate", file}, 64},
		{"too many arguments", []string{"validate", good, good}, 64},
		{"unknown flag", []string{"validate", "--bogus", good}, 64},
	} {
		got := run(t, nil, tc.args...)
		if got.code != tc.code {
			t.Errorf("%s: exit %d, want %d\nstdout %q\nstderr %q", tc.name, got.code, tc.code, got.stdout, got.err)
		}
		if tc.code != 0 && !strings.HasPrefix(got.err, "whr: ") {
			t.Errorf("%s: no whr: line: %q", tc.name, got.err)
		}
	}
}

func TestValidateDefaultsToCurrentDirectory(t *testing.T) {
	t.Chdir(goodDir(t))
	if got := run(t, nil, "validate"); got.code != 0 {
		t.Errorf("exit %d: %q %q", got.code, got.stdout, got.err)
	}
}

func TestValidateJSONMatchesSchema(t *testing.T) {
	schema := compileSchema(t)
	bad := goodDir(t)
	put(t, bad, "workharbor.json", `{"schema_version":1,"devcontainer_path":"/etc","projects_path":"projects","bundles":[1],"x":"y"}`)
	warn := goodDir(t)
	put(t, warn, ".devcontainer/devcontainer.json", `{"image":"ubuntu:22.04"}`)
	var many strings.Builder
	many.WriteString("{")
	for i := 0; i < 300; i++ {
		many.WriteString(`"k` + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + `":1,`)
	}
	many.WriteString(`"image":"x"}`)
	big := goodDir(t)
	put(t, big, ".devcontainer/devcontainer.json", many.String())
	for name, tc := range map[string]struct {
		dir  string
		code int
	}{
		"valid": {goodDir(t), 0}, "warnings": {warn, 0}, "errors": {bad, 68}, "truncated": {big, 0},
		"missing manifest": {t.TempDir(), 68}, "absent": {filepath.Join(bad, "nope"), 65},
	} {
		got := run(t, nil, "validate", "--json", tc.dir)
		doc, err := jsonUnmarshal(got.stdout)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := schema.Validate(doc); err != nil {
			t.Errorf("%s: %v\n%s", name, err, got.stdout)
		}
		env := decodeEnvelope(t, got.stdout)
		if got.code != tc.code || env["exit_code"] != float64(tc.code) || env["command"] != "workspace.validate" || env["ok"] != (tc.code == 0) {
			t.Errorf("%s: exit %d envelope %v", name, got.code, env)
		}
		if res, ok := env["result"].(map[string]any); ok {
			if res["provider_admission"] != "not_evaluated" || res["valid"] != (tc.code == 0) {
				t.Errorf("%s: result %v", name, res)
			}
			if _, ok := res["findings"].([]any); !ok {
				t.Errorf("%s: findings is not an array: %v", name, res["findings"])
			}
			if name == "truncated" && (res["truncated"] != true || len(res["findings"].([]any)) != 200) {
				t.Errorf("%s: truncated=%v len=%d", name, res["truncated"], len(res["findings"].([]any)))
			}
		} else if tc.code == 68 {
			t.Errorf("%s: 68 without a result", name)
		}
	}
}

func TestValidateAliasesAreEquivalent(t *testing.T) {
	dir := goodDir(t)
	want := run(t, nil, "workspace", "validate", dir, "--json")
	for _, args := range [][]string{{"validate", dir, "--json"}, {"ws", "validate", dir, "--json"}} {
		if got := run(t, nil, args...); got != want {
			t.Errorf("%v differs:\n%v\n%v", args, got, want)
		}
	}
}

func TestValidateHumanOutput(t *testing.T) {
	dir := goodDir(t)
	put(t, dir, "workharbor.json", "{\"schema_version\":1,\"devcontainer_path\":\".devcontainer/devcontainer.json\",\"projects_path\":\"projects\",\"bundles\":[],\"bad\\u001b[2J\\u202ekey\\u009b\":1}")
	got := run(t, nil, "validate", dir)
	if got.code != 68 {
		t.Fatalf("exit %d", got.code)
	}
	for _, s := range []string{got.stdout, got.err} {
		if strings.ContainsAny(s, "\x1b\x00\a\u202e\u009b") {
			t.Errorf("raw control bytes: %q", s)
		}
	}
	for _, want := range []string{"manifest.schema", "error", "not evaluated", "invalid"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
	if !strings.HasPrefix(got.err, "whr: ") {
		t.Errorf("stderr %q", got.err)
	}
	ok := run(t, nil, "validate", goodDir(t))
	if ok.code != 0 || ok.err != "" || !strings.Contains(ok.stdout, "valid") || !strings.Contains(ok.stdout, "not evaluated") {
		t.Errorf("valid run: %+v", ok)
	}
}

func TestValidateHostileDirectoryNameIsEscaped(t *testing.T) {
	base := t.TempDir()
	name := filepath.Join(base, "bad\x1b[31m\u202edir")
	got := run(t, nil, "validate", name)
	if got.code != 65 || strings.ContainsAny(got.err+got.stdout, "\x1b\u202e") {
		t.Errorf("exit %d stderr %q", got.code, got.err)
	}
	got = run(t, nil, "validate", "--json", name)
	if strings.ContainsAny(got.stdout, "\x1b\u202e") {
		t.Errorf("raw bytes in JSON: %q", got.stdout)
	}
}

func TestValidateInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut strings.Builder
	code := runWithContext(ctx, &out, &errOut, "validate", goodDir(t))
	if code != 72 {
		t.Errorf("exit %d, want 72; stderr %q", code, errOut.String())
	}
}

func TestValidateRefusesLinksAndSpecialFiles(t *testing.T) {
	dir := goodDir(t)
	if err := os.Remove(filepath.Join(dir, "workharbor.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "workharbor.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := run(t, nil, "validate", "--json", dir)
	if got.code != 68 || !strings.Contains(got.stdout, "manifest.unsafe_path") {
		t.Errorf("exit %d %q", got.code, got.stdout)
	}
}

func TestValidateJSONWithGitFindingsMatchesSchema(t *testing.T) {
	dir := goodDir(t)
	sha := strings.Repeat("a", 40)
	put(t, dir, ".gitmodules", "[submodule \"app\"]\n\tpath = projects/app\n\turl = https://u:CANARY@f.example/o/app.git\n")
	env := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", "--template="}, {"add", "-A"},
		{"update-index", "--add", "--cacheinfo", "160000," + sha + ",projects/app"}, {"commit", "-q", "-m", "x"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	got := run(t, nil, "validate", "--json", dir)
	doc, err := jsonUnmarshal(got.stdout)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileSchema(t).Validate(doc); err != nil {
		t.Errorf("%v\n%s", err, got.stdout)
	}
	if got.code != 68 || !strings.Contains(got.stdout, "submodule.url_credentials") || strings.Contains(got.stdout+got.err, "CANARY") {
		t.Errorf("exit %d: %s %s", got.code, got.stdout, got.err)
	}
}
