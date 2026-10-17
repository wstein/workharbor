// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// Bounds of the Git stage. The listing cap keeps memory far below the 256 MiB
// run bound even for a hostile index.
const (
	MaxListingBytes   = 4 << 20
	MaxListingEntries = 10000
	maxStderr         = 4 << 10
)

// Minimum Git version (owner-confirmed threshold, H77 (6)).
const minGitMajor, minGitMinor, minGitPatch = 2, 39, 5

// ErrGitShim marks the macOS /usr/bin/git stub that would open the Command Line
// Tools installer; it is reported as unavailable without ever executing it.
var ErrGitShim = errors.New("git is the macOS installer shim")

// GitCmd is one fixed Git invocation: absolute binary, full argument vector,
// a fresh environment, a neutral working directory and bounds.
type GitCmd struct {
	Path      string
	Args      []string
	Env       []string
	Dir       string
	MaxOutput int           // bytes of standard output kept; more sets Truncated
	Timeout   time.Duration // the remainder of the shared budget
}

// GitOut is the outcome of a Git invocation.
type GitOut struct {
	Stdout, Stderr []byte
	Exit           int
	Truncated      bool
}

// GitRunner runs Git. The production runner is ExecRunner; tests inject fakes.
// A non-nil error means Git did not run to completion (start failure, timeout
// or cancellation); a non-zero exit is reported in GitOut.
type GitRunner interface {
	Run(ctx context.Context, c GitCmd) (GitOut, error)
}

// ExecRunner executes the Git binary with os/exec.
type ExecRunner struct{}

// Run executes c with its own deadline, an empty standard input and capped
// output. The process (group) is killed on timeout, cancellation or overflow.
func (ExecRunner) Run(ctx context.Context, c GitCmd) (GitOut, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Env = c.Env
	cmd.Dir = c.Dir
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond
	var errBuf limitedBuffer
	errBuf.limit = maxStderr
	cmd.Stderr = &errBuf
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return GitOut{}, err
	}
	if err := cmd.Start(); err != nil {
		return GitOut{}, err
	}
	out, readErr := io.ReadAll(io.LimitReader(pipe, int64(c.MaxOutput)+1))
	truncated := len(out) > c.MaxOutput
	if truncated {
		out = out[:c.MaxOutput]
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	waitErr := cmd.Wait()
	res := GitOut{Stdout: out, Stderr: errBuf.Bytes(), Truncated: truncated}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, ctxErr
	}
	var ee *exec.ExitError
	switch {
	case truncated:
		res.Exit = -1
	case errors.As(waitErr, &ee):
		res.Exit = ee.ExitCode()
	case waitErr != nil:
		return res, waitErr
	case readErr != nil:
		return res, readErr
	}
	return res, nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

// gitProtocols are the transports whose use is denied with -c overrides.
var gitProtocols = []string{"file", "git", "ssh", "http", "https", "ext"}

// gitEnv builds the environment from scratch: only PATH is inherited (to find
// helper programs of Git itself), no GIT_* variable of the caller, no system or
// global configuration, no pager, no prompt, no optional lock, no lazy fetch and
// a protocol allow-list that matches nothing.
func gitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=/nonexistent",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_NO_LAZY_FETCH=1",
		"GIT_PAGER=cat",
		"GIT_ALLOW_PROTOCOL=workharbor-no-protocol",
	}
}

// gitArgs prepends the hardening to a subcommand: explicit git directory and
// work tree (no discovery), no pager, locks or replace refs, and -c overrides
// of core.fsmonitor, core.hooksPath and every transport (a per-protocol setting
// of the hostile .git/config would otherwise override protocol.allow).
func gitArgs(gitDir, workTree string, sub ...string) []string {
	args := []string{
		"--no-pager", "--no-optional-locks", "--no-replace-objects",
		"--git-dir=" + gitDir, "--work-tree=" + workTree,
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "protocol.allow=never",
	}
	for _, p := range gitProtocols {
		args = append(args, "-c", "protocol."+p+".allow=never")
	}
	return append(args, sub...)
}

var gitVersion = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:\.(\d+))?`)

// gitVersionOK parses the output of `git --version` and compares it with the
// minimum version.
func gitVersionOK(out []byte) bool {
	m := gitVersion.FindSubmatch(out)
	if m == nil {
		return false
	}
	num := func(b []byte) int {
		if len(b) == 0 {
			return 0
		}
		n, err := strconv.Atoi(string(b))
		if err != nil {
			return -1
		}
		return n
	}
	v := [3]int{num(m[1]), num(m[2]), num(m[3])}
	want := [3]int{minGitMajor, minGitMinor, minGitPatch}
	for i := range v {
		if v[i] < 0 {
			return false
		}
		if v[i] != want[i] {
			return v[i] > want[i]
		}
	}
	return true
}

// IsMacOSInstallerShim reports whether path is /usr/bin/git on macOS without a
// developer directory behind it: executing it would open the installer.
func IsMacOSInstallerShim(goos, path string, exists func(string) bool) bool {
	return goos == "darwin" && path == "/usr/bin/git" &&
		!exists("/Library/Developer/CommandLineTools/usr/bin/git") &&
		!exists("/Applications/Xcode.app/Contents/Developer/usr/bin/git")
}

// lookGit finds the Git binary. A relative PATH hit (exec.ErrDot) counts as
// absent, and the macOS shim is refused without being executed.
func lookGit() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if IsMacOSInstallerShim(runtime.GOOS, path, exists) {
		return "", ErrGitShim
	}
	return path, nil
}
