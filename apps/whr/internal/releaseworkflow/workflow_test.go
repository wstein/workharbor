// SPDX-License-Identifier: EUPL-1.2

package releaseworkflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The workflow is checked as text: the module has no YAML dependency and the rules are about
// the presence of lines, not about a parsed tree. Each test names the rule it pins.

// platforms is the release matrix: archive target, GOOS and GOARCH. It is the set of targets that
// compile and whose behaviour is meaningful (docs/releasing.md). A change to the
// workflow matrix must change this table, the plan and the notes script together.
var platforms = []struct{ target, goos, goarch string }{
	{"linux_amd64", "linux", "amd64"},
	{"linux_arm64", "linux", "arm64"},
	{"darwin_arm64", "darwin", "arm64"},
}

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var jobHeader = regexp.MustCompile(`(?m)^  ([a-z-]+):\n`)

// jobs splits the workflow text into its jobs by name.
func jobs(t *testing.T, raw string) map[string]string {
	t.Helper()
	i := strings.Index(raw, "\njobs:\n")
	if i < 0 {
		t.Fatal("no jobs:")
	}
	body := raw[i+len("\njobs:\n"):]
	locs := jobHeader.FindAllStringSubmatchIndex(body, -1)
	out := map[string]string{}
	for n, l := range locs {
		end := len(body)
		if n+1 < len(locs) {
			end = locs[n+1][0]
		}
		out[body[l[2]:l[3]]] = body[l[0]:end]
	}
	return out
}

func TestWorkflowTriggersAreTagAndDispatchOnly(t *testing.T) {
	raw := readRepo(t, ".github/workflows/release.yml")
	head := raw[:strings.Index(raw, "\njobs:")]
	for _, want := range []string{"\non:\n  push:\n    tags: ['v[0-9]+.[0-9]+.[0-9]+', 'v[0-9]+.[0-9]+.[0-9]+-*']\n  workflow_dispatch:\n", "\npermissions:\n  contents: read\n", "\nconcurrency:\n", "cancel-in-progress: false"} {
		if !strings.Contains(head, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	for _, bad := range []string{"pull_request", "branches:", "schedule:", "workflow_run:", "repository_dispatch"} {
		if strings.Contains(raw, bad) {
			t.Errorf("workflow must not contain %q", bad)
		}
	}
	if !strings.HasPrefix(raw, "# SPDX-License-Identifier: EUPL-1.2\n") {
		t.Error("missing SPDX header")
	}
}

var pinned = regexp.MustCompile(`^[a-z0-9-]+/[A-Za-z0-9._-]+@[0-9a-f]{40} # v[0-9][0-9.]*$`)

func TestEveryActionIsPinnedToACommit(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		f := e.Name()
		raw := readRepo(t, ".github/workflows/"+f)
		n := 0
		for _, line := range strings.Split(raw, "\n") {
			s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if !strings.HasPrefix(s, "uses:") {
				continue
			}
			n++
			ref := strings.TrimSpace(strings.TrimPrefix(s, "uses:"))
			if strings.HasPrefix(ref, "./") {
				continue // a reusable workflow of this repository
			}
			if !pinned.MatchString(ref) {
				t.Errorf("%s: action not pinned to a full SHA with a version comment: %s", f, ref)
			}
		}
		if n == 0 {
			t.Errorf("%s: no uses: lines found", f)
		}
	}
}

func TestEveryJobHasATimeoutAndExplicitPermissions(t *testing.T) {
	js := jobs(t, readRepo(t, ".github/workflows/release.yml"))
	for _, name := range []string{"verify", "build", "smoke", "publish"} {
		j, ok := js[name]
		if !ok {
			t.Fatalf("job %s missing", name)
		}
		if !strings.Contains(j, "    timeout-minutes: ") || !strings.Contains(j, "    permissions:\n") {
			t.Errorf("job %s lacks timeout-minutes or permissions", name)
		}
	}
	if len(js) != 4 {
		t.Errorf("jobs %v, want exactly verify, build, smoke, publish", keys(js))
	}
}

func keys(m map[string]string) []string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

func TestOnlyPublishCanWriteAndAttest(t *testing.T) {
	raw := readRepo(t, ".github/workflows/release.yml")
	js := jobs(t, raw)
	for _, perm := range []string{"contents: write", "attestations: write", "id-token: write"} {
		if strings.Count(raw, perm) != 1 || !strings.Contains(js["publish"], perm) {
			t.Errorf("%q must appear once, in the publish job", perm)
		}
	}
	if strings.Contains(raw, "write-all") || strings.Contains(raw, "secrets.") {
		t.Error("write-all and secrets are not used")
	}
	p := js["publish"]
	for _, want := range []string{
		"    environment: release\n",
		"needs.verify.outputs.dry_run == 'false' && github.event_name == 'push' && github.ref_type == 'tag'",
		"needs: [verify, build, smoke]",
		"actions/attest-build-provenance@",
		"--verify-tag", "vars.WHR_RELEASE_DRAFT", "--prerelease", "needs.verify.outputs.prerelease",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("publish lacks %q", want)
		}
	}
	for _, rule := range []string{`test "$ACTOR" = "$OWNER"`, `test "$PUSHER" = "$OWNER"`, "releases\" --paginate", "git ls-remote", "SHA256SUMS", "sha256sum --check"} {
		if !strings.Contains(p, rule) {
			t.Errorf("publish does not recheck %q", rule)
		}
	}
	if strings.Contains(p, "workflow_dispatch") {
		t.Error("publish must not mention dispatch")
	}
}

