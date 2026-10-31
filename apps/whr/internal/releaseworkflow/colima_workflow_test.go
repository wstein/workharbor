// SPDX-License-Identifier: EUPL-1.2

package releaseworkflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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
	exprRe     = regexp.MustCompile(`\$\{\{(.*?)\}\}`)
	// The only expressions the workflow may contain: matrix values, the ref in the concurrency group,
	// the runner temp path, and step conditions. Anything else (inputs, tokens, event data) is a violation.
	allowedExpr = regexp.MustCompile(`^(matrix\.[a-z_]+|github\.ref|github\.workflow|runner\.temp|always\(\)|!cancelled\(\)|always\(\) && matrix\.extra_profile == 'true')$`)
	probeRe     = regexp.MustCompile(`\b(?:destroy_)?probe [A-Za-z0-9$_-]+ ([0-9]+) `)
	timeoutRe   = regexp.MustCompile(`(?m)^\s*timeout-minutes: ([0-9]+)$`)
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

	// Expressions: only the allowlist; a single-line run: with an expression is an injection path.
	for _, m := range exprRe.FindAllStringSubmatch(raw, -1) {
		if e := strings.TrimSpace(m[1]); !allowedExpr.MatchString(e) {
			out = append(out, sprintf("expression not allowed: ${{ %s }}", e))
		}
	}
	for _, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "run:") && strings.Contains(t, "${{") {
			out = append(out, sprintf("expression inside a run: line: %s", t))
		}
	}
	for _, bad := range []string{"github.token", "GITHUB_TOKEN", "github.event.inputs", "persist-credentials: true"} {
		if strings.Contains(raw, bad) {
			out = append(out, sprintf("must not contain %q", bad))
		}
	}
	if c, f := strings.Count(raw, "uses: actions/checkout@"), strings.Count(raw, "persist-credentials: false"); c > f {
		out = append(out, "a checkout lacks persist-credentials: false")
	}

	// Destructive commands: every colima delete/stop line is guarded; nothing prunes.
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if (strings.Contains(t, "colima delete") || strings.Contains(t, "colima stop")) &&
			!strings.Contains(t, "destroy_probe ") && !strings.Contains(t, "on_hosted_runner &&") {
			out = append(out, sprintf("unguarded destructive command: %s", t))
		}
		if strings.Contains(t, " prune") {
			out = append(out, sprintf("must not prune: %s", t))
		}
	}
	if c := strings.Index(raw, "- name: Remove throwaway"); c < 0 {
		out = append(out, "no cleanup step")
	} else {
		cleanup := raw[c:]
		g, r := strings.Index(cleanup, "if ! on_hosted_runner; then"), strings.Index(cleanup, "docker rm")
		if g < 0 || r < 0 || g > r {
			out = append(out, "cleanup removes things before refusing on a non-hosted runner")
		}
	}

	// Budgets: every step has a timeout, the steps fit the job cap, and the probe bounds fit each step.
	if m := regexp.MustCompile(`(?m)^    timeout-minutes: ([0-9]+)$`).FindStringSubmatch(raw); m == nil {
		out = append(out, "job has no timeout-minutes")
	} else {
		capMin, _ := strconv.Atoi(m[1])
		total := 0
		for _, st := range strings.Split(raw[strings.Index(raw, "\n    steps:\n")+1:], "\n      - ")[1:] {
			tm := regexp.MustCompile(`(?m)^        timeout-minutes: ([0-9]+)$`).FindStringSubmatch(st)
			if tm == nil {
				out = append(out, sprintf("step without timeout-minutes: %.40s", st))
				continue
			}
			minutes, _ := strconv.Atoi(tm[1])
			total += minutes
			seconds := 0
			for _, l := range strings.Split(st, "\n") {
				if pm := probeRe.FindStringSubmatch(l); pm != nil {
					n, _ := strconv.Atoi(pm[1])
					if strings.Contains(l, "$attempt") {
						n *= 2 // the start loop runs twice
					}
					seconds += n
				}
			}
			if seconds > minutes*60 {
				out = append(out, sprintf("probe bounds %ds exceed the step timeout %dmin: %.40s", seconds, minutes, st))
			}
		}
		if total > capMin {
			out = append(out, sprintf("step timeouts sum to %d min, job cap %d", total, capMin))
		}
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
		"if [ \"$VM_TYPE\" = qemu ]; then\n            brew install qemu\n            qemu-img --version\n",
		"extra_profile: 'true'",
		"colima start -p whr-acc",
		"if: ${{ always() }}\n        shell: bash\n        timeout-minutes: 7",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	if strings.Count(raw, "extra_profile: 'true'") != 1 {
		t.Error("exactly one row (intel qemu sshfs) measures the non-default profile")
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
		{"single-line run with an expression", "          set -eu\n          brew --version\n", "          set -eu\n          brew --version\n          echo \"x\"\n      - run: echo \"${{ github.event.inputs.x }}\"\n        timeout-minutes: 1\n"},
		{"persist-credentials true", "persist-credentials: false", "persist-credentials: true"},
		{"token in env", "      GOTOOLCHAIN: local\n", "      GOTOOLCHAIN: local\n      T: ${{ github.token }}\n"},
		{"unguarded colima delete", "on_hosted_runner && bounded 40 colima delete --force", "bounded 40 colima delete --force"},
		{"unguarded destroy in the start loop", "destroy_probe \"colima-cleanup-after-start-$attempt\" 45 colima delete --force", "probe \"colima-cleanup-after-start-$attempt\" 45 colima delete --force"},
		{"colima stop unguarded", "          unset DOCKER_HOST\n          rm -rf", "          unset DOCKER_HOST\n          colima stop --force\n          rm -rf"},
		{"prune", "          unset DOCKER_HOST\n          rm -rf", "          unset DOCKER_HOST\n          docker system prune -af\n          rm -rf"},
		{"cleanup without refusal", "if ! on_hosted_runner; then", "if false; then"},
		{"step without timeout", "        timeout-minutes: 2\n        with:\n          go-version-file", "        with:\n          go-version-file"},
		{"probe bound over step", "probe whr-env 5 ", "probe whr-env 5000 "},
		{"job cap too small", "    timeout-minutes: 45\n", "    timeout-minutes: 30\n"},
	}
	for _, c := range cases {
		if strings.Count(raw, c.from) < 1 {
			t.Fatalf("%s: mutation anchor %q not found", c.name, c.from)
		}
		v := colimaViolations(strings.Replace(raw, c.from, c.to, 1))
		if len(v) == 0 {
			t.Errorf("%s: no violation reported", c.name)
		}
		t.Logf("%s -> %v", c.name, v)
	}
}

