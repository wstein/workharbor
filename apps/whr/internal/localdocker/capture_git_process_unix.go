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
	"syscall"
	"time"
)

type stageGitTool struct{ Path, Digest, SearchPath string }
type stageGitToolKey struct{}

func pinStageGit(ctx context.Context) (context.Context, stageGitTool, error) {
	if tool, ok := ctx.Value(stageGitToolKey{}).(stageGitTool); ok {
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
	tool.Digest, err = stageGitToolDigest(name)
	if err != nil {
		return ctx, tool, err
	}
	return context.WithValue(ctx, stageGitToolKey{}, tool), tool, nil
}
func stageGitToolDigest(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", stageConflict("Git tool identity unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 256<<20 {
		return "", stageConflict("Git tool identity unsupported")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, (256<<20)+1)); err != nil {
		return "", stageConflict("Git tool identity unreadable")
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
func stageGitCommand(ctx context.Context, home, gitdir, objects, index string, transport bool, args ...string) *exec.Cmd {
	ctx, tool, err := pinStageGit(ctx)
	prefix := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}
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
		current, err = stageGitToolDigest(tool.Path)
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
		time.AfterFunc(2*time.Second, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
		return e
	}
	return cmd
}
func runStageGit(cmd *exec.Cmd) error {
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	return cmd.Run()
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
