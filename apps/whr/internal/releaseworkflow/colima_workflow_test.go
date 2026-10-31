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

// watchdogGrace is the worst-case extra time of one bounded call: the TERM-to-KILL grace of the watchdog.
// TestColimaWatchdogGraceDefault pins it to the workflow text.
const watchdogGrace = 5

var (
	topKey     = regexp.MustCompile(`^[A-Za-z_-]+:`)
	childKey   = regexp.MustCompile(`^  ([A-Za-z_-]+):`)
	usesLine   = regexp.MustCompile(`^\s*(?:- )?uses:\s*(\S.*)$`)
	permsLine  = regexp.MustCompile(`^(\s*)permissions:\s*(.*)$`)
	leadingWS  = regexp.MustCompile(`^\s*`)
	colimaJobs = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*):\n`)
	exprRe     = regexp.MustCompile(`(?s)\$\{\{.*?\}\}`)
	runKeyRe   = regexp.MustCompile(`^(\s*)(?:- )?run:(?:\s+(.*))?$`)
	// The only expressions the workflow may contain: matrix values, the ref in the concurrency group,
	// the runner temp path, and step conditions. Anything else (inputs, tokens, event data) is a violation.
	allowedExpr = regexp.MustCompile(`^(matrix\.[a-z_]+|github\.ref|github\.workflow|runner\.temp|always\(\)|!cancelled\(\)|always\(\) && matrix\.extra_profile == 'true')$`)
	probeRe     = regexp.MustCompile(`\b(?:destroy_)?probe "?[A-Za-z0-9$_-]+"? ([0-9]+) `)
	boundedRe   = regexp.MustCompile(`\bbounded ([0-9]+) `)
	probeCallRe = regexp.MustCompile(`^(?:destroy_)?probe "?[A-Za-z0-9$_-]+"? (\S+)`)
	dockerCmdRe = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])docker `)
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

	// Expressions: only the allowlist, each on one line and closed; none inside any run: scalar.
	found := exprRe.FindAllString(raw, -1)
	if strings.Count(raw, "${{") != len(found) {
		out = append(out, "unclosed expression: ${{ without }}")
	}
	for _, m := range found {
		e := strings.TrimSuffix(strings.TrimPrefix(m, "${{"), "}}")
		switch {
		case strings.Contains(e, "\n"):
			out = append(out, sprintf("expression spans lines: %q", m))
		case !allowedExpr.MatchString(strings.TrimSpace(e)):
			out = append(out, sprintf("expression not allowed: ${{ %s }}", strings.TrimSpace(e)))
		}
	}
	for n, l := range lines {
		m := runKeyRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		scalar := m[2]
		if strings.HasPrefix(scalar, "|") || strings.HasPrefix(scalar, ">") {
			scalar = ""
			indent := len(m[1])
			for _, next := range lines[n+1:] {
				if strings.TrimSpace(next) != "" && len(leadingWS.FindString(next)) <= indent+1 {
					break
				}
				scalar += next + "\n"
			}
		}
		if strings.Contains(scalar, "${{") {
			out = append(out, sprintf("expression inside a run: scalar at line %d", n+1))
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
		if strings.Contains(t, "nerdctl install") {
			out = append(out, sprintf("must not run nerdctl install: %s", t))
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
				weight := 1
				if strings.Contains(l, "$attempt") {
					weight = 2 // the start loop runs twice
				}
				if strings.Contains(l, "docker rm -f") {
					weight = 2 // one throwaway container, with one spare
				}
				if strings.Contains(l, "docker volume rm") {
					weight = 8 // up to eight throwaway volumes are removed one by one
				}
				for _, re := range []*regexp.Regexp{probeRe, boundedRe} {
					if pm := re.FindStringSubmatch(l); pm != nil {
						n, _ := strconv.Atoi(pm[1])
						seconds += weight * (n + watchdogGrace)
					}
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

	// Runtime: the matrix row with the containerd runtime exists, every row names its runtime, the runtime reaches the steps
	// only through env, no default-profile start hard-codes it, and every docker-dependent probe sits behind the guard
	// that skips it for other runtimes.
	if a, b := strings.Index(raw, "        include:\n"), strings.Index(raw, "\n    env:\n"); a < 0 || b < a {
		out = append(out, "no matrix include block")
	} else {
		hasContainerd := false
		for _, row := range strings.Split(raw[a:b+1], "\n          - name: ")[1:] {
			if !strings.Contains(row, "\n            runtime: ") {
				out = append(out, sprintf("matrix row without a runtime key: %.40s", row))
			}
			if strings.HasPrefix(row, "intel-containerd-qemu-sshfs\n") && strings.Contains(row, "runner: macos-15-intel\n") &&
				strings.Contains(row, "vm_type: qemu\n") && strings.Contains(row, "mount_type: sshfs\n") && strings.Contains(row, "runtime: containerd\n") {
				hasContainerd = true
			}
		}
		if !hasContainerd {
			out = append(out, "lacks the intel-containerd-qemu-sshfs containerd row")
		}
	}
	if !strings.Contains(raw, "\n      COLIMA_RUNTIME: ${{ matrix.runtime }}\n") {
		out = append(out, "COLIMA_RUNTIME is not passed from matrix.runtime through env")
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") || !strings.Contains(t, "colima start ") || strings.Contains(t, "-p whr-acc") {
			continue
		}
		if !strings.Contains(t, `--runtime "$COLIMA_RUNTIME"`) {
			out = append(out, sprintf("colima start must use --runtime \"$COLIMA_RUNTIME\": %s", t))
		}
	}
	const runtimeGuard = `if [ "$COLIMA_RUNTIME" != docker ]; then`
	for anchor, names := range map[string][]string{
		"- name: Measure the runtime": {"docker-context-ls", "docker-info", "docker-pull", "docker-run-pinned", "bind-runner-temp", "bind-home", "bind-tmpdir", "volume-roundtrip", "tar-pipe-200mib"},
		"- name: Measure whr against": {"whr-throwaway-setup", "whr-ls-local-json-throwaway", "whr-home-clone-dry-run", "whr-home-clone-apply", "whr-home-clone-verify", "whr-home-backup-apply", "whr-home-backup-verify", "whr-recover-dry-run"},
	} {
		si := strings.Index(raw, anchor)
		if si < 0 {
			out = append(out, sprintf("no step %q", anchor))
			continue
		}
		step := raw[si+1:]
		if next := strings.Index(step, "\n      - "); next >= 0 {
			step = step[:next]
		}
		g := strings.Index(step, runtimeGuard)
		if g < 0 {
			out = append(out, sprintf("step %q lacks the non-docker runtime guard", anchor))
			continue
		}
		e := strings.Index(step[g:], "exit 0")
		if e < 0 {
			out = append(out, sprintf("step %q: the non-docker runtime guard does not exit", anchor))
			continue
		}
		e += g
		for _, name := range names {
			loc := regexp.MustCompile(`(?m)^\s*probe ` + regexp.QuoteMeta(name) + ` `).FindStringIndex(step)
			switch {
			case loc == nil:
				out = append(out, sprintf("docker-only probe %s is missing from the docker branch", name))
			case loc[0] < e:
				out = append(out, sprintf("docker-only probe %s is not guarded for non-docker runtimes", name))
			}
		}
	}
	// A probe that runs a docker command belongs to the guarded docker branch of the measure and whr steps. The
	// non-default profile step runs on the docker row only (extra_profile), and the cleanup step uses no probes.
	for _, st := range strings.Split(raw[strings.Index(raw, "\n    steps:\n")+1:], "\n      - ")[1:] {
		if strings.HasPrefix(st, "name: Measure a non-default profile") || strings.HasPrefix(st, "name: Remove throwaway") {
			continue
		}
		end := len(st)
		if strings.HasPrefix(st, "name: Measure the runtime") || strings.HasPrefix(st, "name: Measure whr against") {
			g := strings.Index(st, runtimeGuard)
			if g < 0 {
				continue // reported above
			}
			end = g + strings.Index(st[g:], "exit 0")
		}
		for _, l := range strings.Split(st[:end], "\n") {
			t := strings.TrimSpace(l)
			if probeCallRe.MatchString(t) && !strings.Contains(t, " brew ") && dockerCmdRe.MatchString(t) {
				out = append(out, sprintf("docker command in a probe outside the guarded docker branch: %s", t))
			}
		}
	}
	// Every probe call carries a numeric bound as its second argument.
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if m := probeCallRe.FindStringSubmatch(t); m != nil && !regexp.MustCompile(`^[0-9]+$`).MatchString(m[1]) {
			out = append(out, sprintf("probe without a numeric bound: %s", t))
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
		"runner: macos-15-intel\n            vm_type: qemu\n            mount_type: sshfs\n            runtime: docker\n",
		"runner: macos-15-intel\n            vm_type: qemu\n            mount_type: 9p\n            runtime: docker\n",
		"runner: macos-15\n            vm_type: vz\n            mount_type: virtiofs\n            runtime: docker\n",
		"name: intel-containerd-qemu-sshfs\n            runner: macos-15-intel\n            vm_type: qemu\n            mount_type: sshfs\n            runtime: containerd\n",
		"COLIMA_RUNTIME: ${{ matrix.runtime }}\n",
		"colima start --runtime \"$COLIMA_RUNTIME\" --vm-type \"$VM_TYPE\" --mount-type \"$MOUNT_TYPE\"",
		"          brew install colima docker\n",
		"          docker --version\n",
		"for d in \"$HOME/.colima/default\" \"$HOME/.colima/_lima/colima\"; do",
		"if [ \"$VM_TYPE\" = qemu ]; then\n            brew install qemu\n            qemu-img --version\n",
		"extra_profile: 'true'",
		"colima start -p whr-acc --runtime docker",
		"if: ${{ always() }}\n        shell: bash\n        timeout-minutes: 8",
		"probe colima-nerdctl-version 20 colima nerdctl -- version",
		"probe colima-nerdctl-help 10 colima nerdctl --help",
		"skipped_runtime \"$n\"",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	if c, d := strings.Count(raw, "            runtime: containerd\n"), strings.Count(raw, "            runtime: docker\n"); c != 1 || d != 3 {
		t.Errorf("runtime rows: containerd %d docker %d, want 1 and 3", c, d)
	}
	for _, name := range []string{"containerd-sockets", "colima-yaml-top-level-keys", "colima-nerdctl-version", "lima-host-agent-log-tail", "whr-doctor-json", "whr-ls-local-json"} {
		if !strings.Contains(raw, "probe "+name+" ") {
			t.Errorf("workflow lacks the probe %s", name)
		}
	}
	// The non-docker run must not set DOCKER_HOST for whr.
	if !strings.Contains(raw, "unset DOCKER_HOST\n          whr=") {
		t.Error("the whr step must unset DOCKER_HOST before measuring")
	}
	if strings.Count(raw, "extra_profile: 'true'") != 1 {
		t.Error("exactly one row (intel qemu sshfs) measures the non-default profile")
	}
}

func TestColimaWorkflowRulesBite(t *testing.T) {
	raw := readRepo(t, colimaWorkflow)
	sha := "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1"
	env := "      GOTOOLCHAIN: local\n"
	cleanupAnchor := "          unset DOCKER_HOST\n          rm -rf"
	// Each case must produce the expected violation, not just any: a case that fails for a second reason proves nothing.
	cases := []struct{ name, from, to, want string }{
		{"pull_request trigger", "on:\n  workflow_dispatch:\n", "on:\n  workflow_dispatch:\n  pull_request:\n", "triggers [workflow_dispatch pull_request]"},
		{"schedule trigger", "on:\n  workflow_dispatch:\n", "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 3 * * *'\n", "triggers [workflow_dispatch schedule]"},
		{"push trigger instead", "on:\n  workflow_dispatch:\n", "on:\n  push:\n", "triggers [push]"},
		{"wider permissions", "permissions:\n  contents: read\n", "permissions:\n  contents: read\n  pull-requests: write\n", "permissions grant [contents: read pull-requests: write]"},
		{"write permission", "permissions:\n  contents: read\n", "permissions:\n  contents: write\n", "permissions grant [contents: write]"},
		{"job-level permissions", "    continue-on-error: true\n", "    continue-on-error: true\n    permissions:\n      contents: read\n      id-token: write\n", "permissions grant [contents: read id-token: write]"},
		{"tag instead of SHA", sha, "actions/checkout@v7 # v7.0.1", "action not pinned to a full SHA with a version comment: actions/checkout@v7"},
		{"SHA without version comment", sha, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "action not pinned"},
		{"missing continue-on-error", "    continue-on-error: true\n", "", "lacks continue-on-error: true"},
		{"continue-on-error false", "    continue-on-error: true\n", "    continue-on-error: false\n", "lacks continue-on-error: true"},
		{"fail-fast on", "      fail-fast: false\n", "      fail-fast: true\n", "lacks fail-fast: false"},
		{"single-line run with an expression", "      - name: Prepare probe helpers\n", "      - run: echo \"${{ github.event.inputs.x }}\"\n        timeout-minutes: 1\n      - name: Prepare probe helpers\n", "expression inside a run: scalar"},
		{"dash run with an allowed expression", "      - name: Prepare probe helpers\n", "      - run: echo ${{ matrix.name }}\n        timeout-minutes: 1\n      - name: Prepare probe helpers\n", "expression inside a run: scalar"},
		{"run with two spaces and an expression", "          set -eu\n          brew --version\n", "          set -eu\n          echo ${{ runner.temp }}\n          brew --version\n", "expression inside a run: scalar"},
		{"folded run with a split expression", "      - name: Prepare probe helpers\n", "      - run: >-\n          echo ${{\n          matrix.name }}\n        timeout-minutes: 1\n      - name: Prepare probe helpers\n", "expression spans lines"},
		{"split expression in env", env, env + "      T: ${{ github.\n        token }}\n", "expression spans lines"},
		{"split expression in with", "          cache: false\n", "          cache: ${{ matrix.\n          name }}\n", "expression spans lines"},
		{"unclosed expression", env, env + "      T: ${{ matrix.name\n", "unclosed expression"},
		{"persist-credentials true", "persist-credentials: false", "persist-credentials: true", "must not contain \"persist-credentials: true\""},
		{"token in env", env, env + "      T: ${{ github.token }}\n", "expression not allowed: ${{ github.token }}"},
		{"input in env", env, env + "      T: ${{ inputs.x }}\n", "expression not allowed: ${{ inputs.x }}"},
		{"unguarded colima delete", "on_hosted_runner && bounded 40 colima delete --force", "bounded 40 colima delete --force", "unguarded destructive command"},
		{"unguarded destroy in the start loop", "destroy_probe \"colima-cleanup-after-start-$attempt\" 45 colima delete --force", "probe \"colima-cleanup-after-start-$attempt\" 45 colima delete --force", "unguarded destructive command"},
		{"colima stop unguarded", cleanupAnchor, "          unset DOCKER_HOST\n          colima stop --force\n          rm -rf", "unguarded destructive command: colima stop"},
		{"docker branch probe deleted", "          probe docker-context-ls 10 docker context ls\n", "", "docker-only probe docker-context-ls is missing from the docker branch"},
		{"docker probe in the start step", "          started=false\n", "          probe docker-version 5 docker version\n          started=false\n", "docker command in a probe outside the guarded docker branch: probe docker-version"},
		{"nerdctl install", "          probe colima-nerdctl-help 10 colima nerdctl --help\n", "          probe colima-nerdctl-help 10 colima nerdctl --help\n          probe x 60 colima nerdctl install\n", "must not run nerdctl install"},
		{"prune", cleanupAnchor, "          unset DOCKER_HOST\n          docker system prune -af\n          rm -rf", "must not prune"},
		{"cleanup without refusal", "if ! on_hosted_runner; then", "if false; then", "cleanup removes things before refusing"},
		{"step without timeout", "        timeout-minutes: 2\n        with:\n          go-version-file", "        with:\n          go-version-file", "step without timeout-minutes"},
		{"probe bound over step", "probe whr-env 5 ", "probe whr-env 5000 ", "probe bounds"},
		{"start loop bound over step", "probe \"colima-start-$attempt\" 200 ", "probe \"colima-start-$attempt\" 2000 ", "probe bounds"},
		{"bounded call over step", "bounded 40 colima delete -p", "bounded 4000 colima delete -p", "probe bounds"},
		{"containerd row missing", "name: intel-containerd-qemu-sshfs\n", "name: intel-other-qemu-sshfs\n", "lacks the intel-containerd-qemu-sshfs containerd row"},
		{"containerd row runs docker", "            runtime: containerd\n", "            runtime: docker\n", "lacks the intel-containerd-qemu-sshfs containerd row"},
		{"row without a runtime key", "            mount_type: 9p\n            runtime: docker\n", "            mount_type: 9p\n", "matrix row without a runtime key"},
		{"runtime not passed through env", "      COLIMA_RUNTIME: ${{ matrix.runtime }}\n", "      COLIMA_RUNTIME: docker\n", "COLIMA_RUNTIME is not passed from matrix.runtime through env"},
		{"docker runtime hard-coded in the start step", "colima start --runtime \"$COLIMA_RUNTIME\" --vm-type", "colima start --runtime docker --vm-type", "colima start must use --runtime"},
		{"docker-only probe not guarded", "          probe colima-list 10 colima list\n", "          probe colima-list 10 colima list\n          probe docker-info 15 docker info\n", "docker-only probe docker-info is not guarded for non-docker runtimes"},
		{"docker-only whr probe not guarded", "          probe whr-ls-local-json 10 \"$whr\" ls --local --json\n", "          probe whr-ls-local-json 10 \"$whr\" ls --local --json\n          probe whr-recover-dry-run 25 \"$whr\" workspace recover --local x --dry-run\n", "docker-only probe whr-recover-dry-run is not guarded for non-docker runtimes"},
		{"measure guard missing", "if [ \"$COLIMA_RUNTIME\" != docker ]; then\n            # Not docker", "if false; then\n            # Not docker", "lacks the non-docker runtime guard"},
		{"new probe without a bound", "probe colima-nerdctl-version 20 colima", "probe colima-nerdctl-version colima", "probe without a numeric bound"},
		{"new probe bound over the step", "probe colima-nerdctl-version 20 ", "probe colima-nerdctl-version 2000 ", "probe bounds"},
		{"measure step timeout too small for the new probes", "if: ${{ always() }}\n        shell: bash\n        timeout-minutes: 8", "if: ${{ always() }}\n        shell: bash\n        timeout-minutes: 7", "probe bounds"},
		{"install nerdctl probe over the step", "probe colima-nerdctl-help 10 ", "probe colima-nerdctl-help 1000 ", "probe bounds"},
		{"job cap too small", "    timeout-minutes: 50\n", "    timeout-minutes: 30\n", "step timeouts sum to 46 min, job cap 30"},
	}
	for _, c := range cases {
		if strings.Count(raw, c.from) < 1 {
			t.Fatalf("%s: mutation anchor %q not found", c.name, c.from)
		}
		v := strings.Join(colimaViolations(strings.Replace(raw, c.from, c.to, 1)), "\n")
		if !strings.Contains(v, c.want) {
			t.Errorf("%s: violations %q lack %q", c.name, v, c.want)
		}
	}
}

func TestColimaWatchdogGraceDefault(t *testing.T) {
	want := "$ENV{COLIMA_WATCHDOG_GRACE} : " + strconv.Itoa(watchdogGrace) + ";"
	if !strings.Contains(shellBlock(t, readRepo(t, colimaWorkflow), "WATCHDOG"), want) {
		t.Errorf("watchdog default grace is not %d s; update watchdogGrace and the budget", watchdogGrace)
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
	line, elapsed, _ := probeRunErr(t, secs, cmd...)
	return line, elapsed
}

// probeRunErr is probeRun plus the recorded stderr of the probe.
func probeRunErr(t *testing.T, secs string, cmd ...string) (string, time.Duration, string) {
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
	e, _ := os.ReadFile(filepath.Join(dir, "colima-acceptance", "logs", "t.err"))
	return strings.TrimSpace(string(b)), elapsed, string(e)
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

func TestColimaWatchdogIsQuietOnNormalCalls(t *testing.T) {
	line, _, stderr := probeRunErr(t, "5", "true")
	if !strings.Contains(line, "\tobserved\t") || stderr != "" {
		t.Errorf("result %q with stderr %q, want observed and no stderr noise", line, stderr)
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

func TestColimaSkippedRuntimeRecordsTheRuntime(t *testing.T) {
	need(t, "bash")
	raw := readRepo(t, colimaWorkflow)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.sh"), []byte(shellBlock(t, raw, "LIB")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "colima-acceptance", "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("bash", "-c", `. "$RUNNER_TEMP/lib.sh"; skipped_runtime docker-info`)
	c.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "COLIMA_RUNTIME=containerd", "GITHUB_RUN_ID=1", "GITHUB_RUN_ATTEMPT=1", "MATRIX_NAME=t")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "colima-acceptance", "probes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "docker-info\tskipped: runtime containerd\t0\tdocker-info\n"; got != want {
		t.Errorf("recorded %q, want %q", got, want)
	}
}