func TestVerifyJobGates(t *testing.T) {
	v := jobs(t, readRepo(t, ".github/workflows/release.yml"))["verify"]
	for _, want := range []string{
		"release-verify.sh version", "release-verify.sh dryversion", "release-verify.sh prerelease", "release-verify.sh tag",
		`test "$ACTOR" = "$OWNER"`, `test "$PUSHER" = "$OWNER"`,
		"release-verify.sh ci-green", "releases/tags/$TAG", "go mod verify", "go vet ./...",
		"scripts/check_repo.py", "go test -count=1 ./internal/releaseworkflow", "go-version-file: apps/whr/go.mod", "working-directory: apps/whr",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("verify lacks %q", want)
		}
	}
	if got := strings.Count(v, "if: steps.resolve.outputs.dry_run == 'false'"); got != 3 {
		t.Errorf("%d tag-only steps in verify, want 3 (annotated tag, green ci, no release)", got)
	}
}

// ciGuardViolations checks the green-CI step of the verify job. The retry, the client-side fallback
// and the head_sha, conclusion and event filters live in release-verify.sh (tested against a fake gh in
// scripts_test.go); the step must call it for the run's commit, with the read-only token and the
// repository, and only for a tag run.
func ciGuardViolations(raw string) []string {
	var out []string
	var found []step
	for _, st := range stepsOf(jobsOf(raw)["verify"]) {
		if strings.Contains(st.text, "- name: Require a green check run for the tagged commit\n") {
			found = append(found, st)
		}
	}
	if len(found) != 1 {
		return []string{sprintf("%d green-check steps in verify, want 1", len(found))}
	}
	st := found[0]
	if !strings.Contains(st.text, "        if: steps.resolve.outputs.dry_run == 'false'\n") {
		out = append(out, "the green-check step is not skipped for a dry run only")
	}
	if !strings.Contains(st.text, "        env:\n          GH_TOKEN: ${{ github.token }}\n          REPO: ${{ github.repository }}\n        run: |\n") {
		out = append(out, "the green-check step env is not exactly GH_TOKEN and REPO")
	}
	if !strings.Contains(st.run, "\nsh .github/scripts/release-verify.sh ci-green \"$GITHUB_SHA\"") || !strings.HasPrefix(st.run, "set -euo pipefail\n") {
		out = append(out, "the green-check step does not call ci-green for $GITHUB_SHA under set -euo pipefail")
	}
	if strings.Contains(st.run, "||") || strings.Contains(st.run, "gh api") {
		out = append(out, "the green-check step queries or tolerates failure itself")
	}
	return out
}

