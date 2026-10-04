package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/gittest"
	"github.com/wstein/workharbor/internal/hostgit"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/runtime/runtimetest"
)

// checkRig is a publish rig whose Checks is the real RepoChecker, over the
// fake runtime: the guest's answer to the check script is the test's.
type checkRig struct {
	*pubRig
	chk *RepoChecker

	mu      sync.Mutex
	checks  []runtime.ExecRequest // the check script's requests
	bundle  []byte                // what the guest received on stdin
	reaps   int
	onCheck func(req runtime.ExecRequest) (out string, code int)
	reapOK  bool // the check's process is gone when asked
	busyErr error
}

func newCheckRig(t *testing.T) *checkRig {
	t.Helper()
	c := &checkRig{pubRig: newPubRig(t, withAgent("a1")), reapOK: true}
	c.onCheck = func(runtime.ExecRequest) (string, int) { return "ok\n", 0 }
	c.chk = NewRepoChecker(c.svc, CheckConfig{
		Repo:    c.repo,
		Command: func(string) string { return "make check" },
	})
	c.pub.cfg.Checks = c.chk.Check
	fake := c.rt.Adapter.(*runtimetest.Fake)
	fake.OnExecReq = func(_ string, req runtime.ExecRequest) ([]byte, string, int, bool) {
		if len(req.Cmd) < 4 || req.Cmd[0] != "sh" || req.Cmd[3] != "whr-check" {
			return nil, "", 0, false
		}
		switch req.Cmd[2] {
		case checkScript:
			var in []byte
			if req.Stdin != nil {
				in, _ = io.ReadAll(req.Stdin)
			}
			c.mu.Lock()
			c.checks = append(c.checks, req)
			c.bundle = in
			c.mu.Unlock()
			// While the check runs, a run start in this environment is refused.
			c.busyErr = c.svc.checkEnvFree(bg, c.env, "")
			out, code := c.onCheck(req)
			return []byte(out), "", code, true
		case reapScript:
			c.mu.Lock()
			c.reaps++
			ok := c.reapOK
			c.mu.Unlock()
			if !ok {
				return nil, "", 1, true
			}
			return nil, "", 0, true
		}
		return nil, "", 0, false
	}
	return c
}

