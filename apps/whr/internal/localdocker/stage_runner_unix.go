// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type stageToolIdentity struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}
type stageRunner struct {
	Policy         *stageDriverPolicy
	Root, Endpoint string
	Tools          map[string]stageToolIdentity
	Environment    []string
}
type stagePhaseOutput struct {
	Stdout, Stderr []byte
	OutputDigest   string
	Exit           int
}

func stageFileDigest(ctx context.Context, path string) (string, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return "", stageConflict("tool executable unavailable")
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || !before.Mode().IsRegular() || before.Mode()&0111 == 0 {
		return "", stageConflict("tool executable type or mode")
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, stageContextReader{ctx: ctx, Reader: f}); e != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", stageConflict("tool executable unreadable")
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		return "", stageConflict("tool executable changed during admission")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
func newStageRunner(ctx context.Context, root, endpoint string, paths map[string]string) (*stageRunner, error) {
	st, e := os.Lstat(root)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
		return nil, stageConflict("phase storage must be private")
	}
	if !strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\x00\r\n") {
		return nil, stageConflict("phase endpoint must be admitted Unix socket")
	}
	r := &stageRunner{Root: root, Endpoint: endpoint, Tools: map[string]stageToolIdentity{}}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		switch name {
		case "docker", "devcontainer", "node", "git", "bash", "sh":
		default:
			return nil, stageConflict("phase tool name unsupported")
		}
		path, e := filepath.EvalSymlinks(paths[name])
		if e != nil || !filepath.IsAbs(path) {
			return nil, stageConflict("phase tool path unavailable")
		}
		digest, e := stageFileDigest(ctx, path)
		if e != nil {
			return nil, e
		}
		r.Tools[name] = stageToolIdentity{Path: path, Digest: digest}
	}
	for _, name := range []string{"home", "tmp", "cache", "docker-config", "user-data", "driver"} {
		if e = os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			return nil, stageConflict("phase private directory unavailable")
		}
	}
	r.Environment = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + filepath.Join(root, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "DOCKER_CONFIG=" + filepath.Join(root, "docker-config"), "DOCKER_HOST=" + endpoint}
	for _, name := range []string{"LANG", "LC_ALL", "TZ", "ANCHORAGE_CONTAINER_NAME", "ANCHORAGE_CONTAINER_HOSTNAME", "ANCHORAGE_ROLE", "ANCHORAGE_HOST_LABEL", "ANCHORAGE_HOST_IP"} {
		if value, ok := os.LookupEnv(name); ok {
			if len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
				return nil, stageConflict("nonsecret substitution value unsupported")
			}
			r.Environment = append(r.Environment, name+"="+value)
		}
	}
	return r, nil
}
func (r *stageRunner) Run(parent context.Context, phase, tool string, args []string, stdin io.Reader, budget time.Duration) (stagePhaseOutput, error) {
	var result stagePhaseOutput
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	if e := ctx.Err(); e != nil {
		return result, e
	}
	identity, ok := r.Tools[tool]
	if !ok {
		return result, stageConflict("phase tool was not admitted")
	}
	digest, e := stageFileDigest(ctx, identity.Path)
	if e != nil || digest != identity.Digest {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, stageConflict("tool identity changed before " + phase)
	}
	if tool == "docker" {
		args = append([]string{"--host", r.Endpoint}, args...)
	}
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()
	executable := identity.Path
	if tool == "devcontainer" {
		node, ok := r.Tools["node"]
		if !ok {
			return result, stageConflict("official CLI pinned Node missing")
		}
		if e = stageCheckNodeEntry(identity.Path); e != nil {
			return result, e
		}
		for _, name := range []string{"node", "docker", "sh", "docker_driver"} {
			if dependency, ok := r.Tools[name]; ok {
				sum, err := stageFileDigest(ctx, dependency.Path)
				if err != nil || sum != dependency.Digest {
					if ctx.Err() != nil {
						return result, ctx.Err()
					}
					return result, stageConflict("tool identity changed before " + phase)
				}
			}
		}
		executable = node.Path
		args = append([]string{identity.Path}, args...)
	}
	cmd := exec.CommandContext(child, executable, args...)
	cmd.Dir = r.Root
	cmd.Env = append([]string(nil), r.Environment...)
	cmd.Stdin = stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	if !stageWaitSupported() {
		return result, stageConflict("native wait-without-reap capability unavailable")
	}
	group := &stageProcessGroup{owned: true, signal: func(sig syscall.Signal) error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, sig)
	}}
	cmd.Cancel = group.terminate
	limit := 1 << 20
	if phase == "metadata" {
		limit = 4 << 20
	}
	output := &stagePhaseBuffers{cancel: cancelChild, limit: limit}
	cmd.Stdout = stagePhaseWriter{output: output}
	cmd.Stderr = stagePhaseWriter{output: output, stderr: true}
	e = cmd.Start()
	if e == nil {
		waitErr := stageWaitLeader(cmd.Process.Pid)
		group.releaseBeforeReap(waitErr == nil)
		e = cmd.Wait()
		if waitErr != nil {
			return result, stageConflict("phase process ownership observation failed: " + phase)
		}
	} else {
		group.releaseBeforeReap(false)
	}
	result.Stdout = output.stdout.Bytes()
	result.Stderr = output.stderr.Bytes()
	hash := sha256.New()
	hash.Write(result.Stdout)
	hash.Write([]byte{0})
	hash.Write(result.Stderr)
	result.OutputDigest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if output.exceeded {
		return result, stageConflict("phase output quota exceeded: " + phase)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if e != nil {
		var ee *exec.ExitError
		if errors.As(e, &ee) {
			result.Exit = ee.ExitCode()
			if reason := stageDriverRefusal(result.Stderr); reason != "" {
				return result, stageConflict("Docker driver refused: " + reason)
			}
			return result, stageConflict("phase command failed: " + phase)
		}
		return result, stageConflict("phase command unavailable: " + phase)
	}
	return result, nil
}