func TestCIGuardStepCallsTheScript(t *testing.T) {
	raw := readRepo(t, ".github/workflows/release.yml")
	if v := ciGuardViolations(raw); len(v) != 0 {
		t.Errorf("violations:\n%s", strings.Join(v, "\n"))
	}
	call := `sh .github/scripts/release-verify.sh ci-green "$GITHUB_SHA"`
	tail := "          REPO: ${{ github.repository }}\n        run: |\n          set -euo pipefail\n          # check.yml"
	for _, c := range []struct{ name, from, to string }{
		{"guard call removed", call, "true"},
		{"guard for another commit", `ci-green "$GITHUB_SHA"`, `ci-green "$GITHUB_REF"`},
		{"guard failure tolerated", call, call + " || true"},
		{"guard failure tolerated by echo", call, call + " || echo ignored"},
		{"guard run without strict mode", "set -euo pipefail\n          # check.yml", "# check.yml"},
		{"token dropped", "          GH_TOKEN: ${{ github.token }}\n" + tail, tail},
		{"repo dropped", tail, "        run: |\n          set -euo pipefail\n          # check.yml"},
		{"extra env", tail, "          REPO: ${{ github.repository }}\n          CI_ATTEMPTS: 1\n        run: |\n          set -euo pipefail\n          # check.yml"},
		{"guard also on dry run", "      - name: Require a green check run for the tagged commit\n        if: steps.resolve.outputs.dry_run == 'false'\n", "      - name: Require a green check run for the tagged commit\n"},
		{"inline query back", call, `n=$(gh api "repos/$REPO/actions/workflows/check.yml/runs?head_sha=$GITHUB_SHA&event=push&status=success" --jq '.workflow_runs | length'); test "$n" -ge 1`},
	} {
		if !strings.Contains(raw, c.from) {
			t.Fatalf("%s: the mutation target is not in the workflow", c.name)
		}
		if v := ciGuardViolations(strings.Replace(raw, c.from, c.to, 1)); len(v) == 0 {
			t.Errorf("%s: not caught", c.name)
		}
	}
}

func TestBuildIsReproducibleAndStatic(t *testing.T) {
	b := jobs(t, readRepo(t, ".github/workflows/release.yml"))["build"]
	for _, want := range []string{
		"CGO_ENABLED: '0'", "GOTOOLCHAIN: local", "GOFLAGS: -mod=readonly", "-trimpath",
		"GOWORK: 'off'", "-X $pkg.Version=${VERSION#v}", "-X $pkg.Revision=$COMMIT", "-X $pkg.BuildDate=$built",
		"pkg=github.com/wstein/workharbor-nxt/apps/whr/internal/version", "-buildvcs=false", "./cmd/whr", "working-directory: apps/whr",
		"GOCACHE=\"$RUNNER_TEMP/gocache-first\"", "GOCACHE=\"$RUNNER_TEMP/gocache-second\"", "cmp ",
		"release-package.sh", "SOURCE_DATE_EPOCH=", "cache: false", "retention-days: 1", "if-no-files-found: error",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("build lacks %q", want)
		}
	}
}

func matrixTargets(job string) []string {
	var out []string
	for _, line := range strings.Split(job, "\n") {
		if s := strings.TrimSpace(line); strings.HasPrefix(s, "target: ") || strings.HasPrefix(s, "- target: ") {
			out = append(out, strings.TrimPrefix(strings.TrimPrefix(s, "- "), "target: "))
		}
	}
	return out
}

