// SPDX-License-Identifier: EUPL-1.2

// Package releaseworkflow tests the release pipeline of docs/releasing.md: the scripts under
// .github/scripts (this file) and the structure of the workflow (workflow_test.go). It has no
// non-test code. Tests that need a tool the host lacks are skipped and say so.
package releaseworkflow

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func script(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), ".github", "scripts", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("missing script: %v", err)
	}
	return p
}

// need skips (never passes) a test whose tools are missing.
func need(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("SKIPPED, NOT PASSED: %s is not available", tool)
		}
	}
}

// gnuTar skips unless tar is GNU tar, which the packaging script requires.
func gnuTar(t *testing.T) {
	t.Helper()
	need(t, "tar", "gzip")
	out, err := exec.Command("tar", "--version").Output()
	if err != nil || !strings.Contains(string(out), "GNU tar") {
		t.Skip("SKIPPED, NOT PASSED: GNU tar is not available")
	}
}

type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, dir string, env []string, name string, args ...string) result {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.test")
	cmd.Env = append(cmd.Env, env...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s %v: %v", name, args, err)
		}
		code = ee.ExitCode()
	}
	return result{code, so.String(), se.String()}
}

func sh(t *testing.T, dir string, env []string, name string, args ...string) result {
	t.Helper()
	need(t, "sh")
	return run(t, dir, env, "sh", append([]string{script(t, name)}, args...)...)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	need(t, "git")
	r := run(t, dir, nil, "git", args...)
	if r.code != 0 {
		t.Fatalf("git %v: %s%s", args, r.stdout, r.stderr)
	}
	return strings.TrimSpace(r.stdout)
}

func commitFile(t *testing.T, dir, name, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(subject), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", subject)
}

// newRepo returns a repository with commit subjects on main, oldest first.
func newRepo(t *testing.T, subjects ...string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	for i, s := range subjects {
		commitFile(t, dir, "f"+string(rune('a'+i)), s)
	}
	return dir
}

func TestVerifyVersion(t *testing.T) {
	good := []string{"v0.1.0", "v1.20.300", "v10.0.0", "v0.1.0-alpha.6", "v0.1.0-alpha.0", "v1.0.0-beta.2", "v1.0.0-rc.11"}
	bad := []string{"", "0.1.0", "v1", "v1.2", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-rc1", "v1.2.3-rc", "v1.2.3-rc.01",
		"v1.2.3-alpha", "v1.2.3-alpha.1.2", "v1.2.3-dryrun", "v1.2.3-foo.1", "v1.2.3-Alpha.1", "v1.2.3+b", "v1.2.3-alpha.1+b",
		"v1.2.3\nx", "v1.2.3-alpha.1\nv", " v1.2.3", "V1.2.3", "v1.2.3.4", "v-1.2.3"}
	for _, v := range good {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "version", v); r.code != 0 {
			t.Errorf("version %q rejected: %s", v, r.stderr)
		}
	}
	for _, v := range bad {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "version", v); r.code == 0 {
			t.Errorf("version %q accepted", v)
		}
	}
}

func TestVerifyPreRelease(t *testing.T) {
	for v, want := range map[string]string{"v0.1.0": "false", "v1.0.0": "false", "v0.1.0-alpha.6": "true", "v1.0.0-rc.1": "true", "v1.0.0-beta.3": "true"} {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "prerelease", v); r.code != 0 || strings.TrimSpace(r.stdout) != want {
			t.Errorf("prerelease %s: %+v, want %s", v, r, want)
		}
	}
	for _, v := range []string{"", "v1", "v1.2.3-dryrun", "v1.2.3-x.1", "v1.2.3\nv1.2.4"} {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "prerelease", v); r.code == 0 {
			t.Errorf("prerelease of %q accepted", v)
		}
	}
}

func TestVerifyDryRunVersion(t *testing.T) {
	for _, v := range []string{"v0.0.0-dryrun", "v0.1.0-dryrun", "v1.2.3-dryrun.1", "v1.2.3-dryrun.abc.2"} {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "dryversion", v); r.code != 0 {
			t.Errorf("dry version %q rejected: %s", v, r.stderr)
		}
	}
	// A dry-run label is never a release version and a release version is never a dry-run label.
	for _, v := range []string{"", "v1.2.3", "v0.1.0-alpha.6", "v1.2.3-rc.1", "1.2.3", "v1.2", "v1.2.3-", "v1.2.3-dry", "v1.2.3-dryrunx", "v1.2.3-dryrun.", "v1.2.3-$(id)", "v1.2.3;x", "v1.2.3 x", "v1.2.3-dryrun\nv1.2.4", "v1.2.3-a/b"} {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "dryversion", v); r.code == 0 {
			t.Errorf("dry version %q accepted", v)
		}
	}
	for _, v := range []string{"v0.0.0-dryrun", "v1.2.3-dryrun.1"} {
		if r := sh(t, repoRoot(t), nil, "release-verify.sh", "version", v); r.code == 0 {
			t.Errorf("release version check accepted the dry-run label %q", v)
		}
	}
}

