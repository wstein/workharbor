// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxOutput = 4 << 20

// Client only runs the two fixed, read-only discovery operations. Executable is
// injectable for offline tests; an empty value resolves the installed Docker CLI.
// A fresh empty Docker config directory prevents loading host credentials,
// credential helpers, contexts or CLI plugin settings. It is removed on return.
type Client struct{ Executable string }

func (c Client) read(ctx context.Context, endpoint string, args ...string) ([]byte, error) {
	if !strings.HasPrefix(endpoint, "unix://") || !filepath.IsAbs(strings.TrimPrefix(endpoint, "unix://")) {
		return nil, errors.New("local Unix endpoint required")
	}
	if !discoveryArgs(args) {
		return nil, errors.New("unsupported discovery operation")
	}
	exe := c.Executable
	if exe == "" {
		var err error
		exe, err = exec.LookPath("docker")
		if err != nil {
			return nil, errors.New("Docker CLI unavailable")
		}
	}
	config, err := os.MkdirTemp("", "whr-docker-config-")
	if err != nil {
		return nil, errors.New("cannot isolate Docker configuration")
	}
	defer os.RemoveAll(config)
	childctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childctx, exe, append([]string{"--host=" + endpoint}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "DOCKER_CONFIG=" + config}
	cmd.WaitDelay = 200 * time.Millisecond
	cleanup := isolate(cmd)
	defer cleanup()
	out := &boundedOutput{cancel: cancel}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if out.exceeded {
		return nil, errors.New("Docker response exceeds discovery limit")
	}
	if err != nil {
		return nil, errors.New("Docker discovery command failed")
	}
	return out.buf.Bytes(), nil
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
