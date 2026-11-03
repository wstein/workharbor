// SPDX-License-Identifier: EUPL-1.2

// Package wsname is the single definition of a workspace name and of the
// container and volume name derived from it (specs/006-whr-cli FR-070).
package wsname

import (
	"regexp"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// Prefix marks whr-owned containers and volumes.
const Prefix = "whr-"

var (
	workspaceName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	dockerName    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
)

// Valid reports whether name is a workspace name: 1-64 lowercase letters,
// digits or hyphens, without a leading or trailing hyphen.
func Valid(name string) bool { return workspaceName.MatchString(name) }

// Check returns the error for an invalid --name value.
func Check(name string) error {
	if !Valid(name) {
		return exitcode.Errorf(exitcode.PathBadArguments, "workspace name must be 1-64 lowercase letters, digits or hyphens, without a leading or trailing hyphen")
	}
	return nil
}

// ContainerName returns the container and volume name of a workspace: the
// explicit override when set, otherwise whr-<name>.
func ContainerName(name, override string) (string, error) {
	if err := Check(name); err != nil {
		return "", err
	}
	if override != "" {
		if !dockerName.MatchString(override) {
			return "", exitcode.Errorf(exitcode.PathBadArguments, "container name override is not a valid Docker name")
		}
		return override, nil
	}
	return Prefix + name, nil
}

// DerivedFromSelector returns whr-<selector> when selector is a valid
// workspace name, otherwise the empty string.
func DerivedFromSelector(selector string) string {
	if !Valid(selector) {
		return ""
	}
	return Prefix + selector
}