func TestMatrixMatchesThePlatformTable(t *testing.T) {
	raw := readRepo(t, ".github/workflows/release.yml")
	js := jobs(t, raw)
	var want []string
	for _, p := range platforms {
		want = append(want, p.target)
		block := "          - os: " + p.goos + "\n            arch: " + p.goarch + "\n            target: " + p.target + "\n"
		if !strings.Contains(js["build"], block) {
			t.Errorf("build matrix lacks the entry for %s:\n%s", p.target, block)
		}
	}
	if got := matrixTargets(js["build"]); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("build targets %v, want %v", got, want)
	}
	// Every shipped archive is run natively on a runner of its own platform before release.
	if got := matrixTargets(js["smoke"]); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("smoke targets %v, want %v", got, want)
	}
	for tg, runner := range map[string]string{"linux_amd64": "ubuntu-24.04", "linux_arm64": "ubuntu-24.04-arm", "darwin_arm64": "macos-15"} {
		if !strings.Contains(js["smoke"], "target: "+tg+"\n            runner: "+runner+"\n") {
			t.Errorf("smoke of %s does not run natively on %s", tg, runner)
		}
	}
	// The expected archive count of the publish job follows the table.
	if !strings.Contains(js["publish"], "          n="+itoa(len(platforms))+"\n") {
		t.Errorf("publish does not expect %d archives", len(platforms))
	}
	if strings.Contains(raw, "qemu") || strings.Contains(raw, ".zip") || strings.Contains(raw, "windows") {
		t.Error("the matrix ships no emulated, zip or windows target")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// TestMatrixPairsTypeCheckWithThePinnedGo vets whr for every shipped pair; vet type-checks and
// writes no binary.
func TestMatrixPairsTypeCheckWithThePinnedGo(t *testing.T) {
	need(t, "go")
	for _, p := range platforms {
		if p.goos != "linux" && p.goos != "darwin" {
			t.Errorf("%s: whr uses Unix file-system and terminal primitives; other systems are not shipped", p.target)
		}
		wantTarget := p.goos + "_" + p.goarch
		if p.target != wantTarget {
			t.Errorf("target %s, want %s", p.target, wantTarget)
		}
		r := run(t, filepath.Join(repoRoot(t), "apps", "whr"), []string{"CGO_ENABLED=0", "GOOS=" + p.goos, "GOARCH=" + p.goarch, "GOFLAGS=-mod=readonly", "GOWORK=off"},
			"go", "vet", "./cmd/whr")
		if r.code != 0 {
			t.Errorf("%s does not type-check: %s%s", p.target, r.stdout, r.stderr)
		}
	}
}

func TestNotesAndGuideNameEveryPlatform(t *testing.T) {
	notes := readRepo(t, ".github/scripts/release-notes.sh")
	guide := readRepo(t, "docs/releasing.md")
	for _, p := range platforms {
		if !strings.Contains(notes, p.goos+"/"+p.goarch) {
			t.Errorf("release notes do not list %s/%s", p.goos, p.goarch)
		}
		if !strings.Contains(guide, p.goos+"/"+p.goarch) {
			t.Errorf("the releasing guide does not list %s/%s", p.goos, p.goarch)
		}
	}
	for _, system := range []string{"Windows", "darwin/amd64"} {
		if !strings.Contains(guide, system) || !strings.Contains(notes, system) {
			t.Errorf("guide and notes must say that %s is not shipped", system)
		}
	}
	for _, want := range []string{"gh attestation verify", "SHA256SUMS", "WHR_RELEASE_DRAFT", "v0.1.0-alpha.", "dry run", "--verify-tag", "ci-green", "release-verify.sh"} {
		if !strings.Contains(guide, want) {
			t.Errorf("the releasing guide lacks %q", want)
		}
	}
}

func TestMakefileHasNoReleaseTargets(t *testing.T) {
	mk := readRepo(t, "Makefile")
	for _, bad := range []string{"goreleaser", "release:", "dist:", "release-"} {
		if strings.Contains(mk, bad) {
			t.Errorf("Makefile must have no release target: found %q", bad)
		}
	}
}

// step is one workflow step: its text and the body of its run: block (empty without one).
type step struct{ text, run string }

var stepStart = regexp.MustCompile(`(?m)^      - `)

// stepsOf splits a job's text into steps. A run: block runs until the first line indented
// less than its body.
func stepsOf(job string) []step {
	locs := stepStart.FindAllStringIndex(job, -1)
	var out []step
	for n, l := range locs {
		end := len(job)
		if n+1 < len(locs) {
			end = locs[n+1][0]
		}
		text := job[l[0]:end]
		st := step{text: text}
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if !strings.HasPrefix(t, "run:") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(t, "run:"))
			if rest != "|" {
				st.run = rest
				break
			}
			var body []string
			for _, b := range lines[i+1:] {
				if strings.TrimSpace(b) != "" && !strings.HasPrefix(b, "          ") {
					break
				}
				body = append(body, strings.TrimPrefix(b, "          "))
			}
			st.run = strings.TrimSpace(strings.Join(body, "\n"))
			break
		}
		out = append(out, st)
	}
	return out
}

