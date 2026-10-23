// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"strings"
)

// Filter applies the FR-066 selectors after the mandatory workspace-label guard.
// A label value is split only at the first '='; presence and empty value differ.
func Filter(all []Workspace, role, label string) ([]Workspace, error) {
	if role != "" && role != "primary" && role != "staging" && role != "temp" {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, "--role must be primary, staging or temp")
	}
	key, value, hasValue := strings.Cut(label, "=")
	if label != "" && key == "" {
		return nil, exitcode.Errorf(exitcode.PathBadArguments, "--label requires a key or key=value")
	}
	out := []Workspace{}
	for _, w := range all {
		if _, ok := workspaceRole(w.Labels); !ok {
			continue
		}
		if role != "" && w.Role != role {
			continue
		}
		if label != "" {
			v, exists := w.Labels[key]
			if !exists || hasValue && v != value {
				continue
			}
		}
		out = append(out, w)
	}
	return out, nil
}
