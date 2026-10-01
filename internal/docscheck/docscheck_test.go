// Package docscheck has tests that fail when the design document and the code
// say different things (issue #44): the exit codes of §9.2, the state machines
// of §4.1 and the policy defaults of §6. It holds no code of its own.
package docscheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/policy"
)

// designDir holds the design, one page per topic (issue #43).
const designDir = "../../docs/content/docs/design"

// readDesign returns every page of the design, joined in file-name order. A
// check reads between markers of one section, so the order does not matter.
func readDesign(t *testing.T) string {
	t.Helper()
	pages, err := filepath.Glob(filepath.Join(designDir, "*.md"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no design pages in %s (%v)", designDir, err)
	}
	var doc strings.Builder
	for _, p := range pages {
		b, err := os.ReadFile(p) //nolint:gosec // a page of the design
		if err != nil {
			t.Fatal(err)
		}
		doc.Write(b)
		doc.WriteString("\n")
	}
	return doc.String()
}

// between returns the text from the first start marker to the next end marker.
func between(doc, start, end string) (string, bool) {
	i := strings.Index(doc, start)
	if i < 0 {
		return "", false
	}
	rest := doc[i:]
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		return rest, true
	}
	return rest[:len(start)+j], true
}

// ---- §9.2 exit codes ----

// exitCodes is what the code says, keyed by the word the design uses.
var exitCodes = map[string]int{
	"ok": exitcode.OK, "error": exitcode.Error, "usage": exitcode.Usage, "not found": exitcode.NotFound,
	"auth": exitcode.Auth, "conflict/wrong state": exitcode.Conflict, "needs human input": exitcode.NeedsHuman,
	"timeout": exitcode.Timeout, "task failed": exitcode.TaskFailed,
}

var exitEntry = regexp.MustCompile(`(\d+) ([a-z][a-z /]*[a-z])(?: \([^)]*\))?`)

func exitCodeProblems(doc string) []string {
	line, ok := between(doc, "- **Exit codes:**", "\n")
	if !ok {
		return []string{"§9.2 has no exit-code line"}
	}
	var problems []string
	seen := map[string]bool{}
	for _, m := range exitEntry.FindAllStringSubmatch(strings.TrimPrefix(line, "- **Exit codes:**"), -1) {
		code, _ := strconv.Atoi(m[1])
		name := m[2]
		want, known := exitCodes[name]
		switch {
		case !known:
			problems = append(problems, fmt.Sprintf("§9.2 lists exit code %d %q, which the code does not have", code, name))
		case want != code:
			problems = append(problems, fmt.Sprintf("§9.2 says %q is %d, the code says %d", name, code, want))
		}
		seen[name] = true
	}
	for name, code := range exitCodes {
		if !seen[name] {
			problems = append(problems, fmt.Sprintf("the code has exit code %d (%s), §9.2 does not list it", code, name))
		}
	}
	sort.Strings(problems)
	return problems
}

// ---- §4.1 state machines ----

var cell = regexp.MustCompile("`([a-z_]+)`")

// stateTable reads the markdown table under a heading line into from -> to
// sets. A row whose second cell is "none (terminal)" lists terminal states.
func stateTable(doc, heading, next string) (moves map[string]map[string]bool, terminal map[string]bool, err error) {
	block, ok := between(doc, heading, next)
	if !ok {
		return nil, nil, fmt.Errorf("§4.1 has no %q", heading)
	}
	moves, terminal = map[string]map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "| ---") || strings.HasPrefix(line, "| From") {
			continue
		}
		cols := strings.Split(strings.Trim(line, "|"), "|")
		if len(cols) != 2 {
			continue
		}
		from, to := cell.FindAllStringSubmatch(cols[0], -1), strings.TrimSpace(cols[1])
		if strings.HasPrefix(to, "none") {
			for _, f := range from {
				terminal[f[1]] = true
			}
			continue
		}
		for _, f := range from {
			moves[f[1]] = map[string]bool{}
			// Annotations such as (rework) sit in parentheses and name no state.
			plain := regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(to, "")
			for _, t := range cell.FindAllStringSubmatch(plain, -1) {
				moves[f[1]][t[1]] = true
			}
		}
	}
	return moves, terminal, nil
}

