// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	git, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	c := exec.Command(git, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("fixture git: %v %s", e, b)
	}
	return strings.TrimSpace(string(b))
}
func resolverFixture(t *testing.T, mutate func(string)) (Resolver, Descriptor) {
	t.Helper()
	return resolverFixtureFormat(t, "sha1", mutate)
}
func resolverFixtureFormat(t *testing.T, format string, mutate func(string)) (Resolver, Descriptor) {
	t.Helper()
	return resolverFixtureWithParser(t, format, mutate, jsoncTool(t))
}
func resolverFixtureWithParser(t *testing.T, format string, mutate func(string), parser JSONC) (Resolver, Descriptor) {
	t.Helper()
	repo := t.TempDir()
	gitFixture(t, repo, "init", "--quiet", "--object-format="+format)
	if e := os.MkdirAll(filepath.Join(repo, ".devcontainer"), 0700); e != nil {
		t.Fatal(e)
	}
	os.MkdirAll(filepath.Join(repo, "projects"), 0700)
	os.WriteFile(filepath.Join(repo, "projects", "original.bin"), []byte{0, 1, 255}, 0600)
	os.WriteFile(filepath.Join(repo, "workharbor.json"), []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]}`), 0600)
	os.WriteFile(filepath.Join(repo, ".devcontainer", "devcontainer.json"), []byte(`{"image":"ubuntu@sha256:`+strings.Repeat("a", 64)+`"}`), 0600)
	if mutate != nil {
		mutate(repo)
	}
	gitFixture(t, repo, "add", "--all")
	gitFixture(t, repo, "commit", "--quiet", "-m", "fixture")
	commit := gitFixture(t, repo, "rev-parse", "HEAD")
	gitFixture(t, repo, "update-server-info")
	server := httptest.NewTLSServer(http.FileServer(http.Dir(filepath.Join(repo, ".git"))))
	t.Cleanup(server.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
	if _, e := x509.ParseCertificate(server.Certificate().Raw); e != nil {
		t.Fatal(e)
	}
	git, _ := exec.LookPath("git")
	stageRoot := t.TempDir()
	if e := os.Chmod(stageRoot, 0700); e != nil {
		t.Fatal(e)
	}
	return Resolver{GitPath: git, StageRoot: stageRoot, CAFile: ca, Parser: parser}, Descriptor{RepositoryIdentity: server.URL + "/", Commit: commit, Submodules: []Submodule{}}
}
func TestResolverExactHTTPSCommitPrivateStageAndNoAmbientHooks(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "hook-ran")
	hooks := t.TempDir()
	os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700)
	global := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(global, []byte("[core]\nhooksPath = "+hooks+"\n[filter \"hostile\"]\nsmudge = touch '"+sentinel+"'\n"), 0600)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooks)
	r, d := resolverFixture(t, nil)
	result, e := r.Resolve(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	if result.Descriptor.Commit != d.Commit || result.Source["image"] == nil || result.DevcontainerPath != filepath.Join(result.Root, ".devcontainer/devcontainer.json") {
		t.Fatalf("resolved=%#v", result)
	}
	defer result.Close()
	if _, e := os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatal("ambient Git hook/filter executed")
	}
	info, e := os.Stat(result.Root)
	if e != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private stage=%v %v", info, e)
	}
	data, e := os.ReadFile(filepath.Join(result.ProjectsPath, "original.bin"))
	if e != nil || string(data) != string([]byte{0, 1, 255}) {
		t.Fatal("project changed")
	}
}
func TestResolverRejectsManifestAmbiguitySymlinksAndHostHooks(t *testing.T) {
	for _, mutate := range []func(string){
		func(p string) {
			os.WriteFile(filepath.Join(p, "workharbor.json"), []byte(`{"schema_version":1,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"bundles":[]}`), 0600)
		},
		func(p string) {
			os.Remove(filepath.Join(p, ".devcontainer/devcontainer.json"))
			os.Symlink("/etc/passwd", filepath.Join(p, ".devcontainer/devcontainer.json"))
		},
		func(p string) {
			os.WriteFile(filepath.Join(p, ".devcontainer/devcontainer.json"), []byte(`{"image":"ubuntu@sha256:`+strings.Repeat("a", 64)+`","initializeCommand":"touch MUST_NOT_RUN"}`), 0600)
		},
		func(p string) {
			os.WriteFile(filepath.Join(p, ".gitmodules"), []byte("[submodule \"missing\"]\npath=projects/missing\nurl=../relative\n"), 0600)
		},
	} {
		r, d := resolverFixture(t, mutate)
		_, e := r.Resolve(context.Background(), d)
		if e == nil {
			t.Fatal("unsafe stage accepted")
		}
		if e.Error() == "unimplemented" {
			t.Fatal("gate absent")
		}
	}
}
func TestResolverMissingExactCommitNeverFallsBackToBranch(t *testing.T) {
	r, d := resolverFixture(t, nil)
	d.Commit = strings.Repeat("f", 40)
	_, e := r.Resolve(context.Background(), d)
	code(t, e, "revision_unavailable")
	entries, e := os.ReadDir(r.StageRoot)
	if e != nil || len(entries) != 0 {
		t.Fatal("failed private stage retained")
	}
}

func TestResolverExactCompleteSubmoduleAndNoNestedOrRelativeFallback(t *testing.T) {
	child, project := resolverFixture(t, nil)
	r, d := resolverFixture(t, func(p string) {
		gitFixture(t, p, "-c", "http.sslCAInfo="+child.CAFile, "clone", "--quiet", project.RepositoryIdentity, "projects/app")
		os.WriteFile(filepath.Join(p, ".gitmodules"), []byte("[submodule \"app\"]\npath=projects/app\nurl="+project.RepositoryIdentity+"\n"), 0600)
	})
	d.Submodules = []Submodule{{Path: "projects/app", Commit: project.Commit}}
	resolved, e := r.Resolve(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	defer resolved.Close()
	raw, e := os.ReadFile(filepath.Join(resolved.ProjectsPath, "app/projects/original.bin"))
	if e != nil || string(raw) != string([]byte{0, 1, 255}) {
		t.Fatal("project gitlink not initialized exactly")
	}
	for _, set := range [][]Submodule{{}, {{Path: "projects/app", Commit: strings.Repeat("c", 40)}}, {{Path: "projects/other", Commit: project.Commit}}} {
		wrong := d
		wrong.Submodules = set
		_, e := r.Resolve(context.Background(), wrong)
		code(t, e, "invalid_configuration")
	}
}

func TestResolverFrozenInputsDetectContentModeAndSymlinkDrift(t *testing.T) {
	for _, change := range []func(*Resolved){func(s *Resolved) {
		os.WriteFile(s.DevcontainerPath, []byte(`{"initializeCommand":"touch MUST_NOT_RUN"}`), 0600)
	}, func(s *Resolved) { os.Chmod(s.DevcontainerPath, 0777) }, func(s *Resolved) { os.Remove(s.DevcontainerPath); os.Symlink("/etc/passwd", s.DevcontainerPath) }} {
		r, d := resolverFixture(t, nil)
		s, e := r.Resolve(context.Background(), d)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Check(); e != nil {
			t.Fatal(e)
		}
		change(s)
		code(t, s.Check(), "configuration_changed")
		if e = s.Close(); e != nil {
			t.Fatal(e)
		}
	}
}

func TestResolverSnapshotCannotBeRetargetedThroughExportedFields(t *testing.T) {
	for _, change := range []func(*Resolved){func(s *Resolved) { s.Source["postStartCommand"] = "hostile" }, func(s *Resolved) { s.Descriptor.Commit = strings.Repeat("f", 40) }, func(s *Resolved) { s.DevcontainerPath = "/foreign/devcontainer.json" }, func(s *Resolved) { s.Bases = []string{"foreign@sha256:" + strings.Repeat("b", 64)} }} {
		r, d := resolverFixture(t, nil)
		s, e := r.Resolve(context.Background(), d)
		if e != nil {
			t.Fatal(e)
		}
		change(s)
		code(t, s.Check(), "configuration_changed")
		s.Close()
	}
}

func TestResolverExactSHA256RepositoryNeverUsesDefaultSHA1(t *testing.T) {
	r, d := resolverFixtureFormat(t, "sha256", nil)
	if len(d.Commit) != 64 {
		t.Fatal("fixture hash format")
	}
	resolved, e := r.Resolve(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	defer resolved.Close()
	if resolved.Descriptor.Commit != d.Commit {
		t.Fatal("SHA256 exact revision changed")
	}
}
func TestResolverHTTPSRootUsesExplicitSSHOnlyForApprovedGitlink(t *testing.T) {
	child, project := resolverFixture(t, nil)
	r, d := resolverFixture(t, func(p string) {
		gitFixture(t, p, "-c", "http.sslCAInfo="+child.CAFile, "clone", "--quiet", project.RepositoryIdentity, "projects/app")
		os.WriteFile(filepath.Join(p, ".gitmodules"), []byte("[submodule \"app\"]\npath=projects/app\nurl=git@ssh.example.test:projects/app.git\n"), 0600)
	})
	d.Submodules = []Submodule{{Path: "projects/app", Commit: project.Commit}}
	credentials := t.TempDir()
	marker := filepath.Join(credentials, "ssh-used")
	r.SSHPath = filepath.Join(credentials, "ssh")
	r.KnownHostsFile = filepath.Join(credentials, "known_hosts")
	r.IdentityFile = filepath.Join(credentials, "identity")
	os.WriteFile(r.KnownHostsFile, []byte("fixture"), 0600)
	os.WriteFile(r.IdentityFile, []byte("fixture"), 0600)
	os.WriteFile(r.SSHPath, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700)
	_, e := r.Resolve(context.Background(), d)
	code(t, e, "revision_unavailable")
	if _, e = os.Stat(marker); e != nil {
		t.Fatal("explicit SSH transport never used", e)
	}
}

func TestResolverRelativeSourceSymlinkUsesConfigurationDiagnostic(t *testing.T) {
	r, d := resolverFixture(t, func(root string) {
		path := filepath.Join(root, ".devcontainer/devcontainer.json")
		if e := os.Remove(path); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink("../../outside.json", path); e != nil {
			t.Fatal(e)
		}
	})
	resolved, e := r.Resolve(context.Background(), d)
	if resolved != nil {
		resolved.Close()
	}
	code(t, e, "invalid_configuration")
}

func resolverManifest(version, extra string) []byte {
	return []byte(`{"schema_version":` + version + `,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]` + extra + `}`)
}

func TestResolverVersion1StillExactFields(t *testing.T) {
	for _, extra := range []string{`,"name":"suggested"`, `,"start_dir":"."`, `,"human_shell":"fish"`, `,"console_mode":"plain"`, `,"shortcuts":"on"`, `,"project":{"url":"https://example.test/org/app.git","path":"projects/app"}`, `,"agent_shell":"bash"`} {
		t.Run(extra, func(t *testing.T) {
			_, _, e := decodeResolverManifest(resolverManifest("1", extra))
			resolverManifestError(t, e, "invalid_configuration")
		})
	}
}
func TestResolverVersion2Accepted(t *testing.T) {
	for _, extra := range []string{"", `,"name":"suggested","start_dir":".","human_shell":"nu","console_mode":"plain","shortcuts":"off","project":{"url":"ssh://git@example.test/org/app.git","path":"projects/app"}`} {
		t.Run(extra, func(t *testing.T) {
			dc, projects, e := decodeResolverManifest(resolverManifest("2", extra))
			if e != nil {
				t.Fatal(e)
			}
			if projects != "projects" || dc != ".devcontainer/devcontainer.json" {
				t.Fatal("version changed declared resolver paths")
			}
		})
	}
}
func TestResolverRejectsVersionTrueAndFloat(t *testing.T) {
	for _, version := range []string{"true", "false", "1.0", "1e0", "1E0", "2.0", "2e0", `"2"`, "null"} {
		t.Run(version, func(t *testing.T) {
			_, _, e := decodeResolverManifest(resolverManifest(version, ""))
			resolverManifestError(t, e, "invalid_configuration")
		})
	}
}
func TestResolverIgnoresRuntimeSettings(t *testing.T) {
	for _, extra := range []string{"", `,"name":"suggested","start_dir":"projects/missing","human_shell":"zsh","console_mode":"herdr","shortcuts":"off","project":{"url":"https://unreachable.example.test/org/unfetched.git","path":"projects/unfetched"}`} {
		dc, projects, e := decodeResolverManifest(resolverManifest("2", extra))
		if e != nil {
			t.Fatal(e)
		}
		if dc != ".devcontainer/devcontainer.json" || projects != "projects" {
			t.Fatal("runtime settings altered provider inputs")
		}
	}
}
func TestResolverVersion2IntegrationSettingsIgnored(t *testing.T) {
	r, d := resolverFixture(t, func(repo string) {
		writeResolverManifest(t, repo, resolverManifest("2", `,"name":"suggested","start_dir":"projects/missing","human_shell":"zsh","console_mode":"herdr","shortcuts":"off","project":{"url":"https://unreachable.example.test/org/unfetched.git","path":"projects/unfetched"}`))
	})
	result, e := r.Resolve(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	defer result.Close()
	if len(result.Source) != 1 || result.Source["image"] != "ubuntu@sha256:"+strings.Repeat("a", 64) || len(result.Bases) != 1 || result.Bases[0] != result.Source["image"] || result.Descriptor.Commit != d.Commit {
		t.Fatalf("runtime settings altered resolution: %#v", result)
	}
	for _, name := range []string{"missing", "unfetched"} {
		if _, e = os.Stat(filepath.Join(result.ProjectsPath, name)); !os.IsNotExist(e) {
			t.Fatal("manifest setting materialized a runtime resource")
		}
	}
}

// Each malformed input has one defect relative to an accepted control. Shared
// vectors may have several defects, which cannot isolate these admission guards.
func TestResolverManifestSingleDefectRefusals(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("v"+version, func(t *testing.T) {
			valid := string(resolverManifest(version, ""))
			path := func(value string) []byte {
				return []byte(strings.Replace(valid, `".devcontainer/devcontainer.json"`, `"`+value+`"`, 1))
			}
			type defect struct {
				name string
				raw  []byte
			}
			cases := []defect{
				{"duplicate_literal", resolverManifest(version, `,"schema_version":`+version)},
				{"duplicate_escaped", resolverManifest(version, `,"schema\u005fversion":`+version)},
				{"lone_high_surrogate", path(`\ud800`)},
				{"lone_low_surrogate", path(`\udc00`)},
				{"high_then_non_surrogate", path(`\ud800\u0041`)},
				{"escaped_quote_then_high", path(`a\"\ud800`)},
				{"overlong_utf8", path(string([]byte{0xc0, 0xaf}))},
				{"cesu8_surrogate", path(string([]byte{0xed, 0xa0, 0x80}))},
				{"trailing_token", []byte(valid + "x")},
				{"trailing_object", []byte(valid + " {}")},
			}
			if version == "2" {
				cases = append(cases,
					defect{"unknown_v2_field", resolverManifest(version, `,"agent_shell":"bash"`)},
					defect{"nested_duplicate", resolverManifest(version, `,"project":{"url":"https://example.test/org/app.git","path":"projects/app","pa\u0074h":"projects/app"}`)})
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					_, _, err := decodeResolverManifest(tc.raw)
					resolverManifestError(t, err, "invalid_configuration")
				})
			}
			// Valid scalar pairs, replacement characters, quotes and whitespace
			// ensure broad Unicode or trailing-byte refusals cannot pass.
			for _, control := range [][]byte{[]byte(valid), path(`\ufffd`), path(`\ud800\udc00`), path(`a\"b`), []byte(valid + " \r\n\t")} {
				if _, _, err := decodeResolverManifest(control); err != nil {
					t.Fatalf("valid single-defect control refused: %q: %v", control, err)
				}
			}
		})
	}
}

