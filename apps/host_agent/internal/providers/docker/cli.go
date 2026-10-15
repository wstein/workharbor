// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"path/filepath"
	"strings"
)

// Phase separates read-only capability preflight, resource setup and trusted hooks.
type Phase string

const (
	Preflight Phase = "preflight"
	Resources Phase = "resources"
	Hooks     Phase = "hooks"
)

type Invocation struct {
	Phase                                                                Phase
	Scope                                                                Scope
	Workspace, Config, Override, Driver, UserData, ContainerID, DataPath string
}

func (i Invocation) Arguments() ([]string, error) {
	if !i.Scope.valid() {
		return nil, fail("invalid_configuration")
	}
	for _, path := range []string{i.Workspace, i.Config, i.Override, i.Driver, i.UserData} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
			return nil, fail("invalid_configuration")
		}
	}
	var name string
	switch i.Phase {
	case Preflight:
		name = "read-configuration"
	case Resources:
		name = "up"
	case Hooks:
		name = "run-user-commands"
		if !hexID.MatchString(i.ContainerID) {
			return nil, fail("invalid_configuration")
		}
	default:
		return nil, fail("invalid_configuration")
	}
	args := []string{name, "--workspace-folder", i.Workspace, "--config", i.Config, "--override-config", i.Override, "--docker-path", i.Driver, "--mount-workspace-git-root", "false", "--mount-git-worktree-common-dir", "false", "--log-format", "json", "--log-level", "info"}
	for _, label := range i.Scope.LabelList() {
		args = append(args, "--id-label", label)
	}
	switch i.Phase {
	case Preflight:
		args = append(args, "--include-merged-configuration", "true")
	case Resources:
		args = append(args, "--user-data-folder", i.UserData, "--skip-post-create", "true", "--skip-post-attach", "true", "--default-user-env-probe", "none", "--update-remote-user-uid-default", "never", "--omit-syntax-directive", "true", "--gpu-availability", "none", "--include-configuration", "true", "--include-merged-configuration", "true")
	case Hooks:
		args = append(args, "--user-data-folder", i.UserData, "--container-id", i.ContainerID, "--skip-post-attach", "true", "--default-user-env-probe", "none")
	}
	return args, nil
}