// allowedActions is every action the workflow may use, with its exact pin.
var allowedActions = map[string]bool{
	"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1":                true,
	"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0":                true,
	"actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2":         true,
	"actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2":       true,
	"actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8 # v4.2.2": true,
}

var (
	swallowRe = regexp.MustCompile(`\|\|\s*(true|:)(\s|$|;)`)
	disarmRe  = regexp.MustCompile(`(?m)(^|\s)(exit 0|set \+e)(\s|$)|\|\|\s*echo\b`)
)

// violations lists the rule breaks of a release workflow text.
func violations(raw string) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, strings.TrimSpace(sprintf(format, a...))) }
	jt := jobsOf(raw)
	for name, job := range jt {
		for _, st := range stepsOf(job) {
			if i := strings.Index(st.text, "uses: "); i >= 0 {
				ref := strings.TrimSpace(strings.SplitN(st.text[i+len("uses: "):], "\n", 2)[0])
				if !allowedActions[ref] {
					add("job %s uses an action that is not on the allowed list: %s", name, ref)
				}
				if strings.HasPrefix(ref, "actions/checkout@") && !strings.Contains(st.text, "persist-credentials: false") {
					add("job %s checkout must set persist-credentials: false", name)
				}
				if strings.HasPrefix(ref, "actions/upload-artifact@") && name != "build" {
					add("only build may upload artifacts, job %s does", name)
				}
				if strings.HasPrefix(ref, "actions/download-artifact@") && name != "smoke" && name != "publish" {
					add("only the smoke and publish jobs may download artifacts, job %s does", name)
				}
			}
			run := st.run
			if run == "" {
				continue
			}
			if strings.Contains(run, "\n") && !strings.HasPrefix(run, "set -euo pipefail\n") {
				add("job %s multi-line script must start with set -euo pipefail", name)
			}
			if strings.Contains(run, "${{") {
				add("job %s interpolates an expression inside a run script; pass it through env", name)
			}
			if swallowRe.MatchString(run) {
				add("job %s script ignores a failure with || true or || :", name)
			}
			if disarmRe.MatchString(run) {
				add("job %s script can succeed past a failure (exit 0, set +e, || echo)", name)
			}
			checked := run
			for _, banned := range []string{"curl ", "wget ", "pip ", "npm ", "go install", "apt-get", "brew "} {
				if strings.Contains(checked, banned) {
					add("job %s run script fetches or installs a tool (%s)", name, strings.TrimSpace(banned))
				}
			}
		}
	}
	if regexp.MustCompile(`(?m)^\s*(?:- )?uses:\s*\S+@(?:v\d|main|master)\b`).MatchString(raw) {
		add("a uses: line names a mutable ref")
	}
	sort.Strings(out)
	return out
}

func sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }

// jobsOf is jobs without a testing.T.
func jobsOf(raw string) map[string]string {
	i := strings.Index(raw, "\njobs:\n")
	if i < 0 {
		return nil
	}
	body := raw[i+len("\njobs:\n"):]
	locs := jobHeader.FindAllStringSubmatchIndex(body, -1)
	out := map[string]string{}
	for n, l := range locs {
		end := len(body)
		if n+1 < len(locs) {
			end = locs[n+1][0]
		}
		out[body[l[2]:l[3]]] = body[l[0]:end]
	}
	return out
}

func TestReleaseWorkflowHasNoViolations(t *testing.T) {
	if v := violations(readRepo(t, ".github/workflows/release.yml")); len(v) != 0 {
		t.Errorf("violations:\n%s", strings.Join(v, "\n"))
	}
}

