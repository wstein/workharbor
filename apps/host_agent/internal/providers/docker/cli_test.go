// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"reflect"
	"strings"
	"testing"
)

func invocation(p Phase) Invocation {
	return Invocation{Phase: p, Scope: scopeFixture(), Workspace: "/private/storage/data", Config: "/private/config/.devcontainer/devcontainer.json", Override: "/private/operation/override.json", Driver: "/product/workharbor-agent-driver", UserData: "/private/operation/cli", ContainerID: strings.Repeat("a", 64)}
}
func option(args []string, key string) string {
	for i, v := range args {
		if v == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func TestOfficialResourceAndHookArgumentsStaySeparated(t *testing.T) {
	for _, phase := range []Phase{Preflight, Resources, Hooks} {
		i := invocation(phase)
		args, e := i.Arguments()
		if e != nil {
			t.Fatal(e)
		}
		if option(args, "--config") != i.Config || option(args, "--override-config") != i.Override || option(args, "--docker-path") != i.Driver || option(args, "--mount-workspace-git-root") != "false" || option(args, "--mount-git-worktree-common-dir") != "false" {
			t.Fatal("immutable path or mount policy missing")
		}
		labels := []string{}
		for n, v := range args {
			if v == "--id-label" && n+1 < len(args) {
				labels = append(labels, args[n+1])
			}
		}
		if len(labels) != len(i.Scope.Labels()) {
			t.Fatal("scope labels missing")
		}
		switch phase {
		case Preflight:
			if args[0] != "read-configuration" || option(args, "--include-merged-configuration") != "true" || option(args, "--container-id") != "" {
				t.Fatal("preflight not fresh read-only config")
			}
		case Resources:
			if args[0] != "up" || option(args, "--skip-post-create") != "true" || option(args, "--skip-post-attach") != "true" || option(args, "--omit-syntax-directive") != "true" || option(args, "--update-remote-user-uid-default") != "never" {
				t.Fatal("resource policy missing")
			}
		case Hooks:
			if args[0] != "run-user-commands" || option(args, "--container-id") != i.ContainerID || option(args, "--skip-post-create") != "" || option(args, "--skip-post-attach") != "true" {
				t.Fatal("hook phase wrong")
			}
		}
		if phase != Preflight && option(args, "--default-user-env-probe") != "none" {
			t.Fatal("host environment probe permitted")
		}
		again, e := i.Arguments()
		if e != nil || !reflect.DeepEqual(args, again) {
			t.Fatal("argv unstable")
		}
	}
}
func TestOfficialInvocationRejectsMissingIdentityPathsAndPhase(t *testing.T) {
	for _, mutate := range []func(*Invocation){func(i *Invocation) { i.Phase = "any" }, func(i *Invocation) { i.Driver = "docker" }, func(i *Invocation) { i.Scope.RuntimeID = "foreign" }, func(i *Invocation) { i.ContainerID = "short" }, func(i *Invocation) { i.Override = "/bad\x00path" }} {
		i := invocation(Hooks)
		mutate(&i)
		_, e := i.Arguments()
		errorCode(t, e, "invalid_configuration")
	}
}