type machine struct {
	name     string
	heading  string
	next     string
	states   []string
	can      func(from, to string) bool
	terminal func(s string) bool
}

func machines() []machine {
	task := []domain.TaskState{domain.TaskQueued, domain.TaskRunning, domain.TaskAwaitingGuidance, domain.TaskReadyForReview, domain.TaskCompleted, domain.TaskCancelled, domain.TaskFailed}
	run := []domain.RunState{domain.RunStarting, domain.RunRunning, domain.RunPaused, domain.RunInterrupted, domain.RunStopped, domain.RunFailed}
	env := []domain.EnvState{domain.EnvProvisioning, domain.EnvRunning, domain.EnvStopped, domain.EnvDeleted}
	names := func(n int, f func(i int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = f(i)
		}
		return out
	}
	return []machine{
		{
			name: "task", heading: "- **Task** (D13):", next: "- **Run**",
			states:   names(len(task), func(i int) string { return string(task[i]) }),
			can:      func(f, t string) bool { return domain.TaskState(f).CanTransition(domain.TaskState(t)) },
			terminal: func(s string) bool { return domain.TaskState(s).Terminal() },
		},
		{
			name: "run", heading: "- **Run** (issue #15):", next: "- **Environment**",
			states:   names(len(run), func(i int) string { return string(run[i]) }),
			can:      func(f, t string) bool { return domain.RunState(f).CanTransition(domain.RunState(t)) },
			terminal: func(s string) bool { return domain.RunState(s).Terminal() },
		},
		{
			name: "environment", heading: "- **Environment** (issue #15):", next: "- **Coupling rules**",
			states:   names(len(env), func(i int) string { return string(env[i]) }),
			can:      func(f, t string) bool { return domain.EnvState(f).CanTransition(domain.EnvState(t)) },
			terminal: func(s string) bool { return domain.EnvState(s).Terminal() },
		},
	}
}

