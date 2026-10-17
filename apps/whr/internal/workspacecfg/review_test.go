// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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

// M2: links inside .git that Git would follow out of the directory.
func TestValidateGitInternalLinksAreUnsafe(t *testing.T) {
	cases := map[string]func(t *testing.T, d string){
		"refs": func(t *testing.T, d string) { symlink(t, d, ".git/refs", d+"/elsewhere") },
		"refs/heads": func(t *testing.T, d string) {
			write(t, d, ".git/refs/x", "")
			symlink(t, d, ".git/refs/heads", d+"/elsewhere")
		},
		"loose ref named by HEAD": func(t *testing.T, d string) {
			write(t, d, ".git/refs/heads/keep", "")
			symlink(t, d, ".git/refs/heads/main", "/etc/hostname")
		},
		"packed-refs": func(t *testing.T, d string) { symlink(t, d, ".git/packed-refs", "/etc/hostname") },
		"objects":     func(t *testing.T, d string) { remove(t, d, ".git/objects"); symlink(t, d, ".git/objects", "/etc") },
		"objects/info": func(t *testing.T, d string) {
			remove(t, d, ".git/objects/info")
			symlink(t, d, ".git/objects/info", "/etc")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := validDir(t)
			minimalGitDir(t, dir)
			setup(t, dir)
			f := &fakeGit{handler: healthyHandler}
			r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
			if r.Valid || errorCodes(r) != "git.unsafe_path" || len(f.calls) != 0 {
				t.Fatalf("%+v calls=%v", r.Findings, f.subs())
			}
			if m := find(r, "git.unsafe_path").Message; !strings.Contains(m, "symbolic link") || !strings.Contains(m, ".git/") {
				t.Errorf("the message must name the link: %q", m)
			}
		})
	}
}

// M4: an unborn HEAD needs the named ref to be absent.
func TestValidateUnbornDecision(t *testing.T) {
	unborn := func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		switch sub {
		case "rev-parse":
			return workspacecfg.GitOut{Exit: 1}, nil
		case "symbolic-ref":
			return ok("refs/heads/main\n")
		}
		return healthyHandler(sub, c)
	}
	run := func(t *testing.T, setup func(d string), h func(string, workspacecfg.GitCmd) (workspacecfg.GitOut, error)) *workspacecfg.Report {
		dir := validDir(t)
		minimalGitDir(t, dir)
		setup(dir)
		return validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: h}, LookGit: fixedGit("/git")})
	}
	if r := run(t, func(string) {}, unborn); !has(r, "git.head_missing") || has(r, "git.listing_failed") {
		t.Errorf("plain unborn: %+v", r.Findings)
	}
	if r := run(t, func(d string) { write(t, d, ".git/refs/heads/main", sha+"\n") }, unborn); errorCodes(r) != "git.listing_failed" {
		t.Errorf("loose ref exists: %+v", r.Findings)
	}
	if r := run(t, func(d string) { write(t, d, ".git/packed-refs", "# pack-refs\n"+sha+" refs/heads/main\n") }, unborn); errorCodes(r) != "git.listing_failed" {
		t.Errorf("packed ref exists: %+v", r.Findings)
	}
	if r := run(t, func(d string) { write(t, d, ".git/packed-refs", "# pack-refs\n"+sha+" refs/heads/other\n") }, unborn); !has(r, "git.head_missing") {
		t.Errorf("other packed ref: %+v", r.Findings)
	}
	for name, h := range map[string]func(string, workspacecfg.GitCmd) (workspacecfg.GitOut, error){
		"rev-parse 128 although symbolic-ref succeeds": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "rev-parse" {
				return workspacecfg.GitOut{Exit: 128}, nil
			}
			return unborn(sub, c)
		},
		"rev-parse 1 with stderr": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "rev-parse" {
				return workspacecfg.GitOut{Exit: 1, Stderr: []byte("x")}, nil
			}
			return unborn(sub, c)
		},
		"symbolic-ref prints a non-ref": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "symbolic-ref" {
				return ok("HEAD\n")
			}
			return unborn(sub, c)
		},
		"symbolic-ref names another ref than HEAD file": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "symbolic-ref" {
				return ok("refs/heads/../../x\n")
			}
			return unborn(sub, c)
		},
	} {
		if r := run(t, func(string) {}, h); errorCodes(r) != "git.listing_failed" {
			t.Errorf("%s: %+v", name, r.Findings)
		}
	}
}

