//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func fixtureStagePreparation(t *testing.T) (Client, StageOptions, stagePreparationPorts) {
	return fixtureStagePreparationNamed(t, "Anchorage")
}

func fixtureStagePreparationNamed(t *testing.T, basename string) (Client, StageOptions, stagePreparationPorts) {
	t.Helper()
	source := filepath.Join(t.TempDir(), basename)
	if e := os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, source, "init", "--template=", "--initial-branch=main")
	if e := os.Mkdir(filepath.Join(source, ".devcontainer"), 0700); e != nil {
		t.Fatal(e)
	}
	for name, data := range map[string][]byte{".devcontainer/devcontainer.json": []byte(`{"image":"example:local","workspaceFolder":"/workspaces/Anchorage","remoteUser":"vscode","postCreateCommand":"sh .devcontainer/setup.sh"}`), ".devcontainer/setup.sh": []byte("printf hook\n"), ".devcontainer/host.env": stageFactsFixture(), ".devcontainer/smoke-container.sh": []byte("printf smoke\n"), "file": []byte("owner work\n")} {
		if e := os.WriteFile(filepath.Join(source, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	fixtureGit(t, source, "add", ".")
	fixtureGit(t, source, "commit", "-m", "baseline")
	socket := fixtureStageSocketPath(t)
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { listener.Close() })
	id := strings.Repeat("a", 64)
	inspectionData := map[string]any{"Id": id, "Name": "/primary", "Image": "example:local", "State": "running", "Labels": map[string]string{"devcontainer.local_folder": source, "devcontainer.config_file": filepath.Join(source, ".devcontainer/devcontainer.json")}, "Mounts": []any{map[string]any{"Type": "bind", "Source": source, "Destination": "/workspaces/Anchorage", "RW": true}, map[string]any{"Type": "volume", "Name": "anchorage-home-owner", "Source": "/var/lib/docker/volumes/anchorage-home-owner/_data", "Destination": "/home/vscode", "RW": true}}}
	b, _ := json.Marshal(inspectionData)
	tools := t.TempDir()
	docker := filepath.Join(tools, "docker")
	node := filepath.Join(tools, "node")
	cli := filepath.Join(tools, "devcontainer.js")
	script := "#!/bin/sh\nshift 2\ncase \"$1:$2\" in\n--version:) printf 'Docker version fixture\\n';;\ninfo:--format) printf '%s\\n' '{\"ID\":\"daemon\",\"OSType\":\"linux\",\"OperatingSystem\":\"Debian\",\"KernelVersion\":\"fixture\",\"Name\":\"fixture\",\"Architecture\":\"x86_64\"}' ;;\ncontainer:ls) printf '%s\\n' '{\"ID\":\"" + id + "\",\"Status\":\"Up\"}' ;;\ncontainer:inspect) printf '%s\\n' " + stageShellLiteral(string(b)) + " ;;\n*) exit 83 ;;\nesac\n"
	for path, data := range map[string]string{docker: script, node: "#!/bin/sh\nif test \"$1\" = --version; then printf 'v22.0.0\\n'; elif test \"$2\" = --version; then printf '0.89.0\\n'; else printf '%s\\n' '{\"configuration\":{\"image\":\"example:local\"},\"mergedConfiguration\":{\"image\":\"example:local\"}}'; fi\n", cli: "#!/usr/bin/env node\n// official CLI entry fixture\n"} {
		if e = os.WriteFile(path, []byte(data), 0700); e != nil {
			t.Fatal(e)
		}
	}
	git, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	sh, e := exec.LookPath("sh")
	if e != nil {
		t.Fatal(e)
	}
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	p := stagePreparationPorts{Endpoint: func(context.Context) (string, error) { return "unix://" + socket, nil }, Tools: map[string]string{"docker": docker, "node": node, "devcontainer": cli, "git": git, "sh": sh, "bash": bash}, StateRoot: filepath.Join(fixtureStageStateParent(t), "absent-state"), MountAdmission: func(context.Context, stageDaemonInfo, inspection, string) error { return nil }}
	return Client{Executable: docker}, StageOptions{Source: source, Target: "primary"}, p
}

func TestLocalStageSourceBasenameCannotCollideWithPrivateNamespaces(t *testing.T) {
	for _, name := range []string{"phase", "candidate", "configuration", " "} {
		t.Run(name, func(t *testing.T) {
			c, opts, p := fixtureStagePreparationNamed(t, name)
			s, e := c.prepareStage(context.Background(), opts, p)
			if e != nil {
				t.Fatalf("valid source basename refused: %v", e)
			}
			defer s.Close()
			if filepath.Base(s.capture.Path) != name || s.capture.Path == s.git.Clone {
				t.Fatalf("execution/clone routing: %s %s", s.capture.Path, s.git.Clone)
			}
		})
	}
}

func TestLocalStageIndependentPreviewsHaveStableTrustAndNoPersistentEffects(t *testing.T) {
	c, opts, p := fixtureStagePreparation(t)
	var diagnostics bytes.Buffer
	opts.Diagnostics = &diagnostics
	first, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	a := first.Preview()
	if a.Verification != nil || a.Fingerprint == nil {
		t.Fatalf("prebuild preview: %+v", a)
	}
	if e = first.Close(); e != nil {
		t.Fatal(e)
	}
	second, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	b := second.Preview()
	if e = second.Close(); e != nil {
		t.Fatal(e)
	}
	if *a.Fingerprint != *b.Fingerprint {
		t.Fatalf("allocation affected trust: %s != %s", *a.Fingerprint, *b.Fingerprint)
	}
	if _, e = os.Stat(p.StateRoot); !os.IsNotExist(e) {
		t.Fatalf("preview created persistent state: %v", e)
	}
	for _, disclosure := range []string{"Pause input writers", "host initialization", "container hook", "read-write", "verification policy"} {
		if !strings.Contains(diagnostics.String(), disclosure) {
			t.Fatalf("missing pre-consent disclosure %q: %s", disclosure, diagnostics.String())
		}
	}
	if e = os.WriteFile(filepath.Join(opts.Source, ".devcontainer/setup.sh"), []byte("printf changed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	third, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer third.Close()
	if *third.Preview().Fingerprint == *a.Fingerprint {
		t.Fatal("executable source change retained trust fingerprint")
	}
}

func TestLocalStagePreviewBindsToolBytesAndAllowedSubstitution(t *testing.T) {
	c, opts, p := fixtureStagePreparation(t)
	first, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	fingerprint := *first.Preview().Fingerprint
	if e = first.Close(); e != nil {
		t.Fatal(e)
	}
	t.Setenv("ANCHORAGE_HOST_LABEL", "changed admitted label")
	second, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	if *second.Preview().Fingerprint == fingerprint {
		t.Fatal("changed admitted substitution retained trust")
	}
	fingerprint = *second.Preview().Fingerprint
	if e = second.Close(); e != nil {
		t.Fatal(e)
	}
	file, e := os.OpenFile(p.Tools["node"], os.O_APPEND|os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	_, e = file.WriteString("\n# tool content changed\n")
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	third, e := c.prepareStage(context.Background(), opts, p)
	if e != nil {
		t.Fatal(e)
	}
	defer third.Close()
	if *third.Preview().Fingerprint == fingerprint {
		t.Fatal("changed installed tool retained trust")
	}
}

func fixtureStageSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whr-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(physical, "d.sock")
}
func TestLocalStageFixtureSocketIgnoresLongTMPDIR(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("long", 30))
	if err := os.Mkdir(long, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	t.Run("fresh_temp_root", func(t *testing.T) {
		_, _, ports := fixtureStagePreparation(t)
		endpoint, err := ports.Endpoint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		path := strings.TrimPrefix(endpoint, "unix://")
		if len(path) > 103 {
			t.Fatalf("fixture socket exceeds portable sockaddr limit: %d", len(path))
		}
		physical, err := filepath.EvalSymlinks(path)
		if err != nil || physical != path {
			t.Fatalf("socket path is not physical: %v", err)
		}
		st, err := os.Lstat(path)
		if err != nil || st.Mode()&os.ModeSocket == 0 {
			t.Fatalf("socket missing: %v", err)
		}
	})
	t.Run("health_notification", TestLocalStageReviewInterruptDuringHealthWaitFlow)
}

func TestLocalStageGitFixtureCanonicalSourceUnderAlias(t *testing.T) {
	parent := t.TempDir()
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "temporary-alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	t.Run("native_capture_selection", TestLocalStageGitStatusPredicateRejectsCorruptIndexWithoutExecutingConfig)
}

func TestLocalStageGitPreparationRetainsReplacedTemporaryRoot(t *testing.T) {
	parent := t.TempDir()
	original, replacement := filepath.Join(parent, "original"), filepath.Join(parent, "replacement")
	for _, dir := range []string{original, replacement} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(original, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	t.Run("fresh_temp_root", func(t *testing.T) {
		c, opts, ports := fixtureStagePreparation(t)
		var oldRoot, foreignRoot string
		ctx := captureControl(context.Background(), nil, func(phase, path string) {
			if phase != "preparation-root-created" {
				return
			}
			oldRoot = filepath.Join(original, filepath.Base(path))
			foreignRoot = filepath.Join(replacement, filepath.Base(path))
			if err := os.Mkdir(foreignRoot, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(foreignRoot, "sentinel"), []byte("retain foreign bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(replacement, alias); err != nil {
				t.Fatal(err)
			}
		}, nil)
		stage, err := c.prepareStage(ctx, opts, ports)
		if oldRoot == "" {
			t.Fatal("root identity checkpoint not reached")
		}
		if stage != nil || exitcode.FromError(err) != exitcode.Conflict || err.Error() != "local stage temporary preparation identity changed; retained for inspection" {
			t.Fatalf("replaced root refusal: %v %v", stage, err)
		}
		if _, err := os.Lstat(filepath.Join(foreignRoot, "phase")); !os.IsNotExist(err) {
			t.Fatalf("foreign root modified before identity refusal: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(foreignRoot, "sentinel"))
		if err != nil || string(data) != "retain foreign bytes" {
			t.Fatalf("foreign bytes changed: %v", err)
		}
		if _, err := os.Lstat(oldRoot); err != nil {
			t.Fatalf("original root lost: %v", err)
		}
	})
}

func TestLocalStageGitAliasedWorkspaceLabelsAndPromotionMount(t *testing.T) {
	parent := t.TempDir()
	physical := filepath.Join(parent, "physical")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	t.Run("lexical_labels", func(t *testing.T) {
		c, opts, ports, _ := fixtureStageLifecycle(t, false)
		stateParent, err := filepath.EvalSymlinks(filepath.Dir(ports.StateRoot))
		if err != nil {
			t.Fatal(err)
		}
		ports.StateRoot = filepath.Join(stateParent, filepath.Base(ports.StateRoot))
		opts.Smoke = true
		expected, err := filepath.EvalSymlinks(opts.Source)
		if err != nil || expected == opts.Source {
			t.Fatalf("fixture lost lexical alias: %q %q %v", opts.Source, expected, err)
		}
		stage, err := c.prepareStage(context.Background(), opts, ports)
		if err != nil {
			t.Fatalf("lexical CLI labels refused: %v", err)
		}
		defer stage.Close()
		if stage.primary.Labels["devcontainer.local_folder"] != opts.Source || stage.capture.Source != expected {
			t.Fatal("fixture did not retain lexical labels and physical capture")
		}
		result, err := stage.Apply(context.Background(), LifecycleConsent{TrustDigest: *stage.Preview().Fingerprint, Confirmed: true, Quiescence: true})
		if err != nil || result.Status != "staged_verified" {
			t.Fatalf("aliased workspace: %+v %v", result, err)
		}
		data, err := os.ReadFile(filepath.Join(stage.store.operationPath(stage.receipt.OperationID), "primary-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config stagePrimaryConfiguration
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		for _, raw := range config.Mounts {
			value, ok := raw.(string)
			if !ok {
				t.Fatalf("non-string persisted mount: %v", raw)
			}
			mount, err := stageNativeMount(value)
			if err != nil {
				t.Fatal(err)
			}
			if mount["type"] == "bind" && mount["target"] == stage.workspace {
				t.Fatalf("workspace bind duplicated into promotion mounts: %v", raw)
			}
		}
	})
}

func TestLocalStageFixtureStateRootUsesPhysicalAncestors(t *testing.T) {
	physical, err := os.MkdirTemp("", "whr-state-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(physical) })
	physical, err = filepath.EvalSymlinks(physical)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(physical, "alias")
	if err = os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	_, _, ports := fixtureStagePreparation(t)
	parent, err := filepath.EvalSymlinks(filepath.Dir(ports.StateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if ports.StateRoot != filepath.Join(parent, filepath.Base(ports.StateRoot)) {
		t.Fatalf("fixture gives forbidden aliased state path: %s", ports.StateRoot)
	}
	store, err := openStageStore(ports.StateRoot, "fixture-daemon", "unix:///tmp/docker.sock")
	if err != nil {
		t.Fatalf("fixture state root is not admitted: %v", err)
	}
	store.Close()
	t.Run("direct-read-only-disposal", TestThrowawayTreeCleanupHandlesReadOnlyCloneAndSymlink)
	t.Run("direct-state-journal", TestLocalStageDurableIntentAndMonotonicRecovery)
	t.Run("direct-state-modes", TestLocalStageStateModesAndNoFollow)
}

// Persistent test state obeys the production no-symlink path contract. Workspace
// paths/labels stay as given so source alias tests still exercise product code.
func fixtureStageStateParent(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
