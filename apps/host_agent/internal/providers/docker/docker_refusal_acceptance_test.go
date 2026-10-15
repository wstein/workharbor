//go:build docker_integration

// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/protocol"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/storage"
)

// Only the test selects this physical host executable. It preserves actual
// Docker stdio/exit/argv and records fixed categories, never repository content.
func installFixtureDockerLedger(t *testing.T, root, original string) (string, string) {
	t.Helper()
	if !physicalExecutable(original) {
		t.Fatal("fixture Docker executable is not physical")
	}
	ledger := filepath.Join(root, "docker-forwarded-categories")
	f, e := os.OpenFile(ledger, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	wrapper := filepath.Join(root, "docker-with-ledger")
	script := `#!/bin/sh
set -eu
if [ "$#" = 1 ] && [ "$1" = -v ]; then exec ` + shellQuote(original) + ` "$@"; fi
[ "$#" -ge 5 ] && [ "$1" = --config ] && [ "$3" = --host ] || exit 124
category=read
case "$5" in
 run|pull|start|stop|rm|exec|create) category="$5" ;;
 buildx) case "${6-}" in build) category=build ;; version) [ "$#" = 6 ] || exit 124 ;; *) exit 124 ;; esac ;;
 image|volume) [ "${6-}" = ls ] || exit 124 ;;
 inspect|ps|info|events|version) ;;
 *) exit 124 ;;
esac
if [ "$category" != read ]; then printf '%s\n' "$category" >> ` + shellQuote(ledger) + `; fi
exec ` + shellQuote(original) + ` "$@"
`
	realWrite(t, wrapper, []byte(script))
	if e = os.Chmod(wrapper, 0700); e != nil {
		t.Fatal(e)
	}
	return wrapper, ledger
}
func fixtureLedger(t *testing.T, path string) []byte {
	t.Helper()
	info, e := os.Lstat(path)
	if e != nil || !ownedRegular(info) || info.Mode().Perm() != 0600 || info.Size() > 65536 {
		t.Fatal("unsafe or oversized fixture ledger", e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func fixtureInventory(t *testing.T, ctx context.Context, p *nativePort) string {
	t.Helper()
	var b strings.Builder
	for _, args := range [][]string{{"ps", "-aq", "--no-trunc"}, {"image", "ls", "-q", "--no-trunc"}, {"volume", "ls", "-q"}} {
		raw, e := p.read(ctx, args)
		if e != nil {
			t.Fatal(e)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}
func assertRealRefusal(t *testing.T, ctx context.Context, p *nativePort, ledger, diagnostic string, action func() error) {
	t.Helper()
	before := fixtureInventory(t, ctx, p)
	forwarded := fixtureLedger(t, ledger)
	e := action()
	if e == nil || e.Error() != diagnostic {
		t.Fatalf("intended refusal %s not reached: %v", diagnostic, e)
	}
	if !bytes.Equal(forwarded, fixtureLedger(t, ledger)) {
		t.Fatal("refusal forwarded mutating Docker command")
	}
	if fixtureInventory(t, ctx, p) != before {
		t.Fatal("refusal changed actual Docker inventory")
	}
}
func realTrustInputs(t *testing.T, d config.Descriptor, b storage.Binding) (map[string]any, protocol.Context, config.Approval) {
	t.Helper()
	s := scopeFixture()
	request := map[string]any{"schema_version": int64(1), "workspace_id": s.WorkspaceID, "action": "create", "parameters": map[string]any{"host_id": s.HostID, "config": d.Map()}}
	identity := map[string]any{"schema_version": int64(1), "owner_id": s.OwnerID, "workspace_id": s.WorkspaceID, "action": "create", "parameters": request["parameters"]}
	raw, e := wirejson.CanonicalBytes(identity)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	approvalID := "00000000-0000-4000-8000-000000000009"
	command := map[string]any{"schema_version": int64(1), "kind": "command", "command_id": "00000000-0000-4000-8000-000000000010", "owner_id": s.OwnerID, "host_id": s.HostID, "workspace_id": s.WorkspaceID, "operation_id": s.AllocationOperationID, "idempotency_key": "fixture:create:1", "request_sha256": digest, "intent_version": int64(1), "request": request, "bindings": map[string]any{"target": nil, "candidate": map[string]any{"runtime_id": s.RuntimeID, "generation": int64(1)}}, "execution_config": nil, "approval_ref": approvalID}
	trusted := protocol.Context{Peer: &protocol.Peer{Kind: "control_plane", OwnerID: s.OwnerID, HostID: s.HostID}, Snapshot: &protocol.Snapshot{Command: command, RequestBytes: raw, RequestHash: digest, NativeBindings: map[string]any{"target": nil, "candidate": map[string]any{"id": s.RuntimeID, "generation": int64(1)}}, CurrentIntent: 1, Status: "queued"}}
	return command, trusted, config.Approval{ID: approvalID, OwnerID: s.OwnerID, Descriptor: d}
}
func realAdmissionRefusals(t *testing.T, ctx context.Context, host Provider, b storage.Binding, d config.Descriptor, root, ledger string) {
	t.Helper()
	port, e := openNativePort(ctx, nativeOptions{Policy: DriverPolicy{Phase: Preflight, Scope: scopeFixture()}, Scope: scopeFixture(), Check: func(ctx context.Context) error { return ctx.Err() }, DockerPath: host.DockerPath, Socket: host.Socket, BuildxPath: host.BuildxPath, PrivateDirectory: host.PrivateDirectory})
	if e != nil {
		t.Fatal(e)
	}
	defer port.Close()
	// The independently authored command matches this actual HTTPS/gitlink fixture.
	command, trusted, approval := realTrustInputs(t, d, b)
	if _, e := Admit(ctx, command, trusted, b, approval, func(ctx context.Context) error { return ctx.Err() }); e != nil {
		t.Fatal("positive exact trust control", e)
	}
	for _, kind := range []string{"approval-owner", "approval-revision", "approval-gitlink", "peer-host", "workspace-scope", "current-revoked"} {
		t.Run("admission-"+kind, func(t *testing.T) {
			command, trusted, approval := realTrustInputs(t, d, b)
			check := func(ctx context.Context) error { return ctx.Err() }
			want := "trust_required"
			switch kind {
			case "approval-owner":
				approval.OwnerID = b.StorageID
			case "approval-revision":
				approval.Descriptor.Commit = strings.Repeat("b", 40)
			case "approval-gitlink":
				approval.Descriptor.Submodules = append([]config.Submodule{}, d.Submodules...)
				approval.Descriptor.Submodules[0].Commit = strings.Repeat("b", 40)
			case "peer-host":
				trusted.Peer.HostID = b.StorageID
				want = "unauthorized_scope"
			case "workspace-scope":
				command = clone(command).(map[string]any)
				command["workspace_id"] = b.StorageID
				want = "unauthorized_scope"
			case "current-revoked":
				check = func(context.Context) error { return fail("forbidden") }
				want = "forbidden"
			}
			assertRealRefusal(t, ctx, port, ledger, want, func() error {
				a, e := Admit(ctx, command, trusted, b, approval, check)
				if e != nil {
					return e
				}
				p, e := host.Prepare(ctx, a)
				if p != nil {
					p.Close()
				}
				return e
			})
		})
	}
	for _, kind := range []string{"missing-revision", "missing-gitlink", "changed-gitlink"} {
		t.Run("revision-"+kind, func(t *testing.T) {
			bad, _ := config.DecodeDescriptor(d.Map())
			want := "invalid_configuration"
			switch kind {
			case "missing-revision":
				bad.Commit = strings.Repeat("f", 40)
				want = "revision_unavailable"
			case "missing-gitlink":
				bad.Submodules = []config.Submodule{}
			case "changed-gitlink":
				bad.Submodules[0].Commit = strings.Repeat("b", 40)
			}
			assertRealRefusal(t, ctx, port, ledger, want, func() error {
				p, e := host.Prepare(ctx, realAdmission(bad, b, 1))
				if p != nil {
					p.Close()
				}
				return e
			})
		})
	}
	repository := filepath.Join(root, "fixture-config")
	manifestPath, sourcePath, modulesPath := filepath.Join(repository, "workharbor.json"), filepath.Join(repository, ".devcontainer/devcontainer.json"), filepath.Join(repository, ".gitmodules")
	manifest, me := os.ReadFile(manifestPath)
	source, se := os.ReadFile(sourcePath)
	modules, ge := os.ReadFile(modulesPath)
	if me != nil || se != nil || ge != nil {
		t.Fatal("fixture source snapshot unavailable")
	}
	commit := func() config.Descriptor {
		realGit(t, ctx, repository, "add", "--all")
		realGit(t, ctx, repository, "commit", "--quiet", "--allow-empty", "-m", "refusal fixture")
		bad, _ := config.DecodeDescriptor(d.Map())
		bad.Commit = realGit(t, ctx, repository, "rev-parse", "HEAD")
		realGit(t, ctx, repository, "update-server-info")
		return bad
	}
	for _, kind := range []string{"manifest-path", "source-symlink", "jsonc-duplicate", "jsonc-surrogate", "ssh-submodule-no-credentials"} {
		t.Run("source-"+kind, func(t *testing.T) {
			if e := os.Remove(sourcePath); e != nil {
				t.Fatal(e)
			}
			realWrite(t, sourcePath, source)
			realWrite(t, manifestPath, manifest)
			realWrite(t, modulesPath, modules)
			want := "invalid_configuration"
			switch kind {
			case "manifest-path":
				var m map[string]any
				if json.Unmarshal(manifest, &m) != nil {
					t.Fatal("manifest snapshot")
				}
				m["devcontainer_path"] = "../outside.json"
				raw, _ := json.Marshal(m)
				realWrite(t, manifestPath, raw)
			case "source-symlink":
				if e := os.Remove(sourcePath); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("../../outside.json", sourcePath); e != nil {
					t.Fatal(e)
				}
				want = "context_schema"
			case "jsonc-duplicate":
				realWrite(t, sourcePath, []byte(`{"image":"one","\u0069mage":"two"}`))
			case "jsonc-surrogate":
				realWrite(t, sourcePath, []byte(`{"image":"\ud800"}`))
			case "ssh-submodule-no-credentials":
				realWrite(t, modulesPath, []byte("[submodule \"app\"]\npath=projects/app\nurl=ssh://git@example.invalid/project.git\n"))
				want = "credential_unavailable"
			}
			bad := commit()
			assertRealRefusal(t, ctx, port, ledger, want, func() error {
				p, e := host.Prepare(ctx, realAdmission(bad, b, 1))
				if p != nil {
					p.Close()
				}
				return e
			})
		})
	}
	if e := os.Remove(sourcePath); e != nil {
		t.Fatal(e)
	}
	realWrite(t, sourcePath, source)
	realWrite(t, manifestPath, manifest)
	realWrite(t, modulesPath, modules)
	commit()
	if _, e := os.Lstat(filepath.Join(host.Store.Root, b.StorageID)); !os.IsNotExist(e) {
		t.Fatal("admission refusal seeded data", e)
	}
}

func realRuntimeRefusals(t *testing.T, ctx context.Context, host Provider, b storage.Binding, d config.Descriptor, p *Prepared, root, ledger string) {
	t.Helper()
	p.port.stateMu.Lock()
	policy := copyPolicy(p.port.policy)
	policy.Phase = Hooks
	target := *p.port.target
	images := copyImages(p.port.images)
	p.port.stateMu.Unlock()
	for _, kind := range []string{"duplicate-runtime", "partial-runtime-labels", "reserved-name-collision", "internal-wrong-target"} {
		t.Run("runtime-"+kind, func(t *testing.T) {
			if kind == "internal-wrong-target" {
				wrong := strings.Repeat("f", 64)
				policy.KnownIDs[wrong] = true
				g := &Gateway{Parser: p.port.parser, Policy: copyPolicy(policy), Port: p.port, Authorize: p.Check, Target: &target, Images: images}
				assertRealRefusal(t, ctx, p.port, ledger, "runtime_conflict", func() error {
					_, e := g.Forward(ctx, []string{"exec", "-i", "-u", policy.RemoteUser, "-w", "/workspace", wrong, "true"})
					return e
				})
				return
			}
			name := "workharbor-fixture-" + filepath.Base(root) + "-" + kind
			marker := name
			labels := map[string]string{"workharbor.test.fixture": marker}
			if kind == "duplicate-runtime" {
				for k, v := range target.Scope.Labels() {
					labels[k] = v
				}
			}
			if kind == "partial-runtime-labels" {
				labels["workharbor.runtime"] = target.Scope.RuntimeID
			}
			collision := realAdmission(d, b, 5)
			if kind == "reserved-name-collision" {
				name = collision.Scope().Name()
			}
			args := []string{"create", "--pull=never", "--name", name}
			keys := []string{}
			for k := range labels {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				args = append(args, "--label", k+"="+labels[k])
			}
			args = append(args, target.ImageID, "/bin/true")
			raw, e := p.port.read(ctx, args)
			if e != nil {
				t.Fatal("owned collision setup", e)
			}
			id := strings.TrimSpace(string(raw))
			if !hexID.MatchString(id) {
				t.Fatal("collision setup returned no exact ID")
			}
			verify := func(checkCtx context.Context) {
				t.Helper()
				raw, e := p.port.read(checkCtx, []string{"inspect", "--type", "container", id})
				if e != nil {
					t.Fatal("exact fixture inspect", e)
				}
				c, e := nativeContainers(raw)
				if e != nil || len(c) != 1 || c[0].ID != id || c[0].Name != "/"+name || c[0].Image != target.ImageID || c[0].Running || c[0].Labels["workharbor.test.fixture"] != marker {
					t.Fatal("foreign collision fixture", e)
				}
			}
			verify(ctx)
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				verify(cleanup)
				if _, e := p.port.read(cleanup, []string{"rm", id}); e != nil {
					t.Error("owned collision teardown", e)
					return
				}
				raw, e := p.port.read(cleanup, []string{"ps", "-aq", "--no-trunc"})
				if e != nil || strings.Contains(string(raw), id) {
					t.Error("collision removal not verified", e)
				}
			}()
			assertRealRefusal(t, ctx, p.port, ledger, "runtime_conflict", func() error {
				if kind == "reserved-name-collision" {
					candidate, e := host.Prepare(ctx, collision)
					if candidate != nil {
						candidate.Close()
					}
					return e
				}
				g := &Gateway{Parser: p.port.parser, Policy: copyPolicy(policy), Port: p.port, Authorize: p.Check, Target: &target, Images: images}
				_, e := g.Forward(ctx, []string{"exec", "-i", "-u", policy.RemoteUser, "-w", "/workspace", target.ContainerID, "true"})
				return e
			})
		})
	}
}

// These transport controls use an owned shell fixture, not Docker acceptance.
func TestDockerFixtureFixedLedgerTransport(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	original := filepath.Join(root, "owned-transport-fixture")
	realWrite(t, original, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\nexit 17\n"))
	if e = os.Chmod(original, 0700); e != nil {
		t.Fatal(e)
	}
	wrapper, ledger := installFixtureDockerLedger(t, root, original)
	for _, row := range []struct {
		args     []string
		code     int
		category string
	}{
		{[]string{"-v"}, 17, ""},
		{[]string{"--config", "private", "--host", "unix://owned", "exec", "literal with space"}, 17, "exec\n"},
		{[]string{"--config", "private", "--host", "unix://owned", "buildx", "build", "literal"}, 17, "build\n"},
		{[]string{"--config", "private", "--host", "unix://owned", "buildx", "version"}, 17, ""},
		{[]string{"--config", "private", "--host", "unix://owned", "image", "rm", "foreign"}, 124, ""},
		{[]string{"--host", "unix://foreign", "run"}, 124, ""},
	} {
		before := fixtureLedger(t, ledger)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c := exec.CommandContext(ctx, wrapper, row.args...)
		c.Env = []string{"PATH=/usr/bin:/bin"}
		raw, e := c.Output()
		cancel()
		exit, ok := e.(*exec.ExitError)
		if !ok || exit.ExitCode() != row.code {
			t.Fatalf("transport exit: %v", e)
		}
		if row.code == 17 && string(raw) != strings.Join(row.args, "\n")+"\n" {
			t.Fatalf("argv changed: %q", raw)
		}
		if row.code == 124 && len(raw) != 0 {
			t.Fatal("unknown shape reached original executable")
		}
		if !bytes.Equal(append(append([]byte{}, before...), []byte(row.category)...), fixtureLedger(t, ledger)) {
			t.Fatal("ledger category mismatch")
		}
	}
}
func TestDockerFixtureIndependentTrustCommandMatchesExactInputs(t *testing.T) {
	d := config.Descriptor{RepositoryIdentity: "https://fixture.example/config.git", Commit: strings.Repeat("a", 40), Submodules: []config.Submodule{{Path: "projects/app", Commit: strings.Repeat("b", 40)}}}
	s := scopeFixture()
	b := storage.Binding{OwnerID: s.OwnerID, HostID: s.HostID, WorkspaceID: s.WorkspaceID, StorageID: s.StorageID, ReservationOperationID: s.AllocationOperationID, Initial: d}
	command, trusted, approval := realTrustInputs(t, d, b)
	admitted, e := Admit(context.Background(), command, trusted, b, approval, func(context.Context) error { return nil })
	if e != nil || admitted.Descriptor().Commit != d.Commit || admitted.Scope() != s {
		t.Fatalf("independent trusted fixture mismatch: %v %#v", e, admitted.Scope())
	}
}
