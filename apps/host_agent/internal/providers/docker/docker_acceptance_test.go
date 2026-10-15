//go:build docker_integration

// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/devcontainer"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/storage"
)

// These explicit real-Docker roots NEVER skip or substitute a recording port.
// They run only in the separately reviewed disposable Linux fixture. The helper
// owns whole-guest containment/ancestry/teardown; these tests do not attest it.
func TestDockerAcceptanceImageFreshCache(t *testing.T) { dockerAcceptance(t, "admitted-image") }
func TestDockerAcceptanceDockerfileFreshCache(t *testing.T) {
	dockerAcceptance(t, "admitted-dockerfile")
}
func realEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if !filepath.IsAbs(v) || filepath.Clean(v) != v {
		t.Fatalf("missing/invalid required real-fixture input %s", key)
	}
	return v
}
func realGit(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	git, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(filepath.Dir(dir), "git-templates"), 0700); e != nil {
		t.Fatal(e)
	}
	c := exec.CommandContext(ctx, git, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "init.templateDir=" + filepath.Join(filepath.Dir(dir), "git-templates")}, args...)...)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "LANG=C.UTF-8", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_TERMINAL_PROMPT=0"}
	raw, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("fixture Git %q: %v %s", args, e, raw)
	}
	return strings.TrimSpace(string(raw))
}
func realWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func realHash(t *testing.T, path string) string {
	t.Helper()
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func fixtureConfig(t *testing.T, ctx context.Context, root, example string) (config.Resolver, config.Descriptor, func(map[string]any) config.Descriptor) {
	t.Helper()
	repository := filepath.Join(root, "fixture-config")
	project := filepath.Join(root, "fixture-project")
	stage := filepath.Join(root, "staging")
	for _, dir := range []string{repository, project, stage} {
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	realGit(t, ctx, project, "init", "--quiet")
	realWrite(t, filepath.Join(project, "README.md"), []byte("independently authored project\n"))
	realWrite(t, filepath.Join(project, "seed.bin"), []byte{0, 1, 255, 128})
	realGit(t, ctx, project, "add", "--all")
	realGit(t, ctx, project, "commit", "--quiet", "-m", "project fixture")
	projectCommit := realGit(t, ctx, project, "rev-parse", "HEAD")
	realGit(t, ctx, project, "update-server-info")
	mux := http.NewServeMux()
	mux.Handle("/config.git/", http.StripPrefix("/config.git/", http.FileServer(http.Dir(filepath.Join(repository, ".git")))))
	mux.Handle("/project.git/", http.StripPrefix("/project.git/", http.FileServer(http.Dir(filepath.Join(project, ".git")))))
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	ca := filepath.Join(root, "git-ca.pem")
	realWrite(t, ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	realGit(t, ctx, repository, "init", "--quiet")
	os.MkdirAll(filepath.Join(repository, "projects"), 0700)
	realGit(t, ctx, repository, "-c", "protocol.file.allow=always", "submodule", "add", "--quiet", project, "projects/app")
	realWrite(t, filepath.Join(repository, ".gitmodules"), []byte("[submodule \"app\"]\npath=projects/app\nurl="+server.URL+"/project.git/\n"))
	examples, _ := filepath.Abs("../../../../../examples/workspace/" + example)
	for _, file := range []string{"workharbor.json", ".devcontainer/devcontainer.json"} {
		raw, e := os.ReadFile(filepath.Join(examples, file))
		if e != nil {
			t.Fatal(e)
		}
		realWrite(t, filepath.Join(repository, file), raw)
	}
	if example == "admitted-dockerfile" {
		raw, e := os.ReadFile(filepath.Join(examples, ".devcontainer/Dockerfile"))
		if e != nil {
			t.Fatal(e)
		}
		hostile, _ := json.Marshal([]map[string]any{{"initializeCommand": "touch " + filepath.Join(root, "AUTHORED_LABEL_MUST_NOT_RUN")}})
		quoted, _ := json.Marshal(string(hostile))
		raw = append(raw, []byte("\nLABEL devcontainer.metadata="+string(quoted)+"\n")...)
		realWrite(t, filepath.Join(repository, ".devcontainer/Dockerfile"), raw)
	}
	commit := func() config.Descriptor {
		realGit(t, ctx, repository, "add", "--all")
		realGit(t, ctx, repository, "commit", "--quiet", "-m", "config fixture")
		revision := realGit(t, ctx, repository, "rev-parse", "HEAD")
		realGit(t, ctx, repository, "update-server-info")
		return config.Descriptor{RepositoryIdentity: server.URL + "/config.git/", Commit: revision, Submodules: []config.Submodule{{Path: "projects/app", Commit: projectCommit}}}
	}
	descriptor := commit()
	git, _ := exec.LookPath("git")
	parser := config.JSONC{NodePath: realEnv(t, "WORKHARBOR_NODE_PATH"), CompanionDir: realEnv(t, "WORKHARBOR_DEVCONTAINER_ROOT")}
	resolver := config.Resolver{GitPath: git, StageRoot: stage, CAFile: ca, Parser: parser}
	mutate := func(source map[string]any) config.Descriptor {
		raw, _ := json.Marshal(source)
		realWrite(t, filepath.Join(repository, ".devcontainer/devcontainer.json"), raw)
		return commit()
	}
	return resolver, descriptor, mutate
}
func realHost(t *testing.T, root string, r config.Resolver) Provider {
	t.Helper()
	for _, name := range []string{"private", "storage"} {
		if e := os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	node := realEnv(t, "WORKHARBOR_NODE_PATH")
	tools := realEnv(t, "WORKHARBOR_DEVCONTAINER_ROOT")
	buildx := realEnv(t, "WORKHARBOR_BUILDX_PATH")
	return Provider{Resolver: r, Tools: devcontainer.Tools{NodePath: node, Root: tools}, Registry: Registry{BuildxPath: buildx, CranePath: realEnv(t, "WORKHARBOR_CRANE_PATH"), PrivateDirectory: filepath.Join(root, "private"), Architecture: runtime.GOARCH, Parser: r.Parser}, Store: storage.Store{Root: filepath.Join(root, "storage")}, DockerPath: realEnv(t, "WORKHARBOR_DOCKER_PATH"), Socket: realEnv(t, "WORKHARBOR_DOCKER_SOCKET"), BuildxPath: buildx, AgentPath: realEnv(t, "WORKHARBOR_DOCKER_TEST_AGENT"), PrivateDirectory: filepath.Join(root, "private")}
}
func realAdmission(d config.Descriptor, b storage.Binding, generation uint64) Admission {
	scope := scopeFixture()
	scope.StorageID = b.StorageID
	scope.Generation = generation
	if generation > 1 {
		scope.RuntimeID = fmt.Sprintf("00000000-0000-4000-8000-%012x", 64+generation*2)
		scope.AllocationOperationID = fmt.Sprintf("00000000-0000-4000-8000-%012x", 65+generation*2)
	}
	return Admission{scope: scope, descriptor: d, binding: b, authorize: func(ctx context.Context) error { return ctx.Err() }}
}
func realExec(t *testing.T, ctx context.Context, p *Prepared, command ...string) []byte {
	t.Helper()
	p.port.stateMu.Lock()
	policy := copyPolicy(p.port.policy)
	policy.Phase = Hooks
	p.port.policy = copyPolicy(policy)
	target := *p.port.target
	images := copyImages(p.port.images)
	p.port.stateMu.Unlock()
	gateway := &Gateway{Parser: p.port.parser, Policy: policy, Port: p.port, Authorize: p.Check, Target: &target, Images: images}
	args := []string{"exec", "-i", "-u", policy.RemoteUser, "-w", "/workspace", target.ContainerID}
	args = append(args, command...)
	var out bytes.Buffer
	if e := gateway.ForwardStream(ctx, args, bytes.NewReader(nil), &out); e != nil {
		t.Fatal("exact runtime exec", e)
	}
	return out.Bytes()
}
func fixtureRuntimeAction(t *testing.T, ctx context.Context, p *Prepared, target Target, action string) {
	t.Helper()
	all, e := p.port.Candidates(ctx, target.Scope)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyTarget(target, all); e != nil {
		t.Fatal(e)
	}
	var args []string
	switch action {
	case "stop":
		args = []string{"stop", "--time", "5", target.ContainerID}
	case "start":
		args = []string{"start", target.ContainerID}
	case "remove":
		if all[0].Running {
			t.Fatal("remove running fixture target")
		}
		args = []string{"rm", target.ContainerID}
	default:
		t.Fatal("unknown fixture action")
	}
	// Test-owned fixture lifecycle only; this is not product T010 dispatch/recovery.
	if _, e = p.port.read(ctx, args); e != nil {
		t.Fatal("fixture runtime action", action, e)
	}
	if action != "remove" {
		all, e = p.port.Candidates(ctx, target.Scope)
		if e != nil {
			t.Fatal(e)
		}
		actual, e := VerifyTarget(target, all)
		if e != nil || actual.Running != (action == "start") {
			t.Fatal("unacknowledged fixture state", actual, e)
		}
	}
}
func dockerAcceptance(t *testing.T, example string) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Fatal("real acceptance requires reviewed rootful Linux fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	evidenceRoot := realEnv(t, "WORKHARBOR_T009_ARTIFACT_DIRECTORY")
	if !ownedDirectory(evidenceRoot) {
		t.Fatal("evidence root is not privately owned")
	}
	root, e := os.MkdirTemp(evidenceRoot, example+"-")
	if e != nil {
		t.Fatal(e)
	}
	artifactManifest := map[string]any{"schema_version": 1, "redacted": true, "files": []any{map[string]any{"path": "outcome.json", "max_bytes": 4096}, map[string]any{"path": "proof.json", "max_bytes": 65536}, map[string]any{"path": "raw-registry-proof.json", "max_bytes": 65536}}, "max_files": 3, "max_total_bytes": 135168}
	manifestBytes, _ := json.Marshal(artifactManifest)
	realWrite(t, filepath.Join(root, "artifact-manifest.json"), append(manifestBytes, '\n'))
	finished := false
	defer func() {
		outcome := "failed_or_unknown"
		if finished && !t.Failed() {
			outcome = "passed"
		}
		raw, _ := json.Marshal(map[string]any{"schema_version": 1, "example": example, "outcome": outcome, "feature006_t010": "not executed", "containment": "separate helper evidence required"})
		os.WriteFile(filepath.Join(root, "outcome.json"), append(raw, '\n'), 0600)
	}()
	resolver, descriptor, mutate := fixtureConfig(t, ctx, root, example)
	host := realHost(t, root, resolver)
	var ledger string
	host.DockerPath, ledger = installFixtureDockerLedger(t, root, host.DockerPath)
	scope := scopeFixture()
	binding := storage.Binding{OwnerID: scope.OwnerID, HostID: scope.HostID, WorkspaceID: scope.WorkspaceID, StorageID: scope.StorageID, ReservationOperationID: scope.AllocationOperationID, Initial: descriptor}
	originalBytes, e := os.ReadFile(filepath.Join(root, "fixture-config/.devcontainer/devcontainer.json"))
	if e != nil {
		t.Fatal(e)
	}
	var originalSource map[string]any
	if json.Unmarshal(originalBytes, &originalSource) != nil {
		t.Fatal("fixture source malformed")
	}
	realAdmissionRefusals(t, ctx, host, binding, descriptor, root, ledger)
	if t.Failed() {
		return
	}
	rawRegistryRefusals(t, ctx, host, binding, mutate, originalSource, root, ledger)
	if t.Failed() {
		return
	}
	p, e := host.Prepare(ctx, realAdmission(descriptor, binding, 1))
	if e != nil {
		t.Fatal("real preparation", e)
	}
	defer p.Close()
	resource, e := p.Resources(ctx)
	if e != nil {
		t.Fatal("real official resource phase", e)
	}
	hooks, e := p.Hooks(ctx)
	if e != nil || hooks.Target != resource.Target {
		t.Fatal("real official hooks", e)
	}
	if _, e := p.Resources(ctx); e == nil {
		t.Fatal("resource phase automatically replayed")
	}
	// Explicit fixture-only official reopen of the already verified exact target
	// proves container-label reuse. This does not add a production retry/recovery API.
	reopened, e := p.runPhase(ctx, Resources, resource.Target.ContainerID)
	if e != nil || reopened.Target != resource.Target {
		t.Fatal("official exact-target reopen", e)
	}
	realRuntimeRefusals(t, ctx, host, binding, descriptor, p, root, ledger)
	if t.Failed() {
		return
	}
	realExec(t, ctx, p, "sh", "-c", "cd /workspace/app && git status --porcelain && chmod +x seed.bin && printf '\\000\\377\\200\\001' > seed.bin && printf '\\000untracked\\377' > untracked.bin && git status --porcelain")
	data := p.Storage().Data
	binary := filepath.Join(data, "app/seed.bin")
	untracked := filepath.Join(data, "app/untracked.bin")
	hashes := map[string]string{"binary": realHash(t, binary), "untracked": realHash(t, untracked)}
	if raw, e := os.ReadFile(binary); e != nil || !bytes.Equal(raw, []byte{0, 255, 128, 1}) {
		t.Fatal("nonroot write/chmod", raw, e)
	}
	if info, e := os.Stat(binary); e != nil || info.Mode().Perm()&0111 == 0 {
		t.Fatal("nonroot executable bit", e)
	}
	assertRetained := func() {
		if realHash(t, binary) != hashes["binary"] || realHash(t, untracked) != hashes["untracked"] {
			t.Fatal("retained exact binary/untracked hashes changed")
		}
	}
	fixtureRuntimeAction(t, ctx, p, resource.Target, "stop")
	assertRetained()
	fixtureRuntimeAction(t, ctx, p, resource.Target, "start")
	assertRetained()
	fixtureRuntimeAction(t, ctx, p, resource.Target, "stop")
	// Changed approved execution config must retain ORIGINAL seeded data/owner.
	source := clone(p.source.Source).(map[string]any)
	source["postStartCommand"] = []any{"sh", "-c", "test -f /workspace/app/untracked.bin"}
	changed := mutate(source)
	binding.ExistingHandle = p.Storage().Parent
	replacement, e := host.Prepare(ctx, realAdmission(changed, binding, 2))
	if e != nil {
		t.Fatal("changed config reuse", e)
	}
	defer replacement.Close()
	assertRetained()
	replacementResource, e := replacement.Resources(ctx)
	if e != nil {
		t.Fatal("real replacement", e)
	}
	if _, e = replacement.Hooks(ctx); e != nil {
		t.Fatal(e)
	}
	assertRetained()
	fixtureRuntimeAction(t, ctx, replacement, replacementResource.Target, "stop")
	fixtureRuntimeAction(t, ctx, replacement, replacementResource.Target, "remove")
	fixtureRuntimeAction(t, ctx, p, resource.Target, "remove")
	assertRetained()
	// An admitted but unsuccessful build cannot reset existing data or register
	// its generated tag as a usable runtime image.
	failedSource := clone(source).(map[string]any)
	delete(failedSource, "image")
	failedSource["build"] = map[string]any{"dockerfile": "Dockerfile", "context": "."}
	pinned := "docker.io/library/buildpack-deps@sha256:4163ac2c58660c533f3cf3d178551287ddd0ac18269ef84f3a382d0698c89254"
	realWrite(t, filepath.Join(root, "fixture-config/.devcontainer/Dockerfile"), []byte("FROM "+pinned+"\nRUN exit 17\n"))
	failedDescriptor := mutate(failedSource)
	failed, e := host.Prepare(ctx, realAdmission(failedDescriptor, binding, 3))
	if e != nil {
		t.Fatal("failed build preparation", e)
	}
	if _, e = failed.Resources(ctx); e == nil {
		failed.Close()
		t.Fatal("failed build reported success")
	}
	failed.Close()
	assertRetained()
	if _, e := os.Lstat(filepath.Join(root, "AUTHORED_LABEL_MUST_NOT_RUN")); !os.IsNotExist(e) {
		t.Fatal("authored image metadata host hook ran", e)
	}
	// Independently authored unsupported source cases must refuse before any
	// new image/container/volume mutation or hook, preserving retained bytes.
	inventory := func() string {
		var out strings.Builder
		for _, args := range [][]string{{"image", "ls", "-q", "--no-trunc"}, {"ps", "-aq", "--no-trunc"}, {"volume", "ls", "-q"}} {
			raw, e := p.port.read(ctx, args)
			if e != nil {
				t.Fatal(e)
			}
			out.Write(raw)
			out.WriteByte('\n')
		}
		return out.String()
	}
	before := inventory()
	for _, kind := range []string{"initializeCommand", "features", "mounts", "named-user", "VOLUME", "ONBUILD", "custom-frontend", "reserved-build-arg", "reserved-later-ARG", "compose", "source-runArgs"} {
		t.Run("unsupported-"+kind, func(t *testing.T) {
			bad := clone(source).(map[string]any)
			switch kind {
			case "initializeCommand":
				bad["initializeCommand"] = "touch " + filepath.Join(root, "SOURCE_HOST_MUST_NOT_RUN")
			case "features":
				bad["features"] = map[string]any{"ghcr.io/workharbor-fixture/must-not-fetch": map[string]any{}}
			case "mounts":
				bad["mounts"] = []any{"/var/run/docker.sock"}
			case "named-user":
				bad["remoteUser"] = "vscode"
			case "compose":
				bad["dockerComposeFile"] = "compose.yml"
				bad["service"] = "workspace"
			case "source-runArgs":
				bad["runArgs"] = []any{"--privileged"}
			default:
				delete(bad, "image")
				b := map[string]any{"dockerfile": "Dockerfile", "context": "."}
				bad["build"] = b
				dockerfile := "FROM " + pinned + "\n"
				switch kind {
				case "VOLUME":
					dockerfile += "VOLUME /workspace/app\n"
				case "ONBUILD":
					dockerfile += "ONBUILD RUN touch /workspace/MUST_NOT_RUN\n"
				case "custom-frontend":
					dockerfile = "# syntax=evil.example/frontend\n" + dockerfile
				case "reserved-build-arg":
					b["args"] = map[string]any{"BUILDKIT_SYNTAX": "evil.example/frontend"}
				case "reserved-later-ARG":
					dockerfile += "ARG OK BUILDKIT_SYNTAX=evil.example/frontend\n"
				}
				realWrite(t, filepath.Join(root, "fixture-config/.devcontainer/Dockerfile"), []byte(dockerfile))
			}
			unsupported := mutate(bad)
			assertRealRefusal(t, ctx, p.port, ledger, "unsupported_configuration", func() error {
				candidate, e := host.Prepare(ctx, realAdmission(unsupported, binding, 4))
				if candidate != nil {
					candidate.Close()
				}
				return e
			})
			if inventory() != before {
				t.Fatal("refusal changed Docker inventory", kind)
			}
			assertRetained()
			if _, e := os.Lstat(filepath.Join(root, "SOURCE_HOST_MUST_NOT_RUN")); !os.IsNotExist(e) {
				t.Fatal("source host hook ran", e)
			}
		})
	}

	if t.Failed() {
		return
	}
	forwarded := string(fixtureLedger(t, ledger))
	if !strings.Contains(forwarded, "run\n") || !strings.Contains(forwarded, "pull\n") || (example == "admitted-dockerfile" && !strings.Contains(forwarded, "build\n")) {
		t.Fatal("actual mutating-forward ledger positive control missing")
	}
	proof := map[string]any{"refusals_zero_mutating_forwards": true, "forwarded_categories": strings.Split(strings.TrimSpace(forwarded), "\n"), "example": example, "initial_config": descriptor.Map(), "replacement_config": changed.Map(), "target": resource.Target, "replacement_target": replacementResource.Target, "hashes": hashes, "owner_uid": p.Storage().Binding.DataUID, "owner_gid": p.Storage().Binding.DataGID, "real_normal_resource_hooks": true, "explicit_fixture_official_reopen": true, "automatic_resource_replay_refused": true, "retention_stop_start_replacement": true, "failed_build_retained": true, "authored_label_host_hook_absent": true, "unsupported_host_hook_refused": true, "guest_containment": "separate helper evidence required", "feature006_t010": "not executed"}
	raw, _ := json.MarshalIndent(proof, "", "  ")
	realWrite(t, filepath.Join(root, "proof.json"), append(raw, '\n'))
	finished = true
}
