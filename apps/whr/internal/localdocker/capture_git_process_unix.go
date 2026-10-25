// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"time"
)

type stageGitTool struct{ Path, Digest, SearchPath, Version string }
type stageGitToolKey struct{}

func pinStageGit(ctx context.Context) (context.Context, stageGitTool, error) {
	if tool, ok := ctx.Value(stageGitToolKey{}).(stageGitTool); ok {
		physical, e := filepath.EvalSymlinks(tool.Path)
		if e != nil || physical != tool.Path {
			return ctx, tool, stageConflict("Git tool identity changed")
		}
		digest, e := stageGitToolDigest(ctx, tool.Path)
		if e != nil || digest != tool.Digest {
			return ctx, tool, stageConflict("Git tool identity changed")
		}
		return ctx, tool, nil
	}
	name, err := exec.LookPath("git")
	if err != nil {
		return ctx, stageGitTool{}, stageConflict("Git tool unavailable")
	}
	name, err = filepath.EvalSymlinks(name)
	if err != nil {
		return ctx, stageGitTool{}, stageConflict("Git tool identity unavailable")
	}
	tool := stageGitTool{Path: name, SearchPath: os.Getenv("PATH")}
	tool.Digest, err = stageGitToolDigest(ctx, name)
	if err != nil {
		return ctx, tool, err
	}
	home, e := os.MkdirTemp("", "whr-git-tool-")
	if e != nil {
		return ctx, tool, stageConflict("Git tool probe storage unavailable")
	}
	defer os.RemoveAll(home)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, tool.Path, "--version")
	cmd.Env = stageGitEnv(home, "", "", false)
	cmd.Dir = home
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	var version bytes.Buffer
	cmd.Stdout = &stageBoundedWriter{Writer: &version, Remaining: 1 << 20}
	cmd.Stderr = io.Discard
	if e = runStageGit(cmd); e != nil {
		return ctx, tool, stageConflict("Git version probe failed")
	}
	tool.Version = strings.TrimSpace(version.String())
	fields := strings.Fields(tool.Version)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return ctx, tool, stageConflict("Git version identity unsupported")
	}
	parts := strings.Split(fields[2], ".")
	numbers := []int{0, 0, 0}
	for i := 0; i < 3 && i < len(parts); i++ {
		text := parts[i]
		j := 0
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		if j == 0 {
			return ctx, tool, stageConflict("Git version identity unsupported")
		}
		numbers[i], _ = strconv.Atoi(text[:j])
	}
	if numbers[0] < 2 || numbers[0] == 2 && (numbers[1] < 39 || numbers[1] == 39 && numbers[2] < 5) {
		return ctx, tool, stageConflict("Git 2.39.5 or newer required")
	}
	return context.WithValue(ctx, stageGitToolKey{}, tool), tool, nil
}
func stageGitToolDigest(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", stageConflict("Git tool identity unavailable")
	}
	f := os.NewFile(uintptr(fd), name)
	if err != nil {
		return "", stageConflict("Git tool identity unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", stageConflict("Git tool identity unsupported")
	}
	h := sha256.New()
	if _, err = io.Copy(h, stageContextReader{ctx: ctx, Reader: f}); err != nil {
		return "", stageConflict("Git tool identity unreadable")
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
func stageGitCommand(ctx context.Context, home, gitdir, objects, index string, transport bool, args ...string) *exec.Cmd {
	ctx, tool, err := pinStageGit(ctx)
	prefix := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "core.commitGraph=false", "-c", "pack.useBitmaps=false"}
	if gitdir != "" {
		prefix = append(prefix, "--git-dir="+gitdir)
	}
	cmd := exec.CommandContext(ctx, tool.Path, append(prefix, args...)...)
	cmd.Env = stageGitEnv(home, objects, index, transport)
	for i, v := range cmd.Env {
		if len(v) >= 5 && v[:5] == "PATH=" {
			cmd.Env[i] = "PATH=" + tool.SearchPath
		}
	}
	cmd.Dir = home
	cmd.WaitDelay = 2 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err == nil {
		var current string
		current, err = stageGitToolDigest(ctx, tool.Path)
		if err == nil && current != tool.Digest {
			err = stageConflict("Git tool identity changed")
		}
	}
	cmd.Err = err
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		return e
	}
	return cmd
}
func runStageGit(cmd *exec.Cmd) error {
	if !stageWaitSupported() {
		return stageConflict("Git wait-without-reap capability unavailable")
	}
	group := &stageProcessGroup{owned: true, signal: func(sig syscall.Signal) error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, sig)
	}}
	cmd.Cancel = group.terminate
	if err := cmd.Start(); err != nil {
		group.releaseBeforeReap(false)
		return err
	}
	observed := stageWaitLeader(cmd.Process.Pid)
	group.releaseBeforeReap(observed == nil)
	err := cmd.Wait()
	if observed != nil {
		return stageConflict("Git process ownership observation failed")
	}
	return err
}

func stageGitRead(ctx context.Context, home, gitdir, objects, index string, input []byte, args ...string) ([]byte, error) {
	return stageGitReadLimit(ctx, home, gitdir, objects, index, input, 4<<20, args...)
}
func stageGitReadLimit(ctx context.Context, home, gitdir, objects, index string, input []byte, limit int64, args ...string) ([]byte, error) {
	cmd := stageGitCommand(ctx, home, gitdir, objects, index, false, args...)
	cmd.Stdin = bytes.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &stageBoundedWriter{Writer: &out, Remaining: limit}
	cmd.Stderr = io.Discard
	if err := runStageGit(cmd); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, stageConflict("Git bounded reader failed: " + args[0])
	}
	return out.Bytes(), nil
}