// M3: the in-memory checker and the disk validator agree on every shared vector.
func TestMemoryAndDiskAgreeOnAllVectors(t *testing.T) {
	for _, c := range loadVectors(t) {
		t.Run(c.ID, func(t *testing.T) {
			raw := c.bytes(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "workharbor.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			in := workspacecfg.MemoryInput{Manifest: raw}
			if m := workspacecfg.CheckManifest(raw).Manifest; m != nil {
				if m.DevcontainerPath != "" {
					write(t, dir, m.DevcontainerPath, devcontainer)
					in.Devcontainer, in.DevcontainerPresent = []byte(devcontainer), true
				}
				if m.ProjectsPath != "" {
					write(t, dir, m.ProjectsPath+"/.gitkeep", "")
					in.ProjectsPresent = true
				}
			}
			disk, _ := json.Marshal(validate(t, dir, workspacecfg.Options{GitFree: true}))
			mem, _ := json.Marshal(workspacecfg.ValidateMemory(dir, in))
			if string(disk) != string(mem) {
				t.Errorf("disk %s\nmemory %s", disk, mem)
			}
		})
	}
	// Oversized devcontainer: one message for both forms.
	dir := validDir(t)
	big := "{}" + strings.Repeat(" ", workspacecfg.MaxDevcontainerLen)
	write(t, dir, ".devcontainer/devcontainer.json", big)
	disk, _ := json.Marshal(validate(t, dir, workspacecfg.Options{GitFree: true}))
	mem, _ := json.Marshal(workspacecfg.ValidateMemory(dir, workspacecfg.MemoryInput{Manifest: []byte(manifestV1), Devcontainer: []byte(big), DevcontainerPresent: true, ProjectsPresent: true}))
	if string(disk) != string(mem) {
		t.Errorf("devcontainer too_large differs:\n%s\n%s", disk, mem)
	}
}

// M5: exact boundaries.
func TestBoundaries(t *testing.T) {
	// devcontainer 256 KiB
	pad := func(n int) []byte { return []byte("{}" + strings.Repeat(" ", n-2)) }
	if fs := workspacecfg.CheckDevcontainer(pad(256 << 10)); len(fs) != 0 {
		t.Errorf("256 KiB: %+v", fs)
	}
	if got := codesOf(workspacecfg.CheckDevcontainer(pad(256<<10 + 1))); got != "devcontainer.too_large" {
		t.Errorf("256 KiB + 1: %s", got)
	}
	// findings 200 / 201
	mk := func(n int) []workspacecfg.Finding {
		var fs []workspacecfg.Finding
		for i := 0; i < n; i++ {
			fs = append(fs, workspacecfg.Finding{Code: "devcontainer.unsupported_key", Severity: workspacecfg.SevWarning, Subject: strconv.Itoa(i)})
		}
		return fs
	}
	if r := workspacecfg.NewReport("d", mk(200)); r.Truncated || len(r.Findings) != 200 || r.Counts.Warning != 200 {
		t.Errorf("200: %+v", r.Counts)
	}
	if r := workspacecfg.NewReport("d", mk(201)); !r.Truncated || len(r.Findings) != 200 || r.Counts.Warning != 201 {
		t.Errorf("201: %+v", r.Counts)
	}
	// listing entries 10,000 / 10,001
	rec := func(n int) string {
		return strings.Repeat("100644 blob "+sha+"\tf\x00", n)
	}
	for n, want := range map[int]string{workspacecfg.MaxListingEntries: "", workspacecfg.MaxListingEntries + 1: "git.limit_exceeded"} {
		dir := validDir(t)
		minimalGitDir(t, dir)
		f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "ls-tree" {
				return ok(rec(n))
			}
			return healthyHandler(sub, c)
		}}
		r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
		got := ""
		if has(r, "git.limit_exceeded") {
			got = "git.limit_exceeded"
		}
		if got != want {
			t.Errorf("%d entries: %q, want %q: %+v", n, got, want, r.Findings)
		}
	}
}