func TestResolverVersion1AdmissionCharacterization(t *testing.T) {
	valid := string(resolverManifest("1", ""))
	for _, tc := range []struct {
		name string
		raw  []byte
		want string
	}{
		{"size_65537", []byte(valid + strings.Repeat(" ", 65537-len(valid))), "invalid_configuration"},
		{"bom", append([]byte{0xef, 0xbb, 0xbf}, []byte(valid)...), "invalid_configuration"},
		{"nul", append([]byte(valid), 0), "invalid_configuration"},
		{"depth_129", []byte(strings.Replace(valid, `"bundles":[]`, `"bundles":`+strings.Repeat("[", 128)+"0"+strings.Repeat("]", 128), 1)), "invalid_configuration"},
		{"bundles_non_array", []byte(strings.Replace(valid, `"bundles":[]`, `"bundles":{}`, 1)), "invalid_configuration"},
		{"bundles_nonempty_valid_paths", []byte(strings.Replace(valid, `"bundles":[]`, `"bundles":["future"]`, 1)), "unsupported_configuration"},
		{"bundles_nonempty_invalid_path", []byte(strings.Replace(strings.Replace(valid, `"bundles":[]`, `"bundles":["future"]`, 1), `"projects"`, `"../projects"`, 1)), "invalid_configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodeResolverManifest(tc.raw)
			resolverManifestError(t, err, tc.want)
		})
	}
	for _, field := range []string{"devcontainer_path", "projects_path"} {
		for _, alias := range []string{".GIT", ".git.", ".git ", "GIT~1", ".git:x", "...", "\u200b", "．git"} {
			t.Run(field+"/"+alias, func(t *testing.T) {
				old := `"` + field + `":"projects"`
				if field == "devcontainer_path" {
					old = `"devcontainer_path":".devcontainer/devcontainer.json"`
				}
				raw := strings.Replace(valid, old, `"`+field+`":"`+alias+`"`, 1)
				_, _, err := decodeResolverManifest([]byte(raw))
				resolverManifestError(t, err, "invalid_configuration")
			})
		}
	}
	if _, _, err := decodeResolverManifest([]byte(valid + strings.Repeat(" ", 65536-len(valid)))); err != nil {
		t.Fatalf("65536-byte v1 control refused: %v", err)
	}
}

func TestResolverManifestAdmissionBeforeMissingParser(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		want string
	}{
		{"invalid_manifest", resolverManifest("1", `,"unexpected":true`), "invalid_configuration"},
		{"valid_manifest", resolverManifest("1", ""), "tool_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A real HTTPS Git fixture and an absent parser exercise Resolve
			// ordering without requiring or imitating a Node toolchain.
			r, d := resolverFixtureWithParser(t, "sha1", func(repo string) {
				writeResolverManifest(t, repo, tc.raw)
			}, JSONC{})
			_, err := r.Resolve(context.Background(), d)
			resolverManifestError(t, err, tc.want)
			entries, err := os.ReadDir(r.StageRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed resolution retained stage: %v %v", entries, err)
			}
		})
	}
}
