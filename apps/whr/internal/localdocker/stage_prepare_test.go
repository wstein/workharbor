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
	p := stagePreparationPorts{Endpoint: func(context.Context) (string, error) { return "unix://" + socket, nil }, Tools: map[string]string{"docker": docker, "node": node, "devcontainer": cli, "git": git, "sh": sh, "bash": bash}, StateRoot: filepath.Join(t.TempDir(), "absent-state"), MountAdmission: func(context.Context, stageDaemonInfo, inspection, string) error { return nil }}
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
