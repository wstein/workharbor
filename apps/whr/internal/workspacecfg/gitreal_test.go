// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func realGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for this test: %v", err)
	}
}

func TestValidateRealPinnedRepository(t *testing.T) {
	realGit(t)
	r := validate(t, pinnedRepo(t), workspacecfg.Options{})
	if !r.Valid || errorCodes(r) != "" {
		t.Fatalf("%+v", r.Findings)
	}
	f := find(r, "submodule.pinned")
	if f.Subject != "projects/app" || !strings.Contains(f.Message, sha) || f.Severity != workspacecfg.SevInfo {
		t.Errorf("%+v", f)
	}
	if has(r, "git.not_repository") || has(r, "git.head_missing") {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateRealCleanRepositoryWithoutSubmodules(t *testing.T) {
	realGit(t)
	r := validate(t, repoDir(t, true), workspacecfg.Options{})
	if !r.Valid || len(r.Findings) != 0 {
		t.Errorf("%+v", r.Findings)
	}
}

// TestValidateUnbornHeadStagedSubmodule: a freshly initialised repository with
// an unborn HEAD and a staged submodule (FR-054).
func TestValidateUnbornHeadStagedSubmodule(t *testing.T) {
	realGit(t)
	dir := repoDir(t, false)
	write(t, dir, ".gitmodules", gitmodulesApp)
	runGit(t, dir, "", "add", ".gitmodules")
	addGitlink(t, dir, "projects/app", sha)
	r := validate(t, dir, workspacecfg.Options{})
	if !r.Valid || !has(r, "git.head_missing") || !has(r, "submodule.pinned") || has(r, "git.listing_failed") {
		t.Errorf("%+v", r.Findings)
	}
	if find(r, "git.head_missing").Severity != workspacecfg.SevWarning {
		t.Error("head_missing is not a warning")
	}
	// An empty unborn repository only warns.
	r = validate(t, repoDir(t, false), workspacecfg.Options{})
	if !r.Valid || codesOf(r.Findings) != "git.head_missing" {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateSubmoduleRules(t *testing.T) {
	realGit(t)
	for name, tc := range map[string]struct {
		modules string
		want    string
	}{
		"credentials": {"[submodule \"app\"]\n\tpath = projects/app\n\turl = https://u:CANARYpw@f.example/o/app.git\n", "submodule.url_credentials"},
		"scp-like ok": {"[submodule \"app\"]\n\tpath = projects/app\n\turl = git@f.example:o/app.git\n", ""},
		"file url":    {"[submodule \"app\"]\n\tpath = projects/app\n\turl = file:///srv/app\n", "submodule.url_invalid"},
		"ext url":     {"[submodule \"app\"]\n\tpath = projects/app\n\turl = ext::sh -c touch% x\n", "submodule.url_invalid"},
		"relative":    {"[submodule \"app\"]\n\tpath = projects/app\n\turl = ../app\n", "submodule.url_invalid"},
		"update cmd":  {"[submodule \"app\"]\n\tpath = projects/app\n\turl = https://f.example/o/app.git\n\tupdate = !touch x\n", "submodule.update_command"},
		"outside":     {"[submodule \"app\"]\n\tpath = other/app\n\turl = https://f.example/o/app.git\n", "submodule.outside_projects"},
		"unsupported": {"[submodule \"app\"]\n\tpath = projects/app\n\turl = https://f.example/o/app.git\n\tbranch = main\n", "gitmodules.unsupported_key"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := repoDir(t, false)
			write(t, dir, ".gitmodules", tc.modules)
			runGit(t, dir, "", "add", "-A")
			path := "projects/app"
			if name == "outside" {
				path = "other/app"
			}
			addGitlink(t, dir, path, sha)
			runGit(t, dir, "", "commit", "-q", "-m", "x")
			r := validate(t, dir, workspacecfg.Options{})
			if got := errorCodes(r); got != tc.want {
				t.Errorf("errors %q, want %q: %+v", got, tc.want, r.Findings)
			}
			for _, f := range r.Findings {
				if strings.Contains(f.Message+f.Subject+f.Remediation, "CANARY") {
					t.Errorf("credential echoed: %+v", f)
				}
			}
		})
	}
}

func TestValidateRealUnlistedAndNotPinned(t *testing.T) {
	realGit(t)
	dir := repoDir(t, false)
	write(t, dir, ".gitmodules", gitmodulesApp)
	runGit(t, dir, "", "add", "-A")
	addGitlink(t, dir, "projects/other", sha)
	runGit(t, dir, "", "commit", "-q", "-m", "x")
	r := validate(t, dir, workspacecfg.Options{})
	if errorCodes(r) != "submodule.not_pinned,submodule.unlisted" {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateRealStagedChanges(t *testing.T) {
	realGit(t)
	dir := pinnedRepo(t)
	addGitlink(t, dir, "projects/app", strings.Repeat("b", 40))
	write(t, dir, ".gitmodules", gitmodulesApp+"# staged\n")
	runGit(t, dir, "", "add", ".gitmodules")
	r := validate(t, dir, workspacecfg.Options{})
	if !r.Valid || codesOf(r.Findings) != "gitmodules.uncommitted_change,submodule.not_committed,submodule.pinned" {
		t.Errorf("%+v", r.Findings)
	}
	if find(r, "submodule.pinned").Message != "pinned at "+sha {
		t.Errorf("the committed pin is reported, got %q", find(r, "submodule.pinned").Message)
	}
}

func TestValidateRealProjectsFilesAndTreePaths(t *testing.T) {
	realGit(t)
	dir := repoDir(t, false)
	write(t, dir, "projects/README.md", "x")
	runGit(t, dir, "", "add", "-A")
	runGit(t, dir, "", "commit", "-q", "-m", "x")
	r := validate(t, dir, workspacecfg.Options{})
	if !r.Valid || codesOf(r.Findings) != "projects.not_a_submodule" || find(r, "projects.not_a_submodule").Subject != "projects/README.md" {
		t.Errorf("%+v", r.Findings)
	}
	// A case-folded .git component in the HEAD tree is an error.
	dir = repoDir(t, false)
	blob := strings.TrimSpace(runGit(t, dir, "", "hash-object", "-w", "--stdin"))
	t1 := strings.TrimSpace(runGit(t, dir, "100644 blob "+blob+"\tx\n", "mktree"))
	t2 := strings.TrimSpace(runGit(t, dir, "040000 tree "+t1+"\t.GIT\n", "mktree"))
	t3 := strings.TrimSpace(runGit(t, dir, "040000 tree "+t2+"\ta\n", "mktree"))
	c := strings.TrimSpace(runGit(t, dir, "", "commit-tree", t3, "-m", "x"))
	runGit(t, dir, "", "update-ref", "refs/heads/main", c)
	r = validate(t, dir, workspacecfg.Options{})
	if errorCodes(r) != "git.tree_path_invalid" {
		t.Errorf("%+v", r.Findings)
	}
}

func TestValidateGitInternalsUnsafe(t *testing.T) {
	realGit(t)
	for name, rel := range map[string]string{"commondir": ".git/commondir", "alternates": ".git/objects/info/alternates"} {
		dir := repoDir(t, true)
		write(t, dir, rel, "/elsewhere\n")
		r := validate(t, dir, workspacecfg.Options{})
		if r.Valid || errorCodes(r) != "git.unsafe_path" {
			t.Errorf("%s: %+v", name, r.Findings)
		}
	}
}

func TestValidateRealUnsafeGitDirectories(t *testing.T) {
	realGit(t)
	dir := repoDir(t, true)
	if err := os.Rename(dir+"/.git", dir+"/real-git"); err != nil {
		t.Fatal(err)
	}
	symlink(t, dir, ".git", dir+"/real-git")
	if r := validate(t, dir, workspacecfg.Options{}); errorCodes(r) != "git.unsafe_path" {
		t.Errorf("symlink: %+v", r.Findings)
	}
	remove(t, dir, ".git")
	write(t, dir, ".git", "gitdir: "+dir+"/real-git\n")
	if r := validate(t, dir, workspacecfg.Options{}); errorCodes(r) != "git.unsafe_path" {
		t.Errorf("gitfile: %+v", r.Findings)
	}
}

func TestValidateIsReadOnlyWithGit(t *testing.T) {
	realGit(t)
	dir := pinnedRepo(t)
	write(t, dir, "projects/untracked", "x")
	before := snapshot(t, dir)
	for i := 0; i < 3; i++ {
		validate(t, dir, workspacecfg.Options{})
	}
	if after := snapshot(t, dir); after != before {
		t.Errorf("tree changed, including .git:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// hostileRepo builds a repository whose configuration runs a marker command
// from every place Git or a container tool could execute repository content.
func hostileRepo(t *testing.T, marker string) string {
	t.Helper()
	dir := repoDir(t, false)
	hook := filepath.Join(t.TempDir(), "hook.sh")
	script := "#!/bin/sh\ntouch '" + marker + "'\nexit 0\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	included := filepath.Join(t.TempDir(), "included")
	if err := os.WriteFile(included, []byte("[core]\n\tfsmonitor = "+hook+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".gitattributes", "* filter=evil\n")
	write(t, dir, ".gitmodules", gitmodulesApp+"\tupdate = !"+hook+"\n")
	for _, h := range []string{"post-checkout", "pre-commit", "post-merge", "reference-transaction", "post-index-change", "fsmonitor-watchman"} {
		if err := os.MkdirAll(dir+"/.git/hooks", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/.git/hooks/"+h, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := fmt.Sprintf(`[core]
	fsmonitor = %[1]s
	pager = %[1]s
	sshCommand = %[1]s
	editor = %[1]s
	hooksPath = %[2]s/.git/hooks
[alias]
	ls-files = !%[1]s
	status = !%[1]s
[filter "evil"]
	clean = %[1]s
	smudge = %[1]s
	process = %[1]s
[diff]
	external = %[1]s
[credential]
	helper = !%[1]s
[remote "origin"]
	url = ext::%[1]s
	uploadpack = %[1]s
[include]
	path = %[3]s
`, hook, dir, included)
	f, err := os.OpenFile(dir+"/.git/config", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(cfg); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stage the hostile repository content without running its payloads.
func commitHostile(t *testing.T, dir string) {
	t.Helper()
	// The fixture's own Git runs with the payload config neutralised.
	run := func(args ...string) {
		a := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "filter.evil.clean=cat", "-c", "filter.evil.smudge=cat", "-c", "filter.evil.process=", "-c", "core.pager=cat"}, args...)
		runGit(t, dir, "", a...)
	}
	run("add", "-A")
	run("update-index", "--add", "--cacheinfo", "160000,"+sha+",projects/app")
	run("commit", "-q", "-m", "hostile")
}

func TestValidateExecutesNothing(t *testing.T) {
	realGit(t)
	marker := filepath.Join(t.TempDir(), "MARKER")
	dir := hostileRepo(t, marker)
	commitHostile(t, dir)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("fixture setup ran a payload")
	}
	before := snapshot(t, dir)
	r := validate(t, dir, workspacecfg.Options{})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("validate executed repository content: marker exists")
	}
	if !has(r, "submodule.update_command") {
		t.Errorf("the update command is not reported: %+v", r.Findings)
	}
	if after := snapshot(t, dir); after != before {
		t.Errorf("tree changed:\n%s\n---\n%s", before, after)
	}
}

// TestHostileFixtureIsLive proves the fixture would run a payload under an
// unhardened Git, so that the marker's absence above means something.
func TestHostileFixtureIsLive(t *testing.T) {
	realGit(t)
	marker := filepath.Join(t.TempDir(), "MARKER")
	dir := hostileRepo(t, marker)
	commitHostile(t, dir)
	cmd := exec.Command("git", "status")
	cmd.Dir = dir
	cmd.Env = hermeticEnv(t.TempDir())
	_ = cmd.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Skipf("this Git did not run the fsmonitor payload on status, so the live control is inconclusive: %v", err)
	}
}

// partialClone builds a workspace clone with a promisor remote whose blobs are
// missing, protocol.file.allow=always and an uploadpack that writes a marker.
func partialClone(t *testing.T, marker string) string {
	t.Helper()
	src := t.TempDir()
	runGit(t, src, "", "init", "-q", "-b", "main", "--template=")
	runGit(t, src, "", "config", "uploadpack.allowFilter", "true")
	runGit(t, src, "", "config", "uploadpack.allowAnySHA1InWant", "true")
	write(t, src, "workharbor.json", manifestV1)
	write(t, src, ".devcontainer/devcontainer.json", devcontainer)
	write(t, src, "projects/.gitkeep", "")
	write(t, src, ".gitmodules", gitmodulesApp)
	runGit(t, src, "", "add", "-A")
	runGit(t, src, "", "update-index", "--add", "--cacheinfo", "160000,"+sha+",projects/app")
	runGit(t, src, "", "commit", "-q", "-m", "x")
	dst := filepath.Join(t.TempDir(), "ws")
	runGit(t, filepath.Dir(dst), "", "-c", "protocol.file.allow=always", "clone", "-q", "--no-checkout", "--filter=blob:none", "file://"+src, dst)
	script := filepath.Join(t.TempDir(), "up.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\nexec git-upload-pack \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dst, "workharbor.json", manifestV1)
	write(t, dst, ".devcontainer/devcontainer.json", devcontainer)
	write(t, dst, "projects/.gitkeep", "")
	runGit(t, dst, "", "config", "protocol.file.allow", "always")
	runGit(t, dst, "", "config", "remote.origin.uploadpack", script)
	return dst
}

func TestValidateGitProtocolOverridesIgnored(t *testing.T) {
	realGit(t)
	marker := filepath.Join(t.TempDir(), "MARKER")
	dir := partialClone(t, marker)
	// Positive control: an unhardened Git fetches the missing blob and runs uploadpack.
	ctl := exec.Command("git", "cat-file", "blob", "HEAD:.gitmodules")
	ctl.Dir = dir
	ctl.Env = hermeticEnv(t.TempDir())
	if err := ctl.Run(); err != nil {
		t.Fatalf("control: the fixture cannot lazy-fetch: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("control: uploadpack did not run: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	// The control fetched the blob into the repository; build a fresh clone.
	dir = partialClone(t, marker)
	r := validate(t, dir, workspacecfg.Options{})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("validate triggered a fetch: uploadpack marker exists")
	}
	if r.Valid || errorCodes(r) != "git.listing_failed" {
		t.Errorf("a failed blob read must be an error: %+v", r.Findings)
	}
}

func TestValidateNoNetwork(t *testing.T) {
	realGit(t)
	denied := func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Errorf("network use: %s %s", network, addr)
		return nil, fmt.Errorf("denied")
	}
	http.DefaultTransport.(*http.Transport).DialContext = denied
	net.DefaultResolver.PreferGo = true
	net.DefaultResolver.Dial = denied
	r := validate(t, pinnedRepo(t), workspacecfg.Options{})
	if !r.Valid {
		t.Errorf("%+v", r.Findings)
	}
}

// TestValidateGitDeadlineIsListingFailure: Git blocks on a FIFO loose reference
// and on a FIFO packed-refs; the shared budget ends it and the result is an error.
func blockedGit(t *testing.T) {
	realGit(t)
	for name, setup := range map[string]func(t *testing.T, dir string){
		"fifo loose ref": func(t *testing.T, dir string) {
			fifo(t, dir, ".git/refs/heads/main")
		},
		"fifo packed-refs": func(t *testing.T, dir string) {
			remove(t, dir, ".git/refs/heads/main")
			fifo(t, dir, ".git/packed-refs")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := repoDir(t, true)
			setup(t, dir)
			start := time.Now()
			r := validate(t, dir, workspacecfg.Options{Budget: 400 * time.Millisecond})
			if r.Valid || errorCodes(r) != "git.listing_failed" {
				t.Errorf("%+v", r.Findings)
			}
			if time.Since(start) > 10*time.Second {
				t.Errorf("took %v", time.Since(start))
			}
		})
	}
}

func TestValidateInterruptedGitIsKilled(t *testing.T) {
	realGit(t)
	dir := repoDir(t, true)
	fifo(t, dir, ".git/refs/heads/main")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := workspacecfg.Validate(ctx, dir, workspacecfg.Options{Budget: time.Minute})
	if err != context.Canceled {
		t.Errorf("%v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

func hostileListings(t *testing.T) {
	realGit(t)
	t.Run("100000 index entries", func(t *testing.T) {
		dir := repoDir(t, false)
		var b strings.Builder
		for i := 0; i < 100000; i++ {
			fmt.Fprintf(&b, "100644 e69de29bb2d1d6434b8b29ae775ad8c2e48c5391\tprojects/f%06d\n", i)
		}
		runGit(t, dir, "", "hash-object", "-w", "--stdin")
		runGit(t, dir, b.String(), "update-index", "--add", "--index-info")
		start := time.Now()
		r := validate(t, dir, workspacecfg.Options{})
		if r.Valid || errorCodes(r) != "git.limit_exceeded" {
			t.Errorf("%+v", r.Findings)
		}
		if time.Since(start) > workspacecfg.DefaultBudget+5*time.Second {
			t.Errorf("took %v", time.Since(start))
		}
	})
	t.Run("100000 gitmodules entries", func(t *testing.T) {
		dir := repoDir(t, false)
		var b strings.Builder
		for i := 0; i < 100000; i++ {
			fmt.Fprintf(&b, "[submodule \"s%d\"]\n\tpath = projects/s%d\n\turl = https://f.example/o/s%d.git\n", i, i, i)
		}
		write(t, dir, ".gitmodules", b.String())
		runGit(t, dir, "", "add", ".gitmodules")
		runGit(t, dir, "", "commit", "-q", "-m", "x")
		r := validate(t, dir, workspacecfg.Options{})
		if r.Valid || errorCodes(r) != "gitmodules.too_large" {
			t.Errorf("%+v", r.Findings)
		}
	})
}

func TestMain(m *testing.M) {
	if os.Getenv("WHR_NO_NETWORK_CHILD") == "1" {
		os.Exit(noNetworkChild())
	}
	os.Exit(m.Run())
}

// noNetworkChild runs inside `unshare -rn`: no usable network at all.
func noNetworkChild() int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "child: "+format+"\n", a...)
		return 1
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback == 0 {
			return fail("a non-loopback interface exists: %s", i.Name)
		}
	}
	if c, err := net.DialTimeout("tcp", "1.1.1.1:53", time.Second); err == nil {
		c.Close()
		return fail("the network is reachable")
	}
	tmp, err := os.MkdirTemp("", "whrnonet")
	if err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(tmp)
	run := func(dir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = hermeticEnv(tmp)
		return cmd.Run()
	}
	for rel, content := range map[string]string{
		"workharbor.json": manifestV1, ".devcontainer/devcontainer.json": devcontainer, "projects/.gitkeep": "", ".gitmodules": gitmodulesApp,
	} {
		p := filepath.Join(tmp, rel)
		if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, []byte(content), 0o644) != nil {
			return fail("fixture")
		}
	}
	for _, a := range [][]string{
		{"init", "-q", "-b", "main", "--template="}, {"add", "-A"},
		{"update-index", "--add", "--cacheinfo", "160000," + sha + ",projects/app"}, {"commit", "-q", "-m", "x"},
	} {
		if err := run(tmp, a...); err != nil {
			return fail("git %v: %v", a, err)
		}
	}
	r, err := workspacecfg.Validate(context.Background(), tmp, workspacecfg.Options{})
	if err != nil || !r.Valid || !has(r, "submodule.pinned") {
		return fail("validate: %v %+v", err, r)
	}
	fmt.Println("child ok")
	return 0
}

// netnsCandidates are the ways to start bin in a fresh network namespace, best
// first: an unprivileged user namespace, then passwordless sudo with the
// privileges dropped back to the invoking user before bin runs (runners that
// restrict unprivileged user namespaces, such as GitHub's ubuntu-24.04, allow
// sudo). Either way bin runs as the invoking user with no network at all. look
// resolves a program name to an absolute path ("" when it is not installed); the
// candidates use absolute paths only, also inside the sudo command.
func netnsCandidates(look func(string) string, bin string, uid, gid int, home, path, tmp string) [][]string {
	unshare, sudo, setpriv, env := look("unshare"), look("sudo"), look("setpriv"), look("env")
	var out [][]string
	if unshare != "" {
		out = append(out, []string{unshare, "-rn", bin})
	}
	if unshare != "" && sudo != "" && setpriv != "" && env != "" {
		out = append(out, []string{sudo, "-n", unshare, "--net", "--", setpriv,
			fmt.Sprintf("--reuid=%d", uid), fmt.Sprintf("--regid=%d", gid), "--clear-groups",
			env, "WHR_NO_NETWORK_CHILD=1", "HOME=" + home, "PATH=" + path, "TMPDIR=" + tmp, bin})
	}
	return out
}

// skipForbidden reports that a run without its proof (namespace, device node) must fail: on CI
// (CI is set by GitHub Actions) or when WHR_REQUIRE_NETNS=1. Elsewhere the test
// still says SKIPPED, NOT PASSED.
func skipForbidden(env func(string) string) bool {
	return env("WHR_REQUIRE_NETNS") == "1" || env("CI") != ""
}

func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil || !filepath.IsAbs(p) {
		return ""
	}
	return p
}

// runBounded runs argv with an empty standard input in its own process group and
// kills the whole group when timeout passes, so a stuck sudo or namespace child
// cannot hang the test binary.
func runBounded(timeout time.Duration, env []string, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("timed out after %v: %w", timeout, ctx.Err())
	}
	return string(out), err
}

func TestRunBoundedKillsTheGroupOnTimeout(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	_, err := runBounded(500*time.Millisecond, os.Environ(), "/bin/sh", "-c", "sleep 300 & echo $! > '"+pidFile+"'; wait")
	if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 5*time.Second {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
	raw, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	deadline := time.Now().Add(3 * time.Second)
	for pid > 0 && alive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pid <= 0 || alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("the grandchild %d survived the timeout", pid)
	}
}

func TestNetnsCandidatesDropPrivilegesAndRequirementIsSet(t *testing.T) {
	look := func(n string) string { return "/abs/" + n }
	c := netnsCandidates(look, "/bin/x", 1001, 1002, "/home/u", "/usr/bin", "/tmp")
	if len(c) != 2 || strings.Join(c[0], " ") != "/abs/unshare -rn /bin/x" {
		t.Fatalf("%v", c)
	}
	sudo := strings.Join(c[1], " ")
	for _, want := range []string{"/abs/sudo -n /abs/unshare --net -- /abs/setpriv --reuid=1001 --regid=1002 --clear-groups /abs/env WHR_NO_NETWORK_CHILD=1", " /bin/x"} {
		if !strings.Contains(sudo, want) {
			t.Errorf("%q lacks %q", sudo, want)
		}
	}
	for _, argv := range c {
		for _, a := range argv[:1] {
			if !filepath.IsAbs(a) {
				t.Errorf("%q is not absolute", a)
			}
		}
	}
	if strings.Index(sudo, "setpriv") > strings.Index(sudo, "/bin/x") || strings.Index(sudo, "unshare --net") > strings.Index(sudo, "setpriv") {
		t.Errorf("the privileges must be dropped inside the namespace, before the binary runs: %q", sudo)
	}
	if got := netnsCandidates(func(n string) string {
		if n == "sudo" {
			return ""
		}
		return "/abs/" + n
	}, "/bin/x", 1, 1, "h", "p", "t"); len(got) != 1 {
		t.Errorf("without sudo only the user namespace remains: %v", got)
	}
	for _, tc := range []struct {
		env  map[string]string
		want bool
	}{{nil, false}, {map[string]string{"CI": "true"}, true}, {map[string]string{"WHR_REQUIRE_NETNS": "1"}, true}, {map[string]string{"WHR_REQUIRE_NETNS": "0"}, false}} {
		if got := skipForbidden(func(k string) string { return tc.env[k] }); got != tc.want {
			t.Errorf("%v: %v", tc.env, got)
		}
	}
}

// TestValidateNoNetworkUnshare re-runs a validation in a fresh network
// namespace (Linux). It must really run on CI: when no namespace can be made
// there, or WHR_REQUIRE_NETNS=1, it fails instead of skipping. Every attempt is
// bounded and its process group killed on timeout.
func TestValidateNoNetworkUnshare(t *testing.T) {
	realGit(t)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var reasons []string
	candidates := netnsCandidates(lookPath, bin, os.Getuid(), os.Getgid(), os.Getenv("HOME"), os.Getenv("PATH"), os.TempDir())
	if len(candidates) == 0 {
		reasons = append(reasons, "unshare is not available")
	}
	for _, argv := range candidates {
		out, err := runBounded(2*time.Minute, append(os.Environ(), "WHR_NO_NETWORK_CHILD=1"), argv...)
		text := strings.TrimSpace(out)
		if err != nil && (strings.Contains(err.Error(), "timed out") || strings.HasPrefix(text, "unshare:") || strings.HasPrefix(text, "sudo:") || strings.HasPrefix(text, "setpriv:")) {
			reasons = append(reasons, filepath.Base(argv[0])+": "+err.Error()+" "+text)
			continue
		}
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(out, "child ok") {
			t.Errorf("%s", out)
		}
		return
	}
	msg := "the network-namespace run did not happen (cannot create one here): " + strings.Join(reasons, "; ")
	if skipForbidden(os.Getenv) {
		t.Fatalf("FAILED, NOT SKIPPED: %s", msg)
	}
	t.Skip("SKIPPED, NOT PASSED: " + msg)
}