func TestVerifyTag(t *testing.T) {
	dir := newRepo(t, "one", "two")
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "tag", "-a", "-m", "release", "v0.1.0")
	git(t, dir, "tag", "v0.2.0") // lightweight
	git(t, dir, "tag", "-a", "-m", "x", "v0.3.0-rc1")
	git(t, dir, "tag", "-a", "-m", "x", "v0.1.0-alpha.6")
	git(t, dir, "tag", "-a", "-m", "x", "v0.1.0-dryrun")
	git(t, dir, "tag", "v0.1.0-alpha.7") // lightweight pre-release

	if r := sh(t, dir, nil, "release-verify.sh", "tag", "v0.1.0", "main"); r.code != 0 || strings.TrimSpace(r.stdout) != head {
		t.Errorf("annotated tag on main must pass and print the commit: %+v", r)
	}
	if r := sh(t, dir, nil, "release-verify.sh", "tag", "v0.1.0-alpha.6", "main"); r.code != 0 || strings.TrimSpace(r.stdout) != head {
		t.Errorf("annotated pre-release tag on main must pass and print the commit: %+v", r)
	}
	if r := sh(t, dir, nil, "release-verify.sh", "tag", "v0.2.0", "main"); r.code == 0 || !strings.Contains(r.stderr, "annotated") {
		t.Errorf("lightweight tag must fail naming 'annotated': %+v", r)
	}
	if r := sh(t, dir, nil, "release-verify.sh", "tag", "v0.1.0-alpha.7", "main"); r.code == 0 || !strings.Contains(r.stderr, "annotated") {
		t.Errorf("lightweight pre-release tag must fail naming 'annotated': %+v", r)
	}
	for _, bad := range []string{"v0.3.0-rc1", "v0.1.0-dryrun", "v9.9.9"} {
		if r := sh(t, dir, nil, "release-verify.sh", "tag", bad, "main"); r.code == 0 {
			t.Errorf("tag %s must fail: %+v", bad, r)
		}
	}

	// A tag on a commit that is not reachable from main.
	git(t, dir, "checkout", "-q", "-b", "side")
	commitFile(t, dir, "g", "side")
	git(t, dir, "tag", "-a", "-m", "side", "v0.4.0")
	if r := sh(t, dir, nil, "release-verify.sh", "tag", "v0.4.0", "main"); r.code == 0 || !strings.Contains(r.stderr, "main") {
		t.Errorf("tag off main must fail naming main: %+v", r)
	}
	if r := sh(t, dir, nil, "release-verify.sh", "tag", "v0.1.0"); r.code != 2 {
		t.Errorf("missing argument must exit 2: %+v", r)
	}
	if r := sh(t, dir, nil, "release-verify.sh", "bogus"); r.code != 2 {
		t.Errorf("unknown subcommand must exit 2: %+v", r)
	}
}