type stagePhaseBuffers struct {
	limit          int
	mu             sync.Mutex
	stdout, stderr bytes.Buffer
	used           int
	exceeded       bool
	cancel         context.CancelFunc
}
type stagePhaseWriter struct {
	output *stagePhaseBuffers
	stderr bool
}

func (w stagePhaseWriter) Write(p []byte) (int, error) {
	b := w.output
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > b.limit-b.used {
		b.exceeded = true
		b.cancel()
		return 0, errors.New("phase output quota")
	}
	b.used += len(p)
	if w.stderr {
		return b.stderr.Write(p)
	}
	return b.stdout.Write(p)
}

type stageContextReader struct {
	ctx context.Context
	io.Reader
}

func (r stageContextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.Reader.Read(p)
}

func stageDriverRefusal(data []byte) string {
	for _, reason := range []string{"empty_vector", "routing_override", "unsupported_native_command", "protected_resource_mutation", "metadata_vector", "image_mutation", "context_mutation", "volume_mutation", "undeclared_initializer_volume", "pull_phase", "undeclared_pull_reference", "promote_build", "builder_mutation", "preview_mutation", "old_primary_target", "container_identity", "foreign_container_target", "exec_option", "exec_vector", "start_vector", "initializer_image_binding", "native_pull_override", "native_name", "native_mount", "native_run_option", "unsupported_native_run_option", "native_mount_binding", "native_image", "promote_image_identity", "undeclared_native_mount", "unbound_env_copy", "native_env_file"} {
		if bytes.Contains(data, []byte("whr-driver:"+reason+"\n")) {
			return reason
		}
	}
	return ""
}

func stageCheckNodeEntry(path string) error {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return stageConflict("official CLI Node entry unavailable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 256))
	if e != nil {
		return stageConflict("official CLI Node entry unreadable")
	}
	line, _, _ := bytes.Cut(b, []byte{'\n'})
	text := string(line)
	if !strings.HasPrefix(text, "#!") || !strings.HasSuffix(strings.TrimSpace(text), "node") {
		return stageConflict("devcontainer launcher is not an admitted Node entry")
	}
	return nil
}