// shellBlock returns the body of the here-document named tag in the workflow, dedented by its 10 columns.
func shellBlock(t *testing.T, raw, tag string) string {
	t.Helper()
	lines := strings.Split(raw, "\n")
	var out []string
	in := false
	for _, l := range lines {
		switch {
		case !in && strings.HasSuffix(l, "<<'"+tag+"'"):
			in = true
		case in && strings.TrimSpace(l) == tag:
			return strings.Join(out, "\n") + "\n"
		case in:
			out = append(out, strings.TrimPrefix(l, "          "))
		}
	}
	t.Fatalf("here-document %s not found", tag)
	return ""
}

// probeRun runs `probe NAME SECS CMD...` from the workflow's real helper text in a scratch directory
// and returns the recorded result line and the elapsed time.
func probeRun(t *testing.T, secs string, cmd ...string) (string, time.Duration) {
	t.Helper()
	need(t, "bash", "perl")
	raw := readRepo(t, colimaWorkflow)
	dir := t.TempDir()
	for name, tag := range map[string]string{"colima-watchdog.pl": "WATCHDOG", "colima-lib.sh": "LIB"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(shellBlock(t, raw, tag)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "colima-acceptance", "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-c", `. "$RUNNER_TEMP/colima-lib.sh"; probe t "$@"`, "_", secs}, cmd...)
	c := exec.Command("bash", args...)
	c.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "GITHUB_RUN_ID=1", "GITHUB_RUN_ATTEMPT=1", "MATRIX_NAME=t", "COLIMA_WATCHDOG_GRACE=1")
	start := time.Now()
	done := make(chan error, 1)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- c.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		_ = c.Process.Kill()
		t.Fatal("probe did not return within 20 s: the watchdog does not bound the command")
	}
	elapsed := time.Since(start)
	b, err := os.ReadFile(filepath.Join(dir, "colima-acceptance", "probes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b)), elapsed
}

func TestColimaWatchdogKillsCommandsThatIgnoreSignals(t *testing.T) {
	// A Go-like child: ignores SIGALRM and SIGTERM, so only the group KILL stops it.
	line, elapsed := probeRun(t, "1", "perl", "-e", `$SIG{ALRM}="IGNORE"; $SIG{TERM}="IGNORE"; $SIG{INT}="IGNORE"; sleep 30`)
	if !strings.Contains(line, "failed: timeout after 1s") {
		t.Errorf("result %q, want a timeout failure", line)
	}
	if elapsed > 8*time.Second {
		t.Errorf("took %v for a 1 s bound with a 1 s grace", elapsed)
	}
	// A shell child with a grandchild that also ignores TERM.
	line, elapsed = probeRun(t, "1", "sh", "-c", `trap "" TERM ALRM; sleep 30 & sleep 30`)
	if !strings.Contains(line, "failed: timeout after 1s") || elapsed > 8*time.Second {
		t.Errorf("result %q after %v, want a timeout within the bound", line, elapsed)
	}
}

func TestColimaProbeRecordsStatus(t *testing.T) {
	if line, _ := probeRun(t, "5", "true"); !strings.Contains(line, "\tobserved\t") {
		t.Errorf("result %q, want observed", line)
	}
	if line, _ := probeRun(t, "5", "sh", "-c", "exit 3"); !strings.Contains(line, "failed: exit 3") {
		t.Errorf("result %q, want exit 3", line)
	}
	if line, _ := probeRun(t, "5", "no-such-command-colima-acceptance"); !strings.Contains(line, "failed: exit 127") {
		t.Errorf("result %q, want exit 127", line)
	}
}

func TestColimaDestructiveCommandsRefuseOutsideHostedRunners(t *testing.T) {
	need(t, "bash", "perl")
	raw := readRepo(t, colimaWorkflow)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.sh"), []byte(shellBlock(t, raw, "LIB")), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, env := range [][]string{{"GITHUB_ACTIONS=", "RUNNER_ENVIRONMENT="}, {"GITHUB_ACTIONS=true", "RUNNER_ENVIRONMENT=self-hosted"}, {"GITHUB_ACTIONS=", "RUNNER_ENVIRONMENT=github-hosted"}} {
		c := exec.Command("bash", "-c", `. "$D/lib.sh"; on_hosted_runner`)
		c.Env = append(os.Environ(), append(env, "D="+dir)...)
		if err := c.Run(); err == nil {
			t.Errorf("%v: on_hosted_runner succeeded", env)
		}
	}
	c := exec.Command("bash", "-c", `. "$D/lib.sh"; on_hosted_runner`)
	c.Env = append(os.Environ(), "GITHUB_ACTIONS=true", "RUNNER_ENVIRONMENT=github-hosted", "D="+dir)
	if err := c.Run(); err != nil {
		t.Errorf("hosted runner refused: %v", err)
	}
}