// fakeBinary writes a shell script that answers like whr for the version commands, so the scripts
// are tested without building whr: the real binary is exercised by the release run itself.
func fakeBinary(t *testing.T, plain, revision string) string {
	t.Helper()
	need(t, "sh")
	out := filepath.Join(t.TempDir(), "whr")
	body := `#!/bin/sh
case "$*" in
"version" | "--version") printf 'whr ` + plain + `\nrevision: ` + revision + `\nbuild date: x\nplatform: x/x\ncontracts: x=1\n' ;;
"version --json") printf '{"schema_version":1,"data":{"version":"` + plain + `","revision":"` + revision + `"}}\n' ;;
"--help") printf 'Usage:\n  whr [command]\n' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(out, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return out
}

func hostTarget() string { return runtime.GOOS + "_" + runtime.GOARCH }

const commitA = "b6ee4d79b6ee4d79b6ee4d79b6ee4d79b6ee4d79"

// pack packages binary as version/target into out, run in the repository root.
func pack(t *testing.T, version, target, binary, out string) result {
	t.Helper()
	return sh(t, repoRoot(t), []string{"SOURCE_DATE_EPOCH=1700000000"}, "release-package.sh", version, target, binary, out)
}

// listing returns the sorted member names of a tar.gz.
func listing(t *testing.T, archive string) []string {
	t.Helper()
	r := run(t, "", nil, "tar", "-tzf", archive)
	if r.code != 0 {
		t.Fatalf("list %s: %s", archive, r.stderr)
	}
	return strings.Fields(r.stdout)
}

// members is the expected member list of an archive of the given name: the top directory, the
// binary, the documents and the third-party notices of the modules linked into whr.
func members(t *testing.T, name string) []string {
	t.Helper()
	d := name + "/"
	want := []string{d, d + "LICENSE", d + "README.md", d + "whr", d + "third_party/", d + "third_party/README.md"}
	root := repoRoot(t)
	for _, dir := range noticeDirs {
		base := filepath.Join(root, "third_party", dir)
		err := filepath.WalkDir(base, func(p string, e os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if e.IsDir() {
				rel += "/"
			}
			want = append(want, d+rel)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(want)
	return want
}

func TestPackageTarLayoutAndDeterminism(t *testing.T) {
	gnuTar(t)
	bin := fakeBinary(t, "0.1.0-alpha.6", commitA)
	out1, out2 := t.TempDir(), t.TempDir()
	if r := pack(t, "v0.1.0-alpha.6", "linux_arm64", bin, out1); r.code != 0 {
		t.Fatalf("pack: %+v", r)
	}
	if r := pack(t, "v0.1.0-alpha.6", "linux_arm64", bin, out2); r.code != 0 {
		t.Fatalf("pack: %+v", r)
	}
	name := "whr_0.1.0-alpha.6_linux_arm64.tar.gz"
	a, err := os.ReadFile(filepath.Join(out1, name))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out2, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two packagings of the same inputs differ")
	}
	want := members(t, "whr_0.1.0-alpha.6_linux_arm64")
	got := listing(t, filepath.Join(out1, name))
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("members %v, want %v", got, want)
	}
	if v := run(t, out1, nil, "tar", "-tvzf", name); !strings.Contains(v.stdout, "-rwxr-xr-x 0/0") || !strings.Contains(v.stdout, "2023-11-14") || strings.Contains(v.stdout, "-rw-rw") || strings.Contains(v.stdout, "drwxrwx") {
		t.Errorf("mode, owner or mtime not normalised:\n%s", v.stdout)
	}
	sum, err := os.ReadFile(filepath.Join(out1, name+".sha256"))
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(sum)), "  "+name) {
		t.Errorf("sidecar %q: %v", sum, err)
	}
	// A stable release and a dry-run label package too.
	for _, v := range []string{"v0.1.0", "v0.0.0-dryrun"} {
		if r := pack(t, v, "darwin_arm64", bin, t.TempDir()); r.code != 0 {
			t.Errorf("pack %s: %+v", v, r)
		}
	}
}

func TestPackageRefusals(t *testing.T) {
	gnuTar(t)
	bin := fakeBinary(t, "1.0.0", commitA)
	out := t.TempDir()
	cases := []struct {
		name string
		env  []string
		args []string
	}{
		{"bad version", nil, []string{"1.0.0", "linux_amd64", bin, out}},
		{"unknown pre-release", nil, []string{"v1.0.0-foo.1", "linux_amd64", bin, out}},
		{"newline in version", nil, []string{"v1.0.0\nv", "linux_amd64", bin, out}},
		{"bad target", nil, []string{"v1.0.0", "linux/amd64", bin, out}},
		{"windows is not shipped", nil, []string{"v1.0.0", "windows_amd64", bin, out}},
		{"freebsd is not shipped", nil, []string{"v1.0.0", "freebsd_amd64", bin, out}},
		{"missing binary", nil, []string{"v1.0.0", "linux_amd64", filepath.Join(out, "nope"), out}},
		{"no epoch", []string{"SOURCE_DATE_EPOCH="}, []string{"v1.0.0", "linux_amd64", bin, out}},
		{"epoch not digits", []string{"SOURCE_DATE_EPOCH=yesterday"}, []string{"v1.0.0", "linux_amd64", bin, out}},
	}
	for _, c := range cases {
		env := c.env
		if env == nil {
			env = []string{"SOURCE_DATE_EPOCH=1"}
		}
		if r := sh(t, repoRoot(t), env, "release-package.sh", c.args...); r.code == 0 {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if r := sh(t, repoRoot(t), nil, "release-package.sh", "v1.0.0"); r.code != 2 {
		t.Errorf("usage error must exit 2: %+v", r)
	}
	if r := sh(t, t.TempDir(), []string{"SOURCE_DATE_EPOCH=1"}, "release-package.sh", "v1.0.0", "linux_amd64", bin, out); r.code == 0 || !strings.Contains(r.stderr, "not found") {
		t.Errorf("a tree without LICENSE must be refused: %+v", r)
	}
}

func smoke(t *testing.T, env []string, args ...string) result {
	t.Helper()
	return sh(t, repoRoot(t), env, "release-smoke.sh", args...)
}

func packedHost(t *testing.T, version, plain string) (archive, out string) {
	t.Helper()
	gnuTar(t)
	out = t.TempDir()
	if r := pack(t, version, hostTarget(), fakeBinary(t, plain, commitA), out); r.code != 0 {
		t.Fatalf("pack: %+v", r)
	}
	return filepath.Join(out, "whr_"+plain+"_"+hostTarget()+".tar.gz"), out
}

func TestSmokeAcceptsAGoodArchiveAndChecksTheStamp(t *testing.T) {
	archive, _ := packedHost(t, "v0.1.0-alpha.6", "0.1.0-alpha.6")
	if r := smoke(t, nil, "v0.1.0-alpha.6", archive); r.code != 0 || !strings.Contains(r.stdout, "ok") {
		t.Fatalf("smoke of a good archive failed: %+v", r)
	}
	if r := smoke(t, nil, "v0.1.0-alpha.6", archive, commitA); r.code != 0 {
		t.Errorf("smoke with the stamped commit failed: %+v", r)
	}
	if r := smoke(t, nil, "v0.1.0-alpha.7", archive); r.code == 0 || !strings.Contains(r.stderr, "whr version printed") {
		t.Errorf("a wrong stamped version must fail: %+v", r)
	}
	if r := smoke(t, nil, "v0.1.0", archive); r.code == 0 {
		t.Errorf("a pre-release archive must not pass as the stable version: %+v", r)
	}
	if r := smoke(t, nil, "v0.1.0-alpha.6", archive, otherSHA); r.code == 0 || !strings.Contains(r.stderr, "revision") {
		t.Errorf("a wrong stamped revision must fail: %+v", r)
	}
	if r := smoke(t, nil, "v0.1.0-alpha.6"); r.code != 2 {
		t.Errorf("usage error must exit 2: %+v", r)
	}
	// A damaged archive fails its checksum.
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, append(data, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := smoke(t, nil, "v0.1.0-alpha.6", archive); r.code == 0 || !strings.Contains(r.stderr, "checksum") {
		t.Errorf("a damaged archive must fail its checksum: %+v", r)
	}
}

func TestSmokeRejectsAnExtraMemberAndAMissingSidecar(t *testing.T) {
	archive, out := packedHost(t, "v0.1.0", "0.1.0")
	name := "whr_0.1.0_" + hostTarget()
	if err := os.Remove(archive + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if r := smoke(t, nil, "v0.1.0", archive); r.code == 0 || !strings.Contains(r.stderr, "sidecar") {
		t.Errorf("a missing sidecar must fail: %+v", r)
	}
	// Rebuild the archive with an extra file and a valid sidecar.
	work := t.TempDir()
	if r := run(t, work, nil, "tar", "-xzf", archive); r.code != 0 {
		t.Fatal(r.stderr)
	}
	if err := os.WriteFile(filepath.Join(work, name, "extra"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(t, work, nil, "tar", "-czf", archive, name); r.code != 0 {
		t.Fatal(r.stderr)
	}
	if r := run(t, out, nil, "sh", "-c", "sha256sum "+name+".tar.gz > "+name+".tar.gz.sha256"); r.code != 0 {
		t.Skipf("SKIPPED, NOT PASSED: sha256sum unavailable: %s", r.stderr)
	}
	if r := smoke(t, nil, "v0.1.0", archive); r.code == 0 || !strings.Contains(r.stderr, "unexpected archive members") {
		t.Errorf("an extra member must fail: %+v", r)
	}
}

func TestSmokeStartsNothingElse(t *testing.T) {
	archive, _ := packedHost(t, "v0.1.0", "0.1.0")
	// Failing launchers stand in for a runner without Docker or a sandbox.
	shims := t.TempDir()
	for _, name := range []string{"docker", "git", "bwrap", "sandbox-exec"} {
		body := "#!/bin/sh\necho \"" + name + ": must not run\" >&2\nexit 1\n"
		if err := os.WriteFile(filepath.Join(shims, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := "PATH=" + shims + string(os.PathListSeparator) + os.Getenv("PATH")
	if r := smoke(t, []string{path}, "v0.1.0", archive); r.code != 0 {
		t.Errorf("the smoke must not depend on Docker, Git or a sandbox: %+v", r)
	}
	for _, line := range strings.Split(readRepo(t, ".github/scripts/release-smoke.sh"), "\n") {
		if code := strings.TrimSpace(line); !strings.HasPrefix(code, "#") && (strings.Contains(code, "docker") || strings.Contains(code, "git ") || strings.Contains(code, "whr init") || strings.Contains(code, "whr workspace")) {
			t.Errorf("the smoke must run only the version and help commands: %s", code)
		}
	}
}

func TestNotes(t *testing.T) {
	dir := newRepo(t, "feat: first", "fix: second")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0-alpha.5")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0-alpha.4", "HEAD~1") // an older tag: not the baseline
	commitFile(t, dir, "n", "feat(cli): third")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.5-dryrun") // a dry-run label: never the baseline
	commitFile(t, dir, "m", "fix: fourth")
	commitFile(t, dir, "k", "docs: fifth")
	commitFile(t, dir, "j", "chore: sixth")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0-alpha.6")
	head := git(t, dir, "rev-parse", "HEAD")

	r := sh(t, dir, nil, "release-notes.sh", "v0.1.0-alpha.6", head, "1.27.2")
	if r.code != 0 {
		t.Fatalf("notes failed: %+v", r)
	}
	for _, want := range []string{"WorkHarbor v0.1.0-alpha.6", "Commit: " + head, "Go 1.27.2", "stamped as 0.1.0-alpha.6", "whr_0.1.0-alpha.6_<os>_<arch>",
		"linux/amd64, linux/arm64, darwin/arm64", "Windows, the BSDs and darwin/amd64 are not shipped",
		"sha256sum --check --ignore-missing SHA256SUMS", "gh attestation verify ARCHIVE --repo wstein/workharbor", "not signed with a certificate or notarized",
		"`whr version`, which prints `whr 0.1.0-alpha.6`",
		"Changes since v0.1.0-alpha.5", "### Features (1)\n\n- feat(cli): third (",
		"<details><summary>Fixes (1)</summary>\n\n- fix: fourth (", ")\n\n</details>\n",
		"Not listed here: 1 documentation and 1 other commits (tests, style, CI, chores). Full comparison: https://github.com/wstein/workharbor/compare/v0.1.0-alpha.5...v0.1.0-alpha.6\n"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("notes lack %q:\n%s", want, r.stdout)
		}
	}
	for _, unwanted := range []string{"- fix: second", "- docs: fifth", "- chore: sixth", "### Documentation", "### Other\n", "### Breaking", "### Other changes", "First release", "v0.1.5-dryrun", "Changes since v0.1.0-alpha.4"} {
		if strings.Contains(r.stdout, unwanted) {
			t.Errorf("notes must not contain %q:\n%s", unwanted, r.stdout)
		}
	}
	lower := strings.ToLower(r.stdout)
	for _, word := range []string{"exciting", "amazing", "powerful", "blazing", "!"} {
		if strings.Contains(lower, word) {
			t.Errorf("notes contain marketing wording %q", word)
		}
	}

	first := newRepo(t, "feat: only")
	git(t, first, "tag", "-a", "-m", "r", "v0.1.0-alpha.1")
	r = sh(t, first, nil, "release-notes.sh", "v0.1.0-alpha.1", git(t, first, "rev-parse", "HEAD"), "1.27.2")
	if r.code != 0 || !strings.Contains(r.stdout, "First release.") || strings.Contains(r.stdout, "- feat: only") {
		t.Errorf("first release notes wrong: %+v", r)
	}
	if r := sh(t, first, nil, "release-notes.sh", "v0.1.0"); r.code != 2 {
		t.Errorf("usage error must exit 2: %+v", r)
	}
}

func TestNotesCountDocsAndOtherCommitsInsteadOfListingThem(t *testing.T) {
	dir := newRepo(t, "feat: a")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0")
	for i, s := range []string{"docs: d1", "docs(x): d2", "test: t1", "style: s1", "ci: c1", "chore: h1", "build: b1", "fix: f1", "fix(y): f2", "feat: f3", "perf: p1", "refactor(z): r1", "wip not conventional"} {
		commitFile(t, dir, "g"+string(rune('a'+i)), s)
	}
	git(t, dir, "tag", "-a", "-m", "r", "v0.2.0")
	r := sh(t, dir, nil, "release-notes.sh", "v0.2.0", git(t, dir, "rev-parse", "HEAD"), "1.27.2")
	if r.code != 0 {
		t.Fatalf("notes failed: %+v", r)
	}
	for _, want := range []string{"### Features (1)\n\n- feat: f3 (", "<details><summary>Fixes (2)</summary>\n\n- fix(y): f2 (", "- fix: f1 (",
		"### Other changes (2)\n\n- refactor(z): r1 (", "- perf: p1 (",
		"Not listed here: 2 documentation and 6 other commits (tests, style, CI, chores). Full comparison: https://github.com/wstein/workharbor/compare/v0.1.0...v0.2.0\n"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("notes lack %q:\n%s", want, r.stdout)
		}
	}
	for _, unwanted := range []string{"docs: d1", "docs(x): d2", "test: t1", "style: s1", "ci: c1", "chore: h1", "build: b1", "wip not", "### Breaking", "- feat: a"} {
		if strings.Contains(r.stdout, unwanted) {
			t.Errorf("notes must not list %q:\n%s", unwanted, r.stdout)
		}
	}
}

func notesFor(t *testing.T, subjects ...string) string {
	t.Helper()
	dir := newRepo(t, "feat: a")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0")
	for i, s := range subjects {
		commitFile(t, dir, "g"+string(rune('a'+i)), s)
	}
	r := sh(t, dir, nil, "release-notes.sh", "v0.2.0", git(t, dir, "rev-parse", "HEAD"), "1.27.2")
	if r.code != 0 {
		t.Fatalf("notes failed: %+v", r)
	}
	return r.stdout
}

func TestNotesListEachCommitOnceWhenTheDescriptionHasParentheses(t *testing.T) {
	subjects := []string{"fix(a)!: x (y): z", "docs(b)!: rename (c): z", "perf(x)!: y (z): w", "fix(q): plain (r): s", "docs(d): plain (e): f", "chore: other"}
	out := notesFor(t, subjects...)
	for _, s := range subjects[:3] {
		if n := strings.Count(out, "- "+s+" ("); n != 1 {
			t.Errorf("%q must be listed once, got %d:\n%s", s, n, out)
		}
	}
	if !strings.Contains(out, "### Breaking changes (3)") || !strings.Contains(out, "Fixes (1)") ||
		!strings.Contains(out, "Not listed here: 1 documentation and 1 other commits") {
		t.Errorf("counts must add up to the six commits:\n%s", out)
	}
}

func TestNotesListBreakingChangesOfAnyType(t *testing.T) {
	subjects := []string{"deps!: bump api", "security!: x", "Feat!: capitalized", "Fix(a)!: capitalized"}
	out := notesFor(t, append(subjects, "feat!: lower")...)
	for _, s := range subjects {
		if n := strings.Count(out, "- "+s+" ("); n != 1 {
			t.Errorf("%q must be listed once, got %d:\n%s", s, n, out)
		}
	}
	if !strings.Contains(out, "### Breaking changes (4)") || !strings.Contains(out, "### Features (1)\n\n- feat!: lower (") || strings.Count(out, "feat!: lower") != 1 {
		t.Errorf("feat! stays under Features only:\n%s", out)
	}
}

func TestNotesEscapeHTMLInSubjects(t *testing.T) {
	out := notesFor(t, "fix: see </details> <b>x</b> & more")
	if strings.Count(out, "</details>") != 1 || strings.Contains(out, "<b>") || !strings.Contains(out, "see &lt;/details> &lt;b>x&lt;/b> &amp; more (") {
		t.Errorf("a subject must not close or inject HTML:\n%s", out)
	}
}

func TestNotesListEveryBreakingChange(t *testing.T) {
	dir := newRepo(t, "feat: a")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0")
	for i, s := range []string{"docs!: drop the old guide", "chore(deps)!: raise the floor", "feat(cli)!: rename a flag", "fix!: change an exit code"} {
		commitFile(t, dir, "g"+string(rune('a'+i)), s)
	}
	git(t, dir, "tag", "-a", "-m", "r", "v0.2.0")
	r := sh(t, dir, nil, "release-notes.sh", "v0.2.0", git(t, dir, "rev-parse", "HEAD"), "1.27.2")
	if r.code != 0 {
		t.Fatalf("notes failed: %+v", r)
	}
	for _, want := range []string{"### Features (1)\n\n- feat(cli)!: rename a flag (", "### Breaking changes (3)\n\n",
		"- docs!: drop the old guide (", "- chore(deps)!: raise the floor (", "- fix!: change an exit code (",
		"Not listed here: 0 documentation and 0 other commits"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("notes lack %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "<details>") || strings.Contains(r.stdout, "Other changes") {
		t.Errorf("no fixes block or other changes section without those commits:\n%s", r.stdout)
	}
}

func TestNotesWithoutFixesOrFeaturesPrintNoEmptyBlock(t *testing.T) {
	dir := newRepo(t, "feat: a")
	git(t, dir, "tag", "-a", "-m", "r", "v0.1.0")
	commitFile(t, dir, "g", "docs: only docs")
	r := sh(t, dir, nil, "release-notes.sh", "v0.1.1", git(t, dir, "rev-parse", "HEAD"), "1.27.2")
	if r.code != 0 || strings.Contains(r.stdout, "<details>") || strings.Contains(r.stdout, "### ") || strings.Contains(r.stdout, "(0)") ||
		!strings.Contains(r.stdout, "Not listed here: 1 documentation and 0 other commits") {
		t.Errorf("an empty group must print nothing: %+v", r)
	}
}

// ciExec runs a command and returns its stdout and stderr together; err is non-nil on a non-zero exit.
func ciExec(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	r := run(t, dir, env, name, args...)
	if r.code != 0 {
		return r.stdout + r.stderr, fmt.Errorf("exit status %d", r.code)
	}
	return r.stdout + r.stderr, nil
}

const ciSHA = "b6ee4d79b6ee4d79b6ee4d79b6ee4d79b6ee4d79"
const otherSHA = "0123456789012345678901234567890123456789"

type ciRun struct{ sha, conclusion, event string }

func ciRuns(rs ...ciRun) string {
	var parts []string
	for _, r := range rs {
		parts = append(parts, `{"head_sha":"`+r.sha+`","conclusion":"`+r.conclusion+`","event":"`+r.event+`"}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func ciRunsN(n int, r ciRun) []ciRun {
	out := make([]ciRun, n)
	for i := range out {
		out[i] = r
	}
	return out
}

// ciCase describes one fake API situation. headModes and listModes are consumed one entry
// per head_sha query or per list query ("ok", "empty" for a zero-result answer, "fail" for a
// gh error); once used up the fake answers "ok". ignoreHead models a server that ignores
// the head_sha, event and status filters and returns unfiltered runs.
type ciCase struct {
	name                 string
	runs                 string
	headModes, listModes []string
	ignoreHead           bool
	ok                   bool
	heads, lists, sleeps int
	msg                  string
}

func fakeGH(t *testing.T, c ciCase, extraEnv ...string) (env []string, files func(string) []string) {
	t.Helper()
	need(t, "jq", "sed", "tr")
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("runs.json", c.runs)
	write("head", strings.Join(c.headModes, "\n")+"\n")
	write("list", strings.Join(c.listModes, "\n")+"\n")
	ignore := "0"
	if c.ignoreHead {
		ignore = "1"
	}
	write("ignorehead", ignore)
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// The fake models the Actions API: it checks the path, applies the head_sha, event and
	// status filters of the query to the runs, pages them and answers the caller's --jq
	// filter with the real jq over {"total_count", "workflow_runs"}.
	fake := `#!/bin/sh
d=` + dir + `
[ "$1" = api ] && [ "$3" = --jq ] || exit 9
url=$2
filter=$4
echo "$url" >> "$d/calls"
path=${url%%\?*}
query=${url#*\?}
if [ "$path" != "repos/o/r/actions/workflows/check.yml/runs" ]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
param() { printf '%s' "$query" | tr '&' '\n' | sed -n "s/^$1=//p"; }
case $url in
*head_sha=*) q=head ;;
*) q=list ;;
esac
mode=$(sed -n 1p "$d/$q")
sed 1d "$d/$q" > "$d/$q.next" && mv "$d/$q.next" "$d/$q"
case ${mode:-ok} in
fail) echo "gh: HTTP 502 (Bad Gateway)" >&2; exit 1 ;;
empty) body='{"total_count":0,"workflow_runs":[]}' ;;
*)
	body=$(jq -c --arg head "$(param head_sha)" --arg event "$(param event)" --arg status "$(param status)" \
		--argjson per "$(param per_page | grep . || echo 30)" --argjson page "$(param page | grep . || echo 1)" \
		--arg ignore "$(cat "$d/ignorehead")" '
		[.[] | select(($ignore == "1" or $head == "" or .head_sha == $head)
			and ($ignore == "1" or $event == "" or .event == $event)
			and ($ignore == "1" or $status == "" or ($status == "success" and .conclusion == "success")))] as $m
		| {total_count: ($m | length), workflow_runs: $m[($page - 1) * $per : $page * $per]}' "$d/runs.json")
	;;
esac
printf '%s' "$body" | jq -r "$filter"
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	// A fake sleep that records its argument keeps the tests fast and shows the pause.
	if err := os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\necho \"$1\" >> '"+dir+"/sleeps'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	files = func(name string) []string {
		raw, _ := os.ReadFile(filepath.Join(dir, name))
		return strings.Fields(string(raw))
	}
	env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "REPO=o/r"}, extraEnv...)
	return env, files
}

func ciGreen(t *testing.T, scriptPath string, env []string) (string, error) {
	t.Helper()
	return ciExec(t, repoRoot(t), env, "sh", scriptPath, "ci-green", ciSHA)
}

func ciCases() []ciCase {
	hit := ciRuns(ciRun{ciSHA, "success", "push"})
	other := ciRuns(ciRun{otherSHA, "success", "push"}, ciRun{otherSHA, "success", "push"})
	five := func(m string) []string { return []string{m, m, m, m, m} }
	withHit := func(n int) string {
		rs := ciRunsN(n, ciRun{otherSHA, "success", "push"})
		return ciRuns(append(rs, ciRun{ciSHA, "success", "push"})...)
	}
	const none = "no successful CI run on push for " + ciSHA
	const failing = "kept failing"
	return []ciCase{
		{name: "first answer is enough", runs: hit, ok: true, heads: 1},
		{name: "0,0,2 succeeds on the third", runs: hit, headModes: []string{"empty", "empty", "ok"}, ok: true, heads: 3, sleeps: 2},
		{name: "five empty answers use the fallback", runs: hit, headModes: five("empty"), ok: true, heads: 5, lists: 1, sleeps: 4},
		{name: "five gh errors use the fallback", runs: hit, headModes: five("fail"), ok: true, heads: 5, lists: 1, sleeps: 4},
		{name: "nothing found", runs: other, ok: false, heads: 5, lists: 1, sleeps: 4, msg: none},
		{name: "no runs at all", runs: "[]", ok: false, heads: 5, lists: 1, sleeps: 4, msg: none},
		{name: "server ignores head_sha, returns other commits", runs: other, ignoreHead: true, ok: false, heads: 5, lists: 1, sleeps: 4, msg: none},
		{name: "run of the commit did not succeed", runs: ciRuns(ciRun{ciSHA, "failure", "push"}), ignoreHead: true, ok: false, heads: 5, lists: 1, sleeps: 4, msg: none},
		{name: "run of the commit is not a push run", runs: ciRuns(ciRun{ciSHA, "success", "pull_request"}), ignoreHead: true, ok: false, heads: 5, lists: 1, sleeps: 4, msg: none},
		{name: "every call fails", runs: hit, headModes: five("fail"), listModes: five("fail"), ok: false, heads: 5, lists: 5, sleeps: 8, msg: failing},
		{name: "the API fails once the head query is empty", runs: hit, headModes: five("empty"), listModes: five("fail"), ok: false, heads: 5, lists: 5, sleeps: 8, msg: none},
		{name: "a failed list page is retried", runs: hit, headModes: five("empty"), listModes: []string{"fail", "ok"}, ok: true, heads: 5, lists: 2, sleeps: 5},
		{name: "fallback reads status=success pages only", runs: ciRuns(append(ciRunsN(450, ciRun{otherSHA, "failure", "push"}), ciRun{ciSHA, "success", "push"})...), headModes: five("empty"), ok: true, heads: 5, lists: 1, sleeps: 4},
		{name: "commit within the newest 500 is found on page 5", runs: withHit(450), headModes: five("empty"), ok: true, heads: 5, lists: 5, sleeps: 4},
		{name: "commit beyond the newest 500 is not found", runs: withHit(550), headModes: five("empty"), ok: false, heads: 5, lists: 5, sleeps: 4, msg: none},
	}
}

// ciProblems runs one case against the script at path (CI_SLEEP unset) and lists what deviates.
func ciProblems(t *testing.T, path string, c ciCase) []string {
	t.Helper()
	env, files := fakeGH(t, c)
	out, err := ciGreen(t, path, env)
	var bad []string
	if (err == nil) != c.ok {
		bad = append(bad, "exit: "+fmt.Sprint(err)+" "+out)
	}
	var heads, lists int
	for _, u := range files("calls") {
		if strings.Contains(u, "head_sha=") {
			heads++
			if !strings.Contains(u, "head_sha="+ciSHA+"&") {
				bad = append(bad, "head query for another commit: "+u)
			}
		} else {
			lists++
		}
		if !strings.Contains(u, "event=push") || !strings.Contains(u, "status=success") || !strings.HasPrefix(u, "repos/o/r/actions/workflows/check.yml/runs?") {
			bad = append(bad, "query lacks event=push, status=success or the check.yml path: "+u)
		}
	}
	if heads != c.heads || lists != c.lists {
		bad = append(bad, fmt.Sprintf("%d head and %d list queries, want %d and %d", heads, lists, c.heads, c.lists))
	}
	sleeps := files("sleeps")
	if len(sleeps) != c.sleeps {
		bad = append(bad, fmt.Sprintf("%d pauses, want %d", len(sleeps), c.sleeps))
	}
	for _, sl := range sleeps {
		if sl != "2" {
			bad = append(bad, "pause of "+sl+" seconds, want 2")
		}
	}
	if c.msg != "" && !strings.Contains(out, c.msg) {
		bad = append(bad, "message lacks "+c.msg+": "+out)
	}
	if !c.ok && c.msg == none(c) && strings.Contains(out, "5 attempts") == false {
		bad = append(bad, "message does not say how many attempts were made: "+out)
	}
	return bad
}

func none(c ciCase) string { return "no successful CI run on push for " + ciSHA }

func TestCIGreen(t *testing.T) {
	for _, c := range ciCases() {
		for _, bad := range ciProblems(t, script(t, "release-verify.sh"), c) {
			t.Errorf("%s: %s", c.name, bad)
		}
	}
}

func TestCIGreenRunsUnderDashAndBash(t *testing.T) {
	for _, shell := range []string{"dash", "bash"} {
		if _, err := exec.LookPath(shell); err != nil {
			t.Logf("SKIPPED, NOT PASSED: %s is not installed", shell)
			continue
		}
		env, _ := fakeGH(t, ciCases()[1])
		if out, err := ciExec(t, repoRoot(t), env, shell, script(t, "release-verify.sh"), "ci-green", ciSHA); err != nil {
			t.Errorf("%s: %v\n%s", shell, err, out)
		}
	}
}

func TestCIGreenRejectsABadCommit(t *testing.T) {
	env, _ := fakeGH(t, ciCases()[0])
	for _, bad := range []string{"", "main", ciSHA[:39], ciSHA + "0", "B6EE4D79B6EE4D79B6EE4D79B6EE4D79B6EE4D79"} {
		if _, err := ciExec(t, repoRoot(t), env, "sh", script(t, "release-verify.sh"), "ci-green", bad); err == nil {
			t.Errorf("commit %q accepted", bad)
		}
	}
}

// Each mutant weakens the script once; some case of TestCIGreen must deviate.
func TestCIGreenMutantsAreCaught(t *testing.T) {
	raw, err := os.ReadFile(script(t, "release-verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	headQ := `"$runs?head_sha=$sha&event=push&status=success"`
	listQ := `"$runs?event=push&status=success&per_page=100&page=$p"`
	mutants := []struct{ name, from, to string }{
		{"no retry", "CI_ATTEMPTS:-5", "CI_ATTEMPTS:-1"},
		{"retry count 0", "CI_ATTEMPTS:-5", "CI_ATTEMPTS:-0"},
		{"no fallback", "pages=${CI_PAGES:-5}", "pages=0"},
		{"fallback uncapped", "pages=${CI_PAGES:-5}", "pages=${CI_PAGES:-50}"},
		{"head filter without head_sha", `select(.head_sha == "'$sha'" and .conclusion == "success" and .event == "push")] | length'`, `select(.conclusion == "success" and .event == "push")] | length'`},
		{"head filter without conclusion", `select(.head_sha == "'$sha'" and .conclusion == "success" and .event == "push")] | length'`, `select(.head_sha == "'$sha'" and .event == "push")] | length'`},
		{"head filter conclusion inverted", `select(.head_sha == "'$sha'" and .conclusion == "success" and .event == "push")] | length'`, `select(.head_sha == "'$sha'" and .conclusion != "success" and .event == "push")] | length'`},
		{"head filter without event", `select(.head_sha == "'$sha'" and .conclusion == "success" and .event == "push")] | length'`, `select(.head_sha == "'$sha'" and .conclusion == "success")] | length'`},
		{"head count is total_count + 1", `hf='[.workflow_runs[] | select(.head_sha == "'$sha'" and .conclusion == "success" and .event == "push")] | length'`, `hf='.total_count + 1'`},
		{"fallback filter without head_sha", `map(select(.head_sha == "'$sha'" and .conclusion`, `map(select(.conclusion`},
		{"fallback filter conclusion inverted", `map(select(.head_sha == "'$sha'" and .conclusion == "success"`, `map(select(.head_sha == "'$sha'" and .conclusion != "success"`},
		{"fallback filter without conclusion", `and .conclusion == "success" and .event == "push")) | length)"'`, `and .event == "push")) | length)"'`},
		{"fallback filter without event", `and .conclusion == "success" and .event == "push")) | length)"'`, `and .conclusion == "success")) | length)"'`},
		{"head query without event=push", headQ, `"$runs?head_sha=$sha&status=success"`},
		{"head query without status=success", headQ, `"$runs?head_sha=$sha&event=push"`},
		{"head query without head_sha", headQ, `"$runs?event=push&status=success"`},
		{"fallback query without event=push", listQ, `"$runs?status=success&per_page=100&page=$p"`},
		{"fallback query without status=success", listQ, `"$runs?event=push&per_page=100&page=$p"`},
		{"other workflow path", `runs="repos/$REPO/actions/workflows/check.yml/runs"`, `runs="repos/$REPO/actions/runs"`},
		{"gh failure counts as success in the head query", `if n=$(gh api "$runs?head_sha`, `if n=1; gh api "$runs?head_sha`},
		{"gh failure counts as success in the fallback", `[ -n "$r" ] || break`, `[ -n "$r" ] || r="1 1"`},
		{"no pause in the head loop", `if [ "$i" -lt "$attempts" ]; then sleep "$sleep_s"; fi`, `:`},
		{"no pause in the page loop", `if [ "$t" -lt "$attempts" ]; then sleep "$sleep_s"; fi`, `:`},
		{"pause default changed", "CI_SLEEP:-2", "CI_SLEEP:-0"},
		{"API failure message lost", `if [ "$api_ok" = 0 ]; then`, `if false; then`},
		{"API failure never noted", "api_ok=1\n\t\t\tif [ \"$n\"", "if [ \"$n\""},
		{"list page error not retried", `[ "$t" -lt "$attempts" ]; then sleep "$sleep_s"; fi
		done`, `[ "$t" -lt "$attempts" ]; then sleep "$sleep_s"; fi
		break
		done`},
	}
	for _, m := range mutants {
		if !strings.Contains(string(raw), m.from) {
			t.Errorf("HARNESS ERROR, NOT A KILL: %q: text not found: %q", m.name, m.from)
			continue
		}
		path := filepath.Join(t.TempDir(), "release-verify.sh")
		if err := os.WriteFile(path, []byte(strings.Replace(string(raw), m.from, m.to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		killed := false
		for _, c := range ciCases() {
			if len(ciProblems(t, path, c)) > 0 {
				killed = true
				break
			}
		}
		if !killed {
			t.Errorf("mutant %q survived", m.name)
		}
	}
}

func TestNotesBaselineIsTheFinalReleaseAboveItsPreReleases(t *testing.T) {
	dir := newRepo(t, "feat: a")
	git(t, dir, "tag", "-a", "-m", "r", "v1.0.0-rc.1")
	commitFile(t, dir, "b", "fix: b")
	git(t, dir, "tag", "-a", "-m", "r", "v1.0.0")
	commitFile(t, dir, "c", "feat: c")
	git(t, dir, "tag", "-a", "-m", "r", "v1.0.1")
	r := sh(t, dir, nil, "release-notes.sh", "v1.0.1", git(t, dir, "rev-parse", "HEAD"), "1.27.2")
	if r.code != 0 || !strings.Contains(r.stdout, "Changes since v1.0.0\n") || strings.Contains(r.stdout, "since v1.0.0-rc.1") {
		t.Errorf("baseline must be v1.0.0: %+v", r)
	}
}