// shim writes an executable script standing in for git.
func shim(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecRunnerOutputCapBoundary(t *testing.T) {
	const n = workspacecfg.MaxListingBytes
	for size, truncated := range map[int]bool{n: false, n + 1: true} {
		p := shim(t, fmt.Sprintf("head -c %d /dev/zero", size))
		out, err := workspacecfg.ExecRunner{}.Run(context.Background(), workspacecfg.GitCmd{Path: p, Env: []string{"PATH=" + os.Getenv("PATH")}, Dir: "/", MaxOutput: n, Timeout: 20 * time.Second})
		if err != nil || out.Truncated != truncated || len(out.Stdout) != n {
			t.Errorf("%d bytes: truncated=%v len=%d err=%v", size, out.Truncated, len(out.Stdout), err)
		}
	}
}

func TestExecRunnerStdinIsDevNull(t *testing.T) {
	p := shim(t, `readlink /proc/self/fd/0 2>/dev/null || ls -l /dev/fd/0`)
	out, err := workspacecfg.ExecRunner{}.Run(context.Background(), workspacecfg.GitCmd{Path: p, Env: []string{"PATH=" + os.Getenv("PATH")}, Dir: "/", MaxOutput: 4096, Timeout: 10 * time.Second})
	if err != nil || !strings.Contains(string(out.Stdout), "/dev/null") {
		t.Errorf("stdin is %q (%v)", out.Stdout, err)
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestExecRunnerKillsTheProcessGroupWithinTheBudget(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	p := shim(t, "sleep 300 &\necho $! > '"+pidFile+"'\nwait")
	start := time.Now()
	_, err := workspacecfg.ExecRunner{}.Run(context.Background(), workspacecfg.GitCmd{Path: p, Env: []string{"PATH=" + os.Getenv("PATH")}, Dir: "/", MaxOutput: 4096, Timeout: time.Second})
	elapsed := time.Since(start)
	if err == nil {
		t.Error("no error on timeout")
	}
	if elapsed > 1300*time.Millisecond {
		t.Errorf("the run lasted %v: the kill delay is not part of the budget", elapsed)
	}
	raw, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if pid <= 0 {
		t.Fatalf("no grandchild pid: %q", raw)
	}
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Error("the grandchild survived the timeout")
	}
}

// M7: a trailing slash must not bypass the symlinked-directory refusal.
func TestValidateSymlinkedDirectorySpellings(t *testing.T) {
	real := validDir(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{link, link + "/", link + "/.", link + "//"} {
		r := validate(t, spelling, workspacecfg.Options{GitFree: true})
		if r.Valid || errorCodes(r) != "manifest.unsafe_path" || r.Directory != spelling {
			t.Errorf("%q: %+v", spelling, r)
		}
		for _, f := range r.Findings {
			if strings.Contains(f.Message, "the directory the directory") {
				t.Errorf("doubled message: %q", f.Message)
			}
			if f.Code == "manifest.unsafe_path" && !strings.Contains(f.Remediation, "real directory") {
				t.Errorf("remediation %q", f.Remediation)
			}
		}
	}
	// A real directory with a trailing slash still works.
	if r := validate(t, real+"/", workspacecfg.Options{GitFree: true}); !r.Valid {
		t.Errorf("%+v", r.Findings)
	}
}

// L9: an unreadable directory gets its own remediation.
func TestValidateUnreadableDirectoryRemediation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	r, err := workspacecfg.Validate(context.Background(), dir, workspacecfg.Options{GitFree: true})
	if err == nil && r != nil {
		f := find(r, "manifest.unsafe_path")
		if strings.Contains(f.Remediation, "real directory") || strings.Contains(f.Message, "the directory the directory") {
			t.Errorf("%+v", f)
		}
	}
}

// L18
func TestValidateUnmergedIndexHasItsOwnMessage(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	r := validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: unmerged}, LookGit: fixedGit("/git")})
	if m := find(r, "git.listing_failed").Message; !strings.Contains(m, "unmerged") {
		t.Errorf("%q", m)
	}
}

// M1: the remote-helper promisor. Only GIT_ALLOW_PROTOCOL stops it when
// GIT_NO_LAZY_FETCH is unavailable (Git 2.39.5-2.43), so it is load-bearing.
type noLazyRunner struct{ inner workspacecfg.ExecRunner }

func (n noLazyRunner) Run(ctx context.Context, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	var env []string
	for _, kv := range c.Env {
		if !strings.HasPrefix(kv, "GIT_NO_LAZY_FETCH=") {
			env = append(env, kv)
		}
	}
	c.Env = env
	return n.inner.Run(ctx, c)
}

func helperPromisorClone(t *testing.T, marker string) (dir, helperDir string) {
	t.Helper()
	dir = partialClone(t, marker)
	helperDir = t.TempDir()
	script := "#!/bin/sh\ntouch '" + marker + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(helperDir, "git-remote-foo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "", "config", "remote.origin.url", "foo::x")
	runGit(t, dir, "", "config", "protocol.foo.allow", "always")
	return dir, helperDir
}

func TestValidateRemoteHelperPromisorNeedsAllowProtocol(t *testing.T) {
	realGit(t)
	marker := filepath.Join(t.TempDir(), "MARKER")
	dir, helperDir := helperPromisorClone(t, marker)
	t.Setenv("PATH", helperDir+":"+os.Getenv("PATH"))
	// Control: an unhardened Git runs the helper for the missing blob.
	ctl := exec.Command("git", "cat-file", "blob", "HEAD:.gitmodules")
	ctl.Dir, ctl.Env = dir, append(hermeticEnv(t.TempDir()), "PATH="+os.Getenv("PATH"))
	_ = ctl.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("control: the helper did not run: %v", err)
	}
	os.Remove(marker)
	dir, _ = helperPromisorClone(t, marker)
	r := validate(t, dir, workspacecfg.Options{Git: noLazyRunner{}})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the remote helper ran although GIT_NO_LAZY_FETCH was absent")
	}
	if errorCodes(r) != "git.listing_failed" {
		t.Errorf("%+v", r.Findings)
	}
}

// M6: a real listener as the promisor URL must never be contacted.
func TestValidateNeverConnectsToAListener(t *testing.T) {
	realGit(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			c.Close()
			accepted <- struct{}{}
		}
	}()
	marker := filepath.Join(t.TempDir(), "MARKER")
	dir := partialClone(t, marker)
	runGit(t, dir, "", "config", "remote.origin.url", "http://"+l.Addr().String()+"/x")
	runGit(t, dir, "", "config", "protocol.http.allow", "always")
	r := validate(t, dir, workspacecfg.Options{})
	l.Close()
	select {
	case <-accepted:
		t.Fatal("validate connected to the listener")
	default:
	}
	if errorCodes(r) != "git.listing_failed" {
		t.Errorf("%+v", r.Findings)
	}
}
