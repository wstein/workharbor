// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/devcontainer"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
)

// readConfiguration runs only the pinned official read command, behind the
// snapshot-only executable adapter. Public admission is a separate native port.
func readConfiguration(ctx context.Context, tools devcontainer.Tools, input Invocation, authorize func(context.Context) error) (map[string]any, error) {
	if authorize == nil {
		return nil, fail("forbidden")
	}
	if e := authorize(ctx); e != nil {
		return nil, e
	}
	if input.Phase != Preflight {
		return nil, fail("invalid_configuration")
	}
	args, e := input.Arguments()
	if e != nil {
		return nil, e
	}
	if tools.Verify(ctx, true) != nil {
		return nil, fail("tool_unavailable")
	}
	parser := config.JSONC{NodePath: tools.NodePath, CompanionDir: tools.Root}
	raw, e := os.ReadFile(input.Config)
	if e != nil {
		return nil, fail("invalid_configuration")
	}
	value, e := parser.Parse(ctx, raw, false)
	if e != nil {
		return nil, e
	}
	source, ok := value.(map[string]any)
	if !ok {
		return nil, fail("invalid_configuration")
	}
	expected, e := FullOverride(source, input.Scope, input.DataPath)
	if e != nil {
		return nil, e
	}
	container, cok := source["containerUser"].(string)
	remote, rok := source["remoteUser"].(string)
	_, _, cv := parseUser(container)
	_, _, rv := parseUser(remote)
	if !cok || !rok || !cv || !rv || (container != remote && container != "0:0") {
		return nil, fail("unsupported_configuration")
	}
	raw, e = os.ReadFile(input.Override)
	if e != nil {
		return nil, fail("invalid_configuration")
	}
	override, e := parser.Parse(ctx, raw, true)
	if e != nil {
		return nil, e
	}
	if !reflect.DeepEqual(expected, override) {
		return nil, fail("unsupported_configuration")
	}
	info, e := os.Lstat(input.UserData)
	physical, pe := filepath.EvalSymlinks(input.UserData)
	if e != nil || pe != nil || physical != input.UserData || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, fail("invalid_configuration")
	}
	cache, e := os.MkdirTemp(input.UserData, "read-")
	if e != nil {
		return nil, fail("staging_failed")
	}
	defer os.RemoveAll(cache)
	for _, dir := range []string{"home", "docker", "tmp"} {
		if os.Mkdir(filepath.Join(cache, dir), 0700) != nil {
			return nil, fail("staging_failed")
		}
	}
	if e := authorize(ctx); e != nil {
		return nil, e
	}
	command := exec.CommandContext(ctx, tools.NodePath, append([]string{filepath.Join(tools.Root, "node_modules/@devcontainers/cli/dist/spec-node/devContainersSpecCLI.js")}, args...)...)
	command.Dir = input.Workspace
	command.Env = append(tools.Environment(), "HOME="+filepath.Join(cache, "home"), "DOCKER_CONFIG="+filepath.Join(cache, "docker"), "TMPDIR="+filepath.Join(cache, "tmp"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	command.Stderr = io.Discard
	raw, e = command.Output()
	if e != nil {
		return nil, fail("configuration_unavailable")
	}
	value, e = parser.Parse(ctx, raw, true)
	if e != nil {
		return nil, e
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fail("invalid_configuration")
	}
	return result, nil
}
