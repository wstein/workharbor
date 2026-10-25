// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustStageToolDigest(t *testing.T, path string) string {
	t.Helper()
	v, e := stageFileDigest(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestLocalStageDriverNeverForwardsPrimaryOrVolumeMutation(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	log := filepath.Join(t.TempDir(), "native.log")
	image := "sha256:" + strings.Repeat("a", 64)
	native := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + stageShellLiteral(log) + "\nif [ \"$3\" = image ]; then printf '%s\\n' " + stageShellLiteral(image) + "; fi\n"
	if e := os.WriteFile(native, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	runner, e := newStageRunner(context.Background(), root, "unix:///tmp/fixed.sock", map[string]string{"docker": native, "sh": "/bin/sh"})
	if e != nil {
		t.Fatal(e)
	}
	primary := strings.Repeat("b", 64)
	home := "whr-stage-home-" + strings.Repeat("c", 32)
	volume := "bundledock-toolbox-" + strings.Repeat("d", 64)
	ref := "quay.io/bundledock/toolbox@sha256:" + strings.Repeat("d", 64)
	runner.Policy = &stageDriverPolicy{PrimaryID: primary, PrimaryName: "primary", PrimaryHome: "anchorage-home-primary", OperationID: strings.Repeat("e", 32), WorkspaceSource: "/private/clone", WorkspaceDestination: "/workspaces/repo", Home: home, HomeDestination: "/home/vscode", InitializerVolumes: map[string]string{volume: ref}}
	driver, e := runner.DockerDriver("stage")
	if e != nil {
		t.Fatal(e)
	}
	_ = driver
	mount := "type=bind,source=/private/clone,target=/workspaces/repo"
	homeMount := "type=volume,source=" + home + ",target=/home/vscode"
	cases := []struct {
		args   []string
		reason string
	}{
		{[]string{"stop", primary}, "protected_resource_mutation"}, {[]string{"rm", "-f", primary}, "protected_resource_mutation"}, {[]string{"container", "rm", primary}, "protected_resource_mutation"}, {[]string{"volume", "rm", home}, "volume_mutation"}, {[]string{"network", "create", "foreign"}, "protected_resource_mutation"}, {[]string{"exec", primary, "true"}, "old_primary_target"},
		{[]string{"run", "--name", "whr-anchorage-staging", "--mount", mount, "--mount", homeMount, "--mount", "type=volume,source=anchorage-home-primary,target=/mnt/primary", "candidate-image"}, "undeclared_native_mount"},
		{[]string{"run", "--name", "whr-anchorage-staging", "--mount", mount, "--mount", homeMount, "-e", "PATH", "candidate-image"}, "unbound_env_copy"},
		{[]string{"run", "--name", "whr-anchorage-staging", "--mount", mount, "--mount", homeMount, "--env=PATH", "candidate-image"}, "unbound_env_copy"},
		{[]string{"run", "--name", "whr-anchorage-staging", "--mount", mount, "--mount", homeMount, "-ePATH", "candidate-image"}, "unbound_env_copy"},
	}
	for _, c := range cases {
		t.Run(c.reason+strings.Join(c.args[:1], ""), func(t *testing.T) {
			if e = os.Remove(log); e != nil && !os.IsNotExist(e) {
				t.Fatal(e)
			}
			_, err := runner.Run(context.Background(), "resources", "docker_driver", c.args, nil, time.Second)
			if exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("wrong refusal: %v", err)
			}
			if _, e = os.Stat(log); !os.IsNotExist(e) {
				t.Fatalf("native Docker was called on unsafe vector %v", c.args)
			}
		})
	}
	for _, args := range [][]string{{"pull", ref}, {"volume", "create", volume}, {"run", "--rm", "--network", "none", "--mount", "type=volume,src=" + volume + ",dst=/bundle,volume-nocopy", image}, {"run", "--sig-proxy=false", "-a", "STDOUT", "-a", "STDERR", "--mount", mount, "--mount", homeMount, "--name", "whr-anchorage-staging", "--entrypoint", "/bin/sh", "candidate-image", "-c", "echo Container started"}} {
		if _, e = runner.Run(context.Background(), "resources", "docker_driver", args, nil, time.Second); e != nil {
			t.Fatalf("admitted official/initializer vector failed %v: %v", args, e)
		}
	}
}

func TestLocalStageDriverRejectsDeclaredPrimaryAtAnyDestination(t *testing.T) {
	for _, mount := range []string{"type=volume,source=anchorage-home-primary,target=/mnt/data", "type=volume,src=anchorage-home-primary,dst=/mnt/data,readonly", "source=anchorage-home-primary,target=/home/vscode", "type=volume,\"source=anchorage-home-primary\",target=/mnt/data"} {
		t.Run(mount, func(t *testing.T) {
			root := t.TempDir()
			if e := os.Chmod(root, 0700); e != nil {
				t.Fatal(e)
			}
			native := filepath.Join(t.TempDir(), "docker")
			if e := os.WriteFile(native, []byte("#!/bin/sh\nexit 99\n"), 0700); e != nil {
				t.Fatal(e)
			}
			r, e := newStageRunner(context.Background(), root, "unix:///tmp/fixed.sock", map[string]string{"docker": native, "sh": "/bin/sh"})
			if e != nil {
				t.Fatal(e)
			}
			r.Policy = &stageDriverPolicy{PrimaryID: strings.Repeat("a", 64), PrimaryName: "primary", PrimaryHome: "anchorage-home-primary", OperationID: strings.Repeat("b", 32), WorkspaceSource: "/private/clone", WorkspaceDestination: "/workspaces/repo", Home: "whr-stage-home-" + strings.Repeat("c", 32), HomeDestination: "/home/vscode", AllowedMounts: []string{mount}}
			if _, e = r.DockerDriver("stage"); exitcode.FromError(e) != exitcode.Conflict || !strings.Contains(e.Error(), "candidate primary home exposure") {
				t.Fatalf("declared exposure admitted: %v", e)
			}
		})
	}
}
func TestLocalStageDriverPinsEndpoint(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	native := filepath.Join(t.TempDir(), "docker")
	if e := os.WriteFile(native, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	runner, e := newStageRunner(context.Background(), root, "unix:///tmp/fixed.sock", map[string]string{"docker": native, "sh": "/bin/sh"})
	if e != nil {
		t.Fatal(e)
	}
	runner.Policy = &stageDriverPolicy{PrimaryID: strings.Repeat("b", 64), PrimaryName: "primary", PrimaryHome: "primary-home", OperationID: strings.Repeat("c", 32), WorkspaceSource: "/original", WorkspaceDestination: "/workspaces/repo", Home: "candidate-home", HomeDestination: "/home/vscode"}
	image := "sha256:" + strings.Repeat("a", 64)
	driver, e := runner.DockerDriver("stage")
	if e != nil {
		t.Fatal(e)
	}
	runner.Tools["docker_driver"] = stageToolIdentity{Path: driver, Digest: mustStageToolDigest(t, driver)}
	out, e := runner.Run(context.Background(), "native_run", "docker_driver", []string{"run", "--name", "whr-anchorage-staging", "--mount", "type=bind,source=/original,target=/workspaces/repo", "--mount", "type=volume,source=candidate-home,target=/home/vscode", image, "/bin/sh"}, nil, time.Second)
	if e != nil || !strings.HasPrefix(string(out.Stdout), "--host\nunix:///tmp/fixed.sock\nrun\n") {
		t.Fatalf("driver output %q %v", out.Stdout, e)
	}

	if _, err := runner.DockerDriver("promote"); exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), "Docker driver phase unsupported") {
		t.Fatalf("promote mode carried in stage: %v", err)
	}
	base := []string{"run", "--name", "whr-anchorage-staging", "--mount", "type=bind,source=/original,target=/workspaces/repo", "--mount", "type=volume,source=candidate-home,target=/home/vscode"}
	cases := []struct {
		args   []string
		reason string
	}{
		{[]string{"pull", image}, "undeclared_pull_reference"}, {[]string{"--host", "unix:///tmp/foreign.sock", "info"}, "routing_override"}, {append([]string{"run", "--pull=always"}, append(base[1:], image)...), "native_pull_override"},
	}
	for _, c := range cases {
		if _, err := runner.Run(context.Background(), "native_run", "docker_driver", c.args, nil, time.Second); exitcode.FromError(err) != exitcode.Conflict || !strings.Contains(err.Error(), c.reason) {
			t.Fatalf("wrong driver refusal for %v: %v", c.args, err)
		}
	}

}