func TestWeakeningTheWorkflowIsCaught(t *testing.T) {
	raw := readRepo(t, ".github/workflows/release.yml")
	checkout := "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1"
	smokeCheckout := "      - uses: " + checkout + "\n        with:\n          persist-credentials: false\n      - uses: actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2\n        with:\n          name: release-${{ matrix.target }}\n"
	smokeRun := `sh .github/scripts/release-smoke.sh "$VERSION" "$dist/whr_${plain}_${TARGET}.tar.gz" "$COMMIT"`
	strict := "          set -euo pipefail\n          test \"$(git rev-parse HEAD)\" = \"$COMMIT\""
	cases := []struct{ name, from, to, want string }{
		{"persist-credentials dropped", smokeCheckout, strings.Replace(smokeCheckout, "        with:\n          persist-credentials: false\n", "", 1), "persist-credentials"},
		{"persist-credentials true", "persist-credentials: false", "persist-credentials: true", "persist-credentials"},
		{"expression in run", `plain=${VERSION#v}`, `plain=${{ needs.verify.outputs.version }}`, "expression"},
		{"swallowed failure", smokeRun, smokeRun + " || true", "|| true"},
		{"swallowed failure with colon", smokeRun, smokeRun + " || :", "|| true"},
		{"failure disarmed with echo", smokeRun, smokeRun + ` || echo ignored`, "exit 0, set +e"},
		{"strict mode off", "set -euo pipefail\n          test \"$(git rev-parse HEAD)\" = \"$COMMIT\"", "set +e\n          test \"$(git rev-parse HEAD)\" = \"$COMMIT\"", "set -euo pipefail"},
		{"unlisted action", "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0", "actions/setup-go@0000000000000000000000000000000000000000 # v7.0.0", "allowed list"},
		{"action pinned to a branch", checkout, "actions/checkout@main", "mutable ref"},
		{"action pinned to a tag", checkout, "actions/checkout@v4", "mutable ref"},
		{"package installation", strict, "          set -euo pipefail\n          sudo apt-get install -y qemu-user\n          test \"$(git rev-parse HEAD)\" = \"$COMMIT\"", "apt-get"},
		{"fetching a tool", strict, "          set -euo pipefail\n          curl -fsSL https://example.test/x | sh\n          test \"$(git rev-parse HEAD)\" = \"$COMMIT\"", "curl"},
		{"no strict mode", strict, "          test \"$(git rev-parse HEAD)\" = \"$COMMIT\"", "set -euo pipefail"},
	}
	for _, c := range cases {
		if !strings.Contains(raw, c.from) {
			t.Fatalf("%s: the mutation target is not in the workflow", c.name)
		}
		mutated := strings.Replace(raw, c.from, c.to, 1)
		v := strings.Join(violations(mutated), "\n")
		if !strings.Contains(v, c.want) {
			t.Errorf("%s: not caught (want %q), violations:\n%s", c.name, c.want, v)
		}
	}
}

func TestPublishGatesOnTheSmokeJobs(t *testing.T) {
	p := jobs(t, readRepo(t, ".github/workflows/release.yml"))["publish"]
	if !strings.Contains(p, "needs: [verify, build, smoke]") {
		t.Error("publish must wait for the native and the emulated smoke jobs")
	}
}

func TestOnlyTheReleaseWorkflowRunsReleaseJobsAndQemu(t *testing.T) {
	rel := readRepo(t, ".github/workflows/release.yml")
	if i := strings.Index(rel, "\non:\n"); i < 0 || !strings.HasPrefix(rel[i:], "\non:\n  push:\n    tags: ['v[0-9]+.[0-9]+.[0-9]+', 'v[0-9]+.[0-9]+.[0-9]+-*']\n  workflow_dispatch:\n    inputs:\n") {
		t.Error("release.yml triggers must be exactly a tag push and workflow_dispatch")
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "release.yml" {
			continue
		}
		raw := strings.ToLower(readRepo(t, ".github/workflows/"+e.Name()))
		for _, bad := range []string{"qemu", "release.yml", "release-smoke", "release-package", "release-verify"} {
			if strings.Contains(raw, bad) {
				t.Errorf("%s must not reference %q: only the release workflow runs release jobs", e.Name(), bad)
			}
		}
	}
}

