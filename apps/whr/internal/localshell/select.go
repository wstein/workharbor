// SPDX-License-Identifier: EUPL-1.2

// Package localshell starts one fresh login shell through the owner's local
// Docker capability (ADR 0015); it never owns container/session lifecycle.
package localshell

import (
	"fmt"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/localdocker"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"regexp"
	"strings"
)

var idPrefix = regexp.MustCompile(`^[a-fA-F0-9]{12,64}$`)

// Select matches all discovered states before checking liveness. Exact names
// win over ID prefixes; neither aliases nor name-prefix guesses are accepted.
func Select(all []localdocker.Workspace, selector string) (localdocker.Workspace, error) {
	workspaces, _ := localdocker.Filter(all, "", "")
	var matches []localdocker.Workspace
	for _, w := range workspaces {
		if w.Name == selector {
			matches = append(matches, w)
		}
	}
	if len(matches) == 0 && idPrefix.MatchString(selector) {
		for _, w := range workspaces {
			if strings.HasPrefix(w.ID, strings.ToLower(selector)) {
				matches = append(matches, w)
			}
		}
	}
	if len(matches) == 0 {
		return localdocker.Workspace{}, exitcode.Errorf(exitcode.PathTargetAbsent, "no labelled local workspace matches %q", terminal.Escape(selector))
	}
	if len(matches) > 1 {
		var candidates []string
		for _, w := range matches {
			id := w.ID
			if len(id) > 12 {
				id = id[:12]
			}
			candidates = append(candidates, fmt.Sprintf("%s (%s)", terminal.Escape(w.Name), terminal.Escape(id)))
		}
		return localdocker.Workspace{}, exitcode.Errorf(exitcode.PathBadArguments, "local workspace selector %q is ambiguous: %s", terminal.Escape(selector), strings.Join(candidates, ", "))
	}
	w := matches[0]
	if w.State != "running" {
		return localdocker.Workspace{}, exitcode.Errorf(exitcode.PathWrongState, "local workspace %s is %s; a running container is required", terminal.Escape(w.Name), terminal.Escape(w.State))
	}
	return w, nil
}
