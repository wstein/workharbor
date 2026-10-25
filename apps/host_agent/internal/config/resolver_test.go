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
	return Resolver{GitPath: git, StageRoot: stageRoot, CAFile: ca, Parser: jsoncTool(t)}, Descriptor{RepositoryIdentity: server.URL + "/", Commit: commit, Submodules: []Submodule{}}
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
