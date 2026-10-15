// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func policyFixture(phase Phase) DriverPolicy {
	script := sha256.Sum256([]byte("approved startup"))
	s := scopeFixture()
	id := strings.Repeat("a", 64)
	return DriverPolicy{Phase: phase, Scope: s, KnownIDs: map[string]bool{id: true}, ImageRefs: map[string]bool{imageSource()["image"].(string): true}, DataPath: "/private/storage/data", ContainerUser: "0:0", RemoteUser: "1000:1000", ContainerEnv: map[string]string{"APP": "literal"}, RemoteEnv: map[string]string{"REMOTE": "literal"}, StartupSHA256: hex.EncodeToString(script[:]), Build: &BuildPolicy{Context: "/private/config", Scratch: "/private/operation/tmp", Tag: "vsc-data-pinned", Target: "dev_containers_target_stage", Args: map[string]string{"APP_VERSION": "1", "BUILDKIT_INLINE_CACHE": "1", "_DEV_CONTAINERS_BASE_IMAGE": "app"}}}
}
func runFixture(p DriverPolicy) []string {
	args := []string{"run", "--sig-proxy=false", "-a", "STDOUT", "-a", "STDERR", "--mount", "type=bind,source=" + p.DataPath + ",target=/workspace"}
	for _, label := range p.Scope.LabelList() {
		args = append(args, "-l", label)
	}
	return append(args, "-e", "APP=literal", "-u", "0:0", "--name", p.Scope.Name(), "--entrypoint", "/bin/sh", imageSource()["image"].(string), "-c", "approved startup", "-")
}
func TestTypedDriverRunPreservesExactScopeMountUsersAndKnownImage(t *testing.T) {
	p := policyFixture(Resources)
	call, e := Classify(p, runFixture(p))
	if e != nil || call.Kind != "run" || call.Run == nil || call.Run.Name != p.Scope.Name() {
		t.Fatal(call, e)
	}
	for _, mutate := range []func([]string) []string{
		func(a []string) []string { return append(a[:1], append([]string{"--privileged"}, a[1:]...)...) },
		func(a []string) []string {
			for i, v := range a {
				if v == "--mount" {
					a[i+1] = "type=bind,source=/var/run/docker.sock,target=/workspace"
				}
			}
			return a
		},
		func(a []string) []string {
			for i, v := range a {
				if v == "--name" {
					a[i+1] = "foreign"
				}
			}
			return a
		},
		func(a []string) []string {
			for i, v := range a {
				if v == "-u" {
					a[i+1] = "root"
				}
			}
			return a
		},
		func(a []string) []string {
			for i, v := range a {
				if strings.HasPrefix(v, "workharbor.owner=") {
					a[i] = "workharbor.owner=foreign"
				}
			}
			return a
		},
		func(a []string) []string {
			for i, v := range a {
				if v == "approved startup" {
					a[i] = "hostile startup"
				}
			}
			return a
		},
	} {
		_, e := Classify(p, mutate(runFixture(p)))
		errorCode(t, e, "provider_refused")
	}
}
func TestTypedDriverHooksOnlyExecKnownTargetAndApprovedEnvironment(t *testing.T) {
	p := policyFixture(Hooks)
	id := strings.Repeat("a", 64)
	call, e := Classify(p, []string{"exec", "-i", "-u", "1000:1000", "-e", "REMOTE=literal", "-w", "/workspace", id, "/bin/sh", "-c", "./trusted-hook"})
	if e != nil || call.Exec == nil || call.Exec.ContainerID != id {
		t.Fatal(call, e)
	}
	for _, args := range [][]string{{"start", id}, {"pull", imageSource()["image"].(string)}, runFixture(p), {"exec", "--privileged", "-i", "-u", "1000:1000", id, "true"}, {"exec", "-i", "-u", "1000:1000", strings.Repeat("b", 64), "true"}, {"exec", "-i", "-u", "1000:1000", "-e", "HOST_SECRET=CANARY", id, "true"}} {
		_, e := Classify(p, args)
		errorCode(t, e, "provider_refused")
	}
}
func TestTypedDriverBuildClosedArgsContextAndGeneratedFile(t *testing.T) {
	p := policyFixture(Resources)
	args := []string{"buildx", "build", "--load", "--build-arg", "BUILDKIT_INLINE_CACHE=1", "-f", "/private/operation/tmp/owned/Dockerfile-with-features", "-t", p.Build.Tag, "--target", p.Build.Target, "--build-arg", "APP_VERSION=1", "--build-arg", "_DEV_CONTAINERS_BASE_IMAGE=app", p.Build.Context}
	call, e := Classify(p, args)
	if e != nil || call.Build == nil || call.Build.Context != p.Build.Context {
		t.Fatal(call, e)
	}
	for _, bad := range [][]string{append(append([]string{}, args[:2]...), append([]string{"--push"}, args[2:]...)...), {"buildx", "build", "--load", "-f", "/etc/Dockerfile", "-t", p.Build.Tag, p.Build.Context}, {"buildx", "build", "--load", "--build-arg", "BUILDKIT_SYNTAX=evil/frontend", p.Build.Context}} {
		_, e := Classify(p, bad)
		errorCode(t, e, "provider_refused")
	}
}
func TestTypedDriverRefusesGlobalUnknownAndLookupMutations(t *testing.T) {
	for _, phase := range []Phase{Preflight, Hooks, Resources} {
		p := policyFixture(phase)
		for _, args := range [][]string{{"--host", "unix:///foreign.sock", "version"}, {"--context", "foreign", "ps"}, {"system", "prune", "-af"}, {"volume", "rm", p.Scope.StorageID}, {"rm", "-f", strings.Repeat("a", 64)}, {"exec", "-i", "--env-file", "/private/secret", strings.Repeat("a", 64), "true"}, {"run", "--name", p.Scope.Name(), "ubuntu:latest"}} {
			_, e := Classify(p, args)
			errorCode(t, e, "provider_refused")
		}
	}
}