func stateProblems(doc string) []string {
	var problems []string
	for _, m := range machines() {
		moves, terminal, err := stateTable(doc, m.heading, m.next)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		known := map[string]bool{}
		for _, s := range m.states {
			known[s] = true
		}
		for from, tos := range moves {
			if !known[from] {
				problems = append(problems, fmt.Sprintf("§4.1 %s table has a state %q that the code does not have", m.name, from))
			}
			for to := range tos {
				if !known[to] {
					problems = append(problems, fmt.Sprintf("§4.1 %s table moves to %q, which the code does not have", m.name, to))
				}
			}
		}
		for _, from := range m.states {
			for _, to := range m.states {
				doc, code := moves[from][to], m.can(from, to)
				switch {
				case doc && !code:
					problems = append(problems, fmt.Sprintf("§4.1 allows %s %s -> %s, the code does not", m.name, from, to))
				case !doc && code:
					problems = append(problems, fmt.Sprintf("the code allows %s %s -> %s, §4.1 does not list it", m.name, from, to))
				}
			}
			if isTerminal := terminal[from]; isTerminal != m.terminal(from) {
				problems = append(problems, fmt.Sprintf("%s state %s: §4.1 terminal=%v, the code says %v", m.name, from, isTerminal, m.terminal(from)))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// ---- §4.1 state diagrams ----

var arrow = regexp.MustCompile(`^\s*(\[\*\]|[a-z_]+)\s*-->\s*(\[\*\]|[a-z_]+)\s*(?::.*)?$`)

// stateDiagram reads the mermaid stateDiagram in a machine's block: its moves
// and its terminal states (those with an arrow to [*]). The start arrow from
// [*] names no transition and is skipped.
func stateDiagram(doc, heading, next string) (moves map[string]map[string]bool, terminal map[string]bool, err error) {
	block, ok := between(doc, heading, next)
	if !ok {
		return nil, nil, fmt.Errorf("§4.1 has no %q", heading)
	}
	i := strings.Index(block, "stateDiagram-v2")
	if i < 0 {
		return nil, nil, fmt.Errorf("§4.1 %q has no state diagram", heading)
	}
	body := block[i:]
	if j := strings.Index(body, "```"); j >= 0 {
		body = body[:j]
	}
	moves, terminal = map[string]map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(body, "\n")[1:] {
		m := arrow.FindStringSubmatch(line)
		switch {
		case m == nil:
			if strings.TrimSpace(line) != "" {
				return nil, nil, fmt.Errorf("§4.1 %q diagram: cannot read %q", heading, strings.TrimSpace(line))
			}
		case m[1] == "[*]":
		case m[2] == "[*]":
			terminal[m[1]] = true
		default:
			if moves[m[1]] == nil {
				moves[m[1]] = map[string]bool{}
			}
			moves[m[1]][m[2]] = true
		}
	}
	return moves, terminal, nil
}

func diagramProblems(doc string) []string {
	var problems []string
	for _, m := range machines() {
		moves, terminal, err := stateDiagram(doc, m.heading, m.next)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, from := range m.states {
			for _, to := range m.states {
				drawn, code := moves[from][to], m.can(from, to)
				switch {
				case drawn && !code:
					problems = append(problems, fmt.Sprintf("the %s diagram draws %s -> %s, the code does not allow it", m.name, from, to))
				case !drawn && code:
					problems = append(problems, fmt.Sprintf("the code allows %s %s -> %s, the diagram does not draw it", m.name, from, to))
				}
			}
			if terminal[from] != m.terminal(from) {
				problems = append(problems, fmt.Sprintf("%s state %s: the diagram terminal=%v, the code says %v", m.name, from, terminal[from], m.terminal(from)))
			}
		}
		known := map[string]bool{}
		for _, st := range m.states {
			known[st] = true
		}
		for from, tos := range moves {
			for to := range tos {
				if !known[from] || !known[to] {
					problems = append(problems, fmt.Sprintf("the %s diagram draws %s -> %s, a state the code does not have", m.name, from, to))
				}
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// ---- §6 policy defaults ----

// policyRows maps a row of the §6 table to the actions it covers.
var policyRows = []struct {
	prefix  string
	actions []policy.Action
}{
	{"| Commit in the topic's own checkout", []policy.Action{policy.Commit}},
	{"| Push an `agent/*` branch", []policy.Action{policy.PushAgentBranch}},
	{"| Open/update PR, comment on issue", []policy.Action{policy.OpenPR, policy.CommentIssue}},
	{"| Merge, tag, release, deploy", []policy.Action{policy.Merge, policy.Tag, policy.Release, policy.Deploy}},
}

// docMode reads the default of a row: "auto", "forbid", or "ask". The push row
// says "after cleanup": the supervisor pushes once the human approves, which is
// ask (design §4.5, §6).
func docMode(c string) (policy.Mode, bool) {
	c = strings.ToLower(strings.ReplaceAll(c, "**", ""))
	switch {
	case strings.Contains(c, "forbid"):
		return policy.Forbid, true
	case strings.HasPrefix(strings.TrimSpace(c), "auto"):
		return policy.Auto, true
	case strings.Contains(c, "after cleanup") && strings.Contains(c, "approve"):
		return policy.Ask, true
	case strings.HasPrefix(strings.TrimSpace(c), "ask"):
		return policy.Ask, true
	}
	return "", false
}

func policyProblems(doc string) []string {
	block, ok := between(doc, "## 6. Policy and autonomy", "## 7.")
	if !ok {
		return []string{"the design has no §6"}
	}
	var problems []string
	table := policy.Default()
	listed := map[policy.Action]bool{}
	for _, row := range policyRows {
		var line string
		for _, l := range strings.Split(block, "\n") {
			if strings.HasPrefix(l, row.prefix) {
				line = l
			}
		}
		if line == "" {
			problems = append(problems, fmt.Sprintf("§6 has no row %q", strings.TrimPrefix(row.prefix, "| ")))
			continue
		}
		cols := strings.Split(strings.Trim(line, "| "), "|")
		if len(cols) < 2 {
			problems = append(problems, fmt.Sprintf("§6 row %q has no default", row.prefix))
			continue
		}
		mode, ok := docMode(cols[1])
		if !ok {
			problems = append(problems, fmt.Sprintf("§6 row %q: cannot read the default %q", row.prefix, strings.TrimSpace(cols[1])))
			continue
		}
		for _, a := range row.actions {
			listed[a] = true
			if got := table.Decide(a); got != mode {
				problems = append(problems, fmt.Sprintf("§6 says %s defaults to %s, policy.Default() says %s", a, mode, got))
			}
		}
	}
	for a := range table {
		if !listed[a] {
			problems = append(problems, fmt.Sprintf("policy.Default() has the action %s, §6 does not list it", a))
		}
	}
	sort.Strings(problems)
	return problems
}

// ---- the tests ----

func TestExitCodesMatchTheDesign(t *testing.T) {
	if p := exitCodeProblems(readDesign(t)); len(p) > 0 {
		t.Errorf("§9.2 and internal/exitcode disagree:\n  %s", strings.Join(p, "\n  "))
	}
}

func TestStateMachinesMatchTheDesign(t *testing.T) {
	if p := stateProblems(readDesign(t)); len(p) > 0 {
		t.Errorf("§4.1 and internal/domain disagree:\n  %s", strings.Join(p, "\n  "))
	}
}

func TestStateDiagramsMatchTheCode(t *testing.T) {
	if p := diagramProblems(readDesign(t)); len(p) > 0 {
		t.Errorf("the §4.1 diagrams and internal/domain disagree:\n  %s", strings.Join(p, "\n  "))
	}
}

func TestPolicyDefaultsMatchTheDesign(t *testing.T) {
	if p := policyProblems(readDesign(t)); len(p) > 0 {
		t.Errorf("§6 and internal/policy disagree:\n  %s", strings.Join(p, "\n  "))
	}
}

// The checks must notice a drift: each edit of the design below is the kind of
// change that goes unnoticed, and each must be reported.
func TestTheChecksNoticeDrift(t *testing.T) {
	doc := readDesign(t)
	edit := func(old, replacement string) string {
		if !strings.Contains(doc, old) {
			t.Fatalf("the design no longer contains %q: update this test", old)
		}
		return strings.Replace(doc, old, replacement, 1)
	}
	tests := []struct {
		name  string
		check func(string) []string
		doc   string
		want  string
	}{
		{"an exit code renumbered", exitCodeProblems, edit("3 not found", "9 not found"), "not found"},
		{"an exit code removed", exitCodeProblems, edit(" · 7 timeout", ""), "timeout"},
		{"an exit code invented", exitCodeProblems, edit("10 task failed", "10 task failed · 11 spooky"), "spooky"},
		{"a task transition added", stateProblems, edit("| `queued` | `running`, `cancelled` |", "| `queued` | `running`, `cancelled`, `completed` |"), "queued -> completed"},
		{"a task transition removed", stateProblems, edit("`awaiting_guidance`, `ready_for_review`, `failed`, `cancelled`", "`awaiting_guidance`, `ready_for_review`, `cancelled`"), "running -> failed"},
		{"a run state renamed", stateProblems, edit("| `interrupted` | `starting` (resume)", "| `lost` | `starting` (resume)"), "lost"},
		{"a terminal state made live", stateProblems, edit("| `stopped`, `failed` | none (terminal) |", "| `stopped` | none (terminal) |"), "failed"},
		{"a diagram arrow added", diagramProblems, edit("    queued --> cancelled\n", "    queued --> cancelled\n    queued --> completed\n"), "queued -> completed"},
		{"a diagram arrow removed", diagramProblems, edit("running --> failed\n", ""), "running -> failed"},
		{"a diagram terminal dropped", diagramProblems, edit("    deleted --> [*]\n", ""), "deleted"},
		{"a policy default loosened", policyProblems, edit("| Merge, tag, release, deploy | **forbid**", "| Merge, tag, release, deploy | auto"), "merge defaults to auto"},
		{"a policy default tightened", policyProblems, edit("| Commit in the topic's own checkout | auto |", "| Commit in the topic's own checkout | ask |"), "commit defaults to ask"},
		{"a policy row removed", policyProblems, edit("| Open/update PR, comment on issue | auto, after the push |\n", ""), "has no row"},
	}
	for _, tc := range tests {
		got := strings.Join(tc.check(tc.doc), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: the check reported %q, want it to mention %q", tc.name, got, tc.want)
		}
	}
}