func TestTheCheckSeesThePreparedCommitAndNoCredential(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	prepared, err := c.pub.Prepare(bg, c.req)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(c.checks) != 1 {
		t.Fatalf("%d checks ran, want 1", len(c.checks))
	}
	req := c.checks[0]
	// sh -c <script> whr-check <dir> <agent worktree> <sha> <ref> <command>
	dir, wt, sha, ref, command := req.Cmd[4], req.Cmd[5], req.Cmd[6], req.Cmd[7], req.Cmd[8]
	if sha != prepared.SHA || command != "make check" || ref != "refs/whr/bundle/"+sha {
		t.Errorf("args = %q", req.Cmd[4:])
	}
	if wt != c.agent.Worktree || dir == wt || !strings.HasPrefix(dir, "/tmp/whr-check-") {
		t.Errorf("the check worktree %q must be under its own directory, not the agent's %q", dir, wt)
	}
	for _, e := range req.Env {
		if !strings.HasPrefix(e, "GIT_CONFIG_") {
			t.Errorf("the check's environment holds %q: only git's settings may be there", strings.SplitN(e, "=", 2)[0])
		}
	}
	for _, a := range req.Cmd {
		if strings.Contains(strings.ToLower(a), "token") || strings.Contains(a, "/dev/fd/") {
			t.Errorf("a credential-looking argument: %q", a)
		}
	}
	// the prepared commits arrived as a bundle the agent's clone can take: its
	// prerequisite is a commit it holds
	if !bytes.HasPrefix(c.bundle, []byte("# v2 git bundle\n")) && !bytes.HasPrefix(c.bundle, []byte("# v3 git bundle\n")) {
		t.Fatalf("stdin is not a bundle: %q", c.bundle[:min(30, len(c.bundle))])
	}
	file := filepath.Join(t.TempDir(), "in.bundle")
	if err := os.WriteFile(file, c.bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := c.guestGit("bundle", "verify", file); err != nil {
		t.Errorf("the agent's clone cannot take the bundle: %v\n%s", err, out)
	}
	if !bytes.Contains(c.bundle, []byte(sha)) {
		t.Errorf("the bundle does not carry %s", sha)
	}
	// busy while it ran, free afterwards
	var conf *domain.ConflictError
	if !errors.As(c.busyErr, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("a run start during the check: %v, want environment busy", c.busyErr)
	}
	if err := c.svc.checkEnvFree(bg, c.env, ""); err != nil {
		t.Errorf("a run start after the check: %v", err)
	}
}

func TestACheckThatFailsIsAnErrorWithItsOutputAsData(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	long := strings.Repeat("noise line\n", 20_000) // 220 kB
	c.onCheck = func(runtime.ExecRequest) (string, int) {
		return long + "last \x1b[31mred\x1b[0m line\n", 3
	}
	_, err := c.pub.Prepare(bg, c.req)
	var ce *CheckError
	if !errors.As(err, &ce) || ce.Code != 3 || ce.TimedOut {
		t.Fatalf("err = %v, want a CheckError with status 3", err)
	}
	if len(ce.Output) > CheckOutputTail {
		t.Errorf("the output is %d bytes, over the %d cap", len(ce.Output), CheckOutputTail)
	}
	if !strings.HasSuffix(ce.Output, "last [31mred[0m line\n") || strings.ContainsRune(ce.Output, 0x1b) {
		t.Errorf("the tail must end the output with control characters dropped: %q", ce.Output[len(ce.Output)-40:])
	}
	if strings.Contains(err.Error(), "noise") || strings.Contains(err.Error(), "red") {
		t.Errorf("the error text carries the output: %v", err)
	}
	if err := c.svc.checkEnvFree(bg, c.env, ""); err != nil {
		t.Errorf("the environment stays busy after a failed check: %v", err)
	}
}

func TestACheckThatTimesOutIsEndedAndItsLeftoversReaped(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.chk.cfg.Timeout = 30 * time.Millisecond
	c.onCheck = func(runtime.ExecRequest) (string, int) { time.Sleep(300 * time.Millisecond); return "late\n", 0 }
	_, err := c.pub.Prepare(bg, c.req)
	var ce *CheckError
	if !errors.As(err, &ce) || !ce.TimedOut {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if c.reaps != 1 {
		t.Errorf("%d reaps, want 1: the process is looked for after a timeout", c.reaps)
	}
	if c.envState() != domain.EnvRunning {
		t.Errorf("the environment is %s: it stops only when the process is still there", c.envState())
	}
}

func TestAProcessThatSurvivesATimeoutStopsTheEnvironment(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.reapOK = false
	c.chk.cfg.Timeout = 30 * time.Millisecond
	c.onCheck = func(runtime.ExecRequest) (string, int) { time.Sleep(300 * time.Millisecond); return "", 0 }
	if _, err := c.pub.Prepare(bg, c.req); err == nil {
		t.Fatal("Prepare passed")
	}
	if got := c.envState(); got != domain.EnvStopped {
		t.Errorf("the environment is %s, want stopped: ending the exec client does not end the guest process", got)
	}
}

func TestWithNoCheckConfiguredThePrepareFailsClosed(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.chk.cfg.Command = func(string) string { return "" }
	_, err := c.pub.Prepare(bg, c.req)
	if !errors.Is(err, ErrNoChecks) {
		t.Fatalf("err = %v, want ErrNoChecks", err)
	}
	if len(c.checks) != 0 {
		t.Error("something ran")
	}
}

// Where the command comes from, in the order of D51.
func TestTheCheckCommandIsChosenInTheOrderOfD51(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	root := t.TempDir()
	g, err := hostgit.New(hostgit.WithWorkspaceRoot(root))
	must(t, err)
	t.Cleanup(func() { _ = g.Close() })
	work := filepath.Join(root, "work")
	must(t, os.MkdirAll(work, 0o750))
	plainGit(t, c.home, work, "init", "--quiet", "-b", "main")
	commit := func(files map[string]string) {
		for name, body := range files {
			p := filepath.Join(work, name)
			must(t, os.MkdirAll(filepath.Dir(p), 0o750))
			must(t, os.WriteFile(p, []byte(body), 0o600))
			plainGit(t, c.home, work, "add", name)
		}
		plainGit(t, c.home, work, "commit", "--quiet", "--allow-empty", "-m", "c")
	}
	bare, err := g.InitBare(bg, filepath.Join(root, "mirror.git"))
	must(t, err)
	publish := func() {
		plainGit(t, c.home, work, "push", "--quiet", "--force", bare.Path(), "main:refs/heads/main")
	}
	src := func(context.Context, string) (CheckSource, error) {
		return CheckSource{Git: bare, Ref: "refs/heads/main"}, nil
	}
	chk := NewRepoChecker(c.svc, CheckConfig{Repo: c.repo, Command: func(string) string { return "" }, Source: src})
	want := func(label, cmd string, no bool) {
		t.Helper()
		got, err := chk.Command(bg, "wstein/workharbor")
		switch {
		case no && !errors.Is(err, ErrNoChecks):
			t.Errorf("%s: %q, %v, want ErrNoChecks", label, got, err)
		case !no && (err != nil || got != cmd):
			t.Errorf("%s: %q, %v, want %q", label, got, err, cmd)
		}
	}

	commit(map[string]string{"README.md": "x"})
	publish()
	want("nothing configured", "", true)

	commit(map[string]string{".pre-commit-config.yaml": "repos: []\n"})
	publish()
	want("pre-commit", PreCommitCheck, false)

	commit(map[string]string{".devcontainer/devcontainer.json": `{"image":"x","customizations":{"workharbor":{"check":"make verify"}}}`})
	publish()
	want("the repository's own check beats pre-commit", "make verify", false)

	chk.cfg.Command = func(repo string) string {
		if repo != "wstein/workharbor" {
			t.Errorf("asked for %q", repo)
		}
		return " just check "
	}
	want("the supervisor's check beats the repository's", "just check", false)

	// a devcontainer.json that cannot be trusted is an error, never "no check"
	chk.cfg.Command = func(string) string { return "" }
	commit(map[string]string{".devcontainer/devcontainer.json": `{"image":"x","privileged":true}`})
	publish()
	if _, err := chk.Command(bg, "wstein/workharbor"); err == nil || errors.Is(err, ErrNoChecks) {
		t.Errorf("a refused devcontainer.json: %v, want an error that is not ErrNoChecks", err)
	}
}

func TestAHoldRefusesARunStartAndIsCounted(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	ws, err := c.store.Workspace(bg, "w1")
	must(t, err)
	r1, err := c.svc.HoldEnvironment(bg, ws)
	must(t, err)
	r2, err := c.svc.HoldEnvironment(bg, ws) // the prepare holds, and so does its check
	must(t, err)
	r2()
	r2() // idempotent
	var conf *domain.ConflictError
	if err := c.svc.checkEnvFree(bg, c.env, ""); !errors.As(err, &conf) || conf.Rule != domain.RuleEnvBusy {
		t.Errorf("held by the outer hold: %v", err)
	}
	r1()
	if err := c.svc.checkEnvFree(bg, c.env, ""); err != nil {
		t.Errorf("released: %v", err)
	}
}

type failStopRuntime struct {
	runtimeAdapter
	err error
}

func (f failStopRuntime) Stop(context.Context, string) error { return f.err }

func TestACheckThatPassesHasItsProcessGroupEnded(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	if _, err := c.pub.Prepare(bg, c.req); err != nil {
		t.Fatal(err)
	}
	if c.reaps != 1 {
		t.Errorf("%d reaps, want 1: a background process of a check that exited normally must be ended", c.reaps)
	}
}

func TestAFailedStopAfterACheckForgetsTheStartMark(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	c.reapOK = false
	var reported []error
	c.svc.cfg.OnError = func(err error) { reported = append(reported, err) }
	c.svc.rt = failStopRuntime{runtimeAdapter: c.svc.rt, err: errors.New("stop refused")}
	c.svc.markEnvStarted(c.env)
	_, _ = c.pub.Prepare(bg, c.req)
	if got := c.envState(); got != domain.EnvRunning {
		t.Errorf("the environment is %s: the stop failed", got)
	}
	if c.svc.envStarted(c.env) {
		t.Error("the environment is still marked as started by this process: the next launch would skip the stop and start beside the surviving check")
	}
	if len(reported) == 0 {
		t.Error("the failed stop is not reported")
	}
	if c.svc.freshMu.TryLock() {
		c.svc.freshMu.Unlock()
	} else {
		t.Error("freshMu is still held")
	}
}

func TestACheckSafeguardsUntrustedText(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	in := "a\u202eb\u2028c\u2029d\u2066e\u009bf\x1bg\th\ni"
	if got, want := c.chk.untrusted(in), "abcdefg\th\ni"; got != want {
		t.Errorf("untrusted = %q, want %q", got, want)
	}
}

func TestTheGuestBaseErrorCarriesNoControlCharacters(t *testing.T) {
	t.Parallel()
	c := newCheckRig(t)
	fake := c.rt.Adapter.(*runtimetest.Fake)
	prev := fake.OnExecReq
	fake.OnExecReq = func(env string, req runtime.ExecRequest) ([]byte, string, int, bool) {
		if len(req.Cmd) > 3 && req.Cmd[0] == "git" && req.Cmd[3] == "merge-base" {
			return []byte("fatal: \x1b[31mbad\u202e path\n"), "", 128, true
		}
		return prev(env, req)
	}
	_, err := c.pub.Prepare(bg, c.req)
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\u202e") {
		t.Errorf("err = %q", err)
	}
	if err == nil || !strings.Contains(err.Error(), `\x1b`) {
		t.Errorf("the escape is not visible: %v", err)
	}
}

// The reap script, in the shells a guest has: dash (Debian, Ubuntu) and bash.
func TestTheReapScriptEndsTheGroupInDashAndBash(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"dash", "bash", "sh"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			d := filepath.Join(dir, "chk")
			// The check: leader of its own group, leaves a background process, exits 0.
			lead := exec.CommandContext(bg, path, "-c", `echo $$ > "$1.pid"; sleep 77 >/dev/null 2>&1 & echo $! > "$1.bg"; exit 0`, "x", d) //nolint:gosec // a test shell found by LookPath
			lead.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			env := gittest.Env(t.TempDir())
			lead.Env = env
			if err := lead.Run(); err != nil {
				t.Fatal(err)
			}
			pidFile, err := os.ReadFile(d + ".bg") //nolint:gosec // a path in the test's temp dir
			if err != nil {
				t.Fatal(err)
			}
			pid, _ := strconv.Atoi(strings.TrimSpace(string(pidFile)))
			if syscall.Kill(pid, 0) != nil {
				t.Fatalf("the background process %d is not running", pid)
			}
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			reap := exec.CommandContext(bg, path, "-c", reapScript, "whr-check", d, dir) //nolint:gosec // a test shell found by LookPath
			reap.Env = env
			out, err := reap.CombinedOutput()
			if err != nil {
				t.Fatalf("reap: %v: %s", err, out)
			}
			for i := 0; i < 50 && syscall.Kill(pid, 0) == nil; i++ {
				time.Sleep(20 * time.Millisecond)
			}
			if syscall.Kill(pid, 0) == nil {
				t.Errorf("the background process %d survived the reap", pid)
			}
			if _, err := os.Stat(d + ".pid"); err == nil {
				t.Error("the pid file is not removed")
			}
		})
	}
}