// scriptViolations lists failure-swallowing constructs of a release script. release-notes.sh may
// end a grep pipeline with || true (no match is not an error there); no other script may.
func scriptViolations(name, text string) []string {
	var out []string
	for n, line := range strings.Split(text, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "#") {
			continue
		}
		if swallowRe.MatchString(code) && !(name == "release-notes.sh" && strings.Contains(code, "grep")) {
			out = append(out, sprintf("%s:%d ignores a failure with || true or || :", name, n+1))
		}
		if name == "release-notes.sh" && code == "exit 0" {
			continue // ends the first-release and no-change branches after their text is printed
		}
		if disarmRe.MatchString(code) {
			out = append(out, sprintf("%s:%d can succeed past a failure (exit 0, set +e, || echo)", name, n+1))
		}
	}
	return out
}

func TestReleaseScriptsSwallowNoFailure(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), ".github", "scripts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw := readRepo(t, ".github/scripts/"+e.Name())
		if v := scriptViolations(e.Name(), raw); len(v) != 0 {
			t.Errorf("violations:\n%s", strings.Join(v, "\n"))
		}
		if !strings.Contains(raw, "set -eu\n") {
			t.Errorf("%s lacks set -eu", e.Name())
		}
	}
	smoke := readRepo(t, ".github/scripts/release-smoke.sh")
	for _, c := range []struct{ from, to string }{
		{`whr --help | grep -q 'Usage:' || fail "whr --help printed no usage"`, `whr --help | grep -q 'Usage:' || true`},
		{`whr --help | grep -q 'Usage:' || fail "whr --help printed no usage"`, `whr --help | grep -q 'Usage:' || :`},
		{"set -eu\n", "set -eu\nset +e\n"},
		{`mkdir "$tmp/home"`, `mkdir "$tmp/home" || echo ignored`},
	} {
		if !strings.Contains(smoke, c.from) {
			t.Fatalf("mutation target missing: %s", c.from)
		}
		if v := scriptViolations("release-smoke.sh", strings.Replace(smoke, c.from, c.to, 1)); len(v) == 0 {
			t.Errorf("mutation not caught: %s", c.to)
		}
	}
}

// linkedModules is the set of modules linked into ./cmd/whr for a GOOS, as module@version.
func linkedModules(t *testing.T, goos string) []string {
	t.Helper()
	r := run(t, filepath.Join(repoRoot(t), "apps", "whr"), []string{"CGO_ENABLED=0", "GOOS=" + goos, "GOARCH=amd64", "GOFLAGS=-mod=readonly", "GOWORK=off"},
		"go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}@{{.Module.Version}}{{end}}", "./cmd/whr")
	if r.code != 0 {
		t.Fatalf("go list: %s%s", r.stdout, r.stderr)
	}
	var out []string
	for _, l := range strings.Fields(r.stdout) {
		if !strings.HasSuffix(l, "@") { // the main module has no version
			out = append(out, l)
		}
	}
	return out
}

// noticeDirs maps each module linked into the shipped binaries to the third_party directory that
// holds its notice, and the archive must ship that directory (release-package.sh, release-smoke.sh).
var noticeDirs = map[string]string{
	"github.com/spf13/cobra": "cobra",
	"github.com/spf13/pflag": "pflag",
	"golang.org/x/sys":       "x-sys",
}

func TestEveryLinkedModuleHasAShippedThirdPartyNotice(t *testing.T) {
	need(t, "go")
	pkg := readRepo(t, ".github/scripts/release-package.sh")
	smk := readRepo(t, ".github/scripts/release-smoke.sh")
	for _, p := range platforms {
		for _, m := range linkedModules(t, p.goos) {
			path := m[:strings.Index(m, "@")]
			dir, ok := noticeDirs[path]
			if !ok {
				t.Errorf("%s is linked into the %s binary but has no notice directory in noticeDirs", m, p.goos)
				continue
			}
			if _, err := os.Stat(filepath.Join(repoRoot(t), "third_party", dir, "LICENSE")); err != nil {
				t.Errorf("%s: %v", m, err)
			}
			if !strings.Contains(pkg, "third_party/"+dir) || !strings.Contains(smk, "third_party/"+dir+"/LICENSE") {
				t.Errorf("the package or smoke script does not ship third_party/%s", dir)
			}
		}
	}
}
