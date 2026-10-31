// SPDX-License-Identifier: EUPL-1.2

package releaseworkflow

import (
	"regexp"
	"strings"
	"testing"
)

// colima-acceptance.yml is an informative, owner-started measurement run. These tests pin the
// rules that keep it that way: it cannot start by itself, it cannot write, it uses only pinned
// actions, and no job can turn a failed measurement into a red check. Text rules, like the release
// workflow tests above; each violation names the rule, and the mutation tests prove the rules bite.

const colimaWorkflow = ".github/workflows/colima-acceptance.yml"

var (
	topKey     = regexp.MustCompile(`^[A-Za-z_-]+:`)
	childKey   = regexp.MustCompile(`^  ([A-Za-z_-]+):`)
	usesLine   = regexp.MustCompile(`^\s*(?:- )?uses:\s*(\S.*)$`)
	permsLine  = regexp.MustCompile(`^(\s*)permissions:\s*(.*)$`)
	leadingWS  = regexp.MustCompile(`^\s*`)
	colimaJobs = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*):\n`)
)

// colimaViolations returns every rule the workflow text breaks.
func colimaViolations(raw string) []string {
	var out []string
	lines := strings.Split(raw, "\n")

	// Triggers: the top-level on: block holds exactly workflow_dispatch.
	var triggers []string
	inOn := false
	for _, l := range lines {
		if topKey.MatchString(l) {
			inOn = strings.HasPrefix(l, "on:")
			continue
		}
		if m := childKey.FindStringSubmatch(l); inOn && m != nil {
			triggers = append(triggers, m[1])
		}
	}
	if len(triggers) != 1 || triggers[0] != "workflow_dispatch" {
		out = append(out, sprintf("triggers %v, want exactly [workflow_dispatch]", triggers))
	}

	// Permissions: every permissions block grants contents: read and nothing else.
	blocks := 0
	for i, l := range lines {
		m := permsLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		blocks++
		if m[2] != "" {
			out = append(out, sprintf("permissions must be a block, got %q", strings.TrimSpace(l)))
			continue
		}
		indent := len(m[1])
		var grants []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if len(leadingWS.FindString(next)) <= indent {
				break
			}
			grants = append(grants, strings.TrimSpace(next))
		}
		if len(grants) != 1 || grants[0] != "contents: read" {
			out = append(out, sprintf("permissions grant %v, want exactly [contents: read]", grants))
		}
	}
	if blocks == 0 || !strings.Contains(raw, "\npermissions:\n  contents: read\n") {
		out = append(out, "no top-level permissions: contents: read")
	}
	for _, bad := range []string{"write-all", "secrets.", "id-token", "pull_request_target"} {
		if strings.Contains(raw, bad) {
			out = append(out, sprintf("must not contain %q", bad))
		}
	}

	// Actions: a full commit SHA with a version comment.
	n := 0
	for _, l := range lines {
		if m := usesLine.FindStringSubmatch(l); m != nil {
			n++
			if !pinned.MatchString(strings.TrimSpace(m[1])) {
				out = append(out, sprintf("action not pinned to a full SHA with a version comment: %s", strings.TrimSpace(m[1])))
			}
		}
	}
	if n == 0 {
		out = append(out, "no uses: lines found")
	}

	// Jobs: continue-on-error, a bounded timeout, and a non-fail-fast matrix on every job.
	i := strings.Index(raw, "\njobs:\n")
	if i < 0 {
		return append(out, "no jobs:")
	}
	body := raw[i+len("\njobs:\n"):]
	locs := colimaJobs.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		out = append(out, "no jobs found")
	}
	for k, loc := range locs {
		end := len(body)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		name, job := body[loc[2]:loc[3]], body[loc[0]:end]
		if !strings.Contains(job, "\n    continue-on-error: true\n") {
			out = append(out, sprintf("job %s lacks continue-on-error: true", name))
		}
		if !strings.Contains(job, "\n    timeout-minutes: ") {
			out = append(out, sprintf("job %s lacks timeout-minutes", name))
		}
		if !strings.Contains(job, "\n      fail-fast: false\n") || !strings.Contains(job, "\n        include:\n") {
			out = append(out, sprintf("job %s lacks fail-fast: false or a matrix include", name))
		}
	}
	return out
}

func TestColimaWorkflowKeepsItsRules(t *testing.T) {
	raw := readRepo(t, colimaWorkflow)
	for _, v := range colimaViolations(raw) {
		t.Error(v)
	}
	if !strings.HasPrefix(raw, "# SPDX-License-Identifier: EUPL-1.2\n") {
		t.Error("missing SPDX header")
	}
}

func TestColimaWorkflowMatrixRows(t *testing.T) {
	raw := readRepo(t, colimaWorkflow)
	for _, want := range []string{
		"runner: macos-15-intel\n            vm_type: qemu\n            mount_type: sshfs\n",
		"runner: macos-15-intel\n            vm_type: qemu\n            mount_type: 9p\n",
		"runner: macos-15\n            vm_type: vz\n            mount_type: virtiofs\n",
		"colima start --runtime docker --vm-type \"$VM_TYPE\" --mount-type \"$MOUNT_TYPE\"",
		"brew install colima docker\n",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	// Matrix values reach scripts through env, never by interpolation inside a run: block.
	inRun := false
	for _, l := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(trimmed, "run: |"):
			inRun = true
		case inRun && len(leadingWS.FindString(l)) <= 8 && trimmed != "":
			inRun = false
		}
		if inRun && strings.Contains(l, "${{") {
			t.Errorf("expression inside a run: script: %s", trimmed)
		}
	}
}

func TestColimaWorkflowRulesBite(t *testing.T) {
	raw := readRepo(t, colimaWorkflow)
	sha := "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1"
	cases := []struct{ name, from, to string }{
		{"pull_request trigger", "on:\n  workflow_dispatch:\n", "on:\n  workflow_dispatch:\n  pull_request:\n"},
		{"schedule trigger", "on:\n  workflow_dispatch:\n", "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 3 * * *'\n"},
		{"push trigger instead", "on:\n  workflow_dispatch:\n", "on:\n  push:\n"},
		{"wider permissions", "permissions:\n  contents: read\n", "permissions:\n  contents: read\n  pull-requests: write\n"},
		{"write permission", "permissions:\n  contents: read\n", "permissions:\n  contents: write\n"},
		{"job-level permissions", "    continue-on-error: true\n", "    continue-on-error: true\n    permissions:\n      contents: read\n      id-token: write\n"},
		{"tag instead of SHA", sha, "actions/checkout@v7 # v7.0.1"},
		{"SHA without version comment", sha, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"},
		{"missing continue-on-error", "    continue-on-error: true\n", ""},
		{"continue-on-error false", "    continue-on-error: true\n", "    continue-on-error: false\n"},
		{"fail-fast on", "      fail-fast: false\n", "      fail-fast: true\n"},
	}
	for _, c := range cases {
		if strings.Count(raw, c.from) < 1 {
			t.Fatalf("%s: mutation anchor %q not found", c.name, c.from)
		}
		if len(colimaViolations(strings.Replace(raw, c.from, c.to, 1))) == 0 {
			t.Errorf("%s: no violation reported", c.name)
		}
	}
}
