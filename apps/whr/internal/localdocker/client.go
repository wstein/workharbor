// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxOutput = 4 << 20

// Client runs fixed Docker vectors, admitted separately for reads and removal. Executable is
// injectable for offline tests; an empty value resolves the installed Docker CLI.
// A fresh empty Docker config directory prevents loading host credentials,
// credential helpers, contexts or CLI plugin settings. It is removed on return.
type Client struct {
	Executable string
	// Progress receives the live output of a home copy (rsync progress). Nil discards it.
	Progress io.Writer
}

type readResult struct {
	stdout, stderr []byte
	status         int
}

func (c Client) read(ctx context.Context, endpoint string, args ...string) ([]byte, error) {
	result, err := c.runRead(ctx, endpoint, args...)
	if err != nil {
		return nil, err
	}
	if result.status != 0 {
		return nil, errors.New("Docker discovery command failed")
	}
	return result.stdout, nil
}

func (c Client) runRead(ctx context.Context, endpoint string, args ...string) (readResult, error) {
	if !strings.HasPrefix(endpoint, "unix://") || !filepath.IsAbs(strings.TrimPrefix(endpoint, "unix://")) {
		return readResult{}, errors.New("local Unix endpoint required")
	}
	shellInspect := len(args) == 5 && args[0] == "container" && args[1] == "inspect" && args[2] == "--format" && (args[3] == "{{json .}}" || args[3] == "{{.State.Running}}") && fullID(args[4])
	if !discoveryArgs(args) && !shellInspect {
		return readResult{}, errors.New("unsupported discovery operation")
	}
	return c.runFixed(ctx, endpoint, args...)
}

// runFixed is private to the operation-specific argv admission methods.
func (c Client) runFixed(ctx context.Context, endpoint string, args ...string) (readResult, error) {
	return c.run(ctx, endpoint, nil, nil, args...)
}

// run is runFixed with optional stdin and an optional live stdout writer that
// replaces the bounded capture (the result then has no stdout).
func (c Client) run(ctx context.Context, endpoint string, stdin io.Reader, stdout io.Writer, args ...string) (readResult, error) {
	exe := c.Executable
	if exe == "" {
		var err error
		exe, err = exec.LookPath("docker")
		if err != nil {
			return readResult{}, errors.New("Docker CLI unavailable")
		}
	}
	env, cleanEnv, err := ClientEnvironment()
	if err != nil {
		return readResult{}, err
	}
	defer cleanEnv()
	childctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childctx, exe, append([]string{"--host=" + endpoint}, args...)...)
	cmd.Env = env
	cmd.WaitDelay = 200 * time.Millisecond
	cleanup := isolate(cmd)
	defer cleanup()
	out, diagnostic := &boundedOutput{cancel: cancel}, &boundedOutput{cancel: cancel}
	cmd.Stdin = stdin
	cmd.Stdout = out
	if stdout != nil {
		cmd.Stdout = stdout
	}
	cmd.Stderr = diagnostic
	err = cmd.Run()
	if ctx.Err() != nil {
		return readResult{}, ctx.Err()
	}
	if out.exceeded || diagnostic.exceeded {
		return readResult{}, errors.New("Docker response exceeds discovery limit")
	}
	status := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return readResult{}, errors.New("Docker discovery command failed")
		}
		status = exit.ExitCode()
	}
	return readResult{stdout: out.buf.Bytes(), stderr: diagnostic.buf.Bytes(), status: status}, nil
}

// Admit only fixed vectors, with validated full container IDs as inspect data.
func discoveryArgs(args []string) bool {
	if len(args) == 6 && strings.Join(args, "\x00") == "container\x00ls\x00--all\x00--no-trunc\x00--format\x00{{json .}}" {
		return true
	}
	if len(args) < 5 || args[0] != "container" || args[1] != "inspect" || args[2] != "--format" || args[3] != inspectFormat {
		return false
	}
	for _, id := range args[4:] {
		if !fullID(id) {
			return false
		}
	}
	return true
}

// A failed bounded write cancels the whole process group, rather than waiting
// for a child that ignores its closed stdout to reach the shared deadline.
type boundedOutput struct {
	buf      bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > maxOutput-b.buf.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errors.New("response too large")
	}
	return b.buf.Write(p)
}

// Deliberately omit Config.Env and all unrelated inspect fields.
const inspectFormat = `{"Id":{{json .Id}},"Name":{{json .Name}},"Image":{{json .Config.Image}},"State":{{json .State.Status}},"Labels":{{json .Config.Labels}},"Mounts":{{json .Mounts}}}`

func fullID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
