// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

type Action struct {
	Action        string `json:"action"`
	Kind          string `json:"kind"`
	Path          string `json:"path"`
	Applied       bool   `json:"applied"`
	Diff          string `json:"diff,omitempty"`
	DiffTruncated bool   `json:"diff_truncated,omitempty"`
}
type Preview struct {
	Created                    []string             `json:"created,omitempty"`
	Remaining                  []string             `json:"remaining,omitempty"`
	Directory                  string               `json:"directory"`
	DryRun                     bool                 `json:"dry_run"`
	Actions                    []Action             `json:"actions"`
	Validation                 *workspacecfg.Report `json:"validation"`
	RuntimeEnforcement         string               `json:"runtime_enforcement"`
	GitChecks                  string               `json:"git_checks"`
	BundleSelectionUnavailable bool                 `json:"bundle_selection_unavailable"`
	Settings                   map[string]Setting   `json:"settings,omitempty"`
	NextSteps                  []string             `json:"next_steps"`
}

// Preview returns a detached view; callers cannot alter the retained bytes that
// a later apply stage must use. Applied is always false in a proposal.
func (p *Proposal) Preview() Preview {
	var out Preview
	b, _ := json.Marshal(p.preview)
	_ = json.Unmarshal(b, &out)
	return out
}
func (p *Proposal) HumanPreview() string {
	var b strings.Builder
	for _, a := range p.preview.Actions {
		fmt.Fprintf(&b, "%s %s %s\n", a.Action, a.Kind, terminal.Escape(a.Path))
		if a.Diff != "" {
			b.WriteString(terminal.Escape(a.Diff))
			b.WriteByte('\n')
		}
		if a.DiffTruncated {
			b.WriteString("[preview truncated; retained content is complete]\n")
		}
	}
	for _, step := range p.preview.NextSteps {
		b.WriteString(terminal.Escape(step))
		b.WriteByte('\n')
	}
	return b.String()
}
func boundedDiff(s string) (string, bool) {
	r := []rune(s)
	if len(r) > 65536 {
		return string(r[:65536]), true
	}
	return s, false
}
func wholeDiff(name string, old, new []byte) string {
	var b strings.Builder
	oldLines := strings.SplitAfter(string(old), "\n")
	newLines := strings.SplitAfter(string(new), "\n")
	if len(old) > 0 && old[len(old)-1] == '\n' {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(new) > 0 && new[len(new)-1] == '\n' {
		newLines = newLines[:len(newLines)-1]
	}
	if len(old) == 0 {
		oldLines = nil
	}
	if len(new) == 0 {
		newLines = nil
	}
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n", name, name, len(oldLines), len(newLines))
	for _, group := range []struct {
		prefix string
		lines  []string
	}{{"-", oldLines}, {"+", newLines}} {
		for _, line := range group.lines {
			b.WriteString(group.prefix)
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return b.String()
}
