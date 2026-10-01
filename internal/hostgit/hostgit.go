// Package hostgit is the only way the host runs git on anything an agent can
// write. Agent-writable repositories are hostile input (design §4.5, §7.4):
// their config, hooks, attributes and file-system monitor can run commands on
// the host the moment plain git touches them, and no list of -c overrides can
// name every key that does.
//
// So the package works in two ways. FetchBranch copies one branch out of an
// agent's checkout into a bare repository the supervisor owns, and everything
// that changes history (cleanup, signing, push) runs only there, on a Repo.
// An Untrusted handle on the agent's checkout runs only read-only plumbing.
// Every command starts from an empty environment and a floor of overrides.
package hostgit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Errors returned for a request that hostgit refuses.
var (
	ErrNotAllowed = errors.New("git command not allowed in an agent-writable repository")
	ErrBadPath    = errors.New("path must be absolute and name an existing directory")
	ErrBadBranch  = errors.New("not a valid branch name")
)

// Git runs git with the hardening described in the package comment.
type Git struct {
	bin  string // absolute path of the git binary
	home string // an empty directory used as HOME and TMPDIR
}

// New finds git and prepares an empty HOME for it. Call Close when done.
func New() (*Git, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("hostgit: %w", err)
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return nil, fmt.Errorf("hostgit: %w", err)
	}
	home, err := os.MkdirTemp("", "hostgit-home-")
	if err != nil {
		return nil, fmt.Errorf("hostgit: %w", err)
	}
	return &Git{bin: bin, home: home}, nil
}

// Close removes the empty HOME.
func (g *Git) Close() error { return os.RemoveAll(g.home) }

// Env returns the whole environment git runs in. It starts empty: nothing is
// inherited from the host, so no GIT_* variable, askpass program, proxy or
// pager can reach git.
func (g *Git) Env() []string {
	return []string{
		"PATH=" + filepath.Dir(g.bin) + ":/usr/bin:/bin",
		"HOME=" + g.home,
		"TMPDIR=" + g.home,
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_PAGER=cat",
		"GIT_EDITOR=:",
	}
}

// Config returns the -c overrides every command starts with. They disable
// the keys known to run commands (hooks, the file-system monitor, the pager,
// the editor, credential helpers, signing, submodule recursion) and every
// transport, except the file transport when fileTransport is set. The list is
// a floor, not a fence: it cannot name filter or textconv drivers, which is
// why an agent's tree only ever sees read-only plumbing.
func (g *Git) Config(fileTransport bool) []string {
	file := "never"
	if fileTransport {
		file = "always"
	}
	kv := []string{
		"core.hooksPath=" + os.DevNull,
		"core.fsmonitor=false",
		"core.untrackedCache=false",
		"core.pager=cat",
		"core.editor=:",
		"core.askPass=" + os.DevNull,
		"credential.helper=",
		"commit.gpgSign=false",
		"tag.gpgSign=false",
		"protocol.allow=never",
		"protocol.file.allow=" + file,
		"submodule.recurse=false",
		"fetch.recurseSubmodules=false",
		"transfer.fsckObjects=true",
		"fetch.fsckObjects=true",
		"receive.fsckObjects=true",
	}
	args := make([]string, 0, 2*len(kv))
	for _, e := range kv {
		args = append(args, "-c", e)
	}
	return args
}

// command builds a git process: the floor of overrides, then args, in dir,
// with the hardened environment.
func (g *Git) command(ctx context.Context, dir string, fileTransport bool, args ...string) *exec.Cmd {
	argv := append(g.Config(fileTransport), args...)
	cmd := exec.CommandContext(ctx, g.bin, argv...) //nolint:gosec // g.bin is the git found by LookPath; args are built here, not taken from the repository
	cmd.Dir = dir
	cmd.Env = g.Env()
	return cmd
}

// run runs git with args in dir and returns its standard output. An error
// names the git command and its standard error, not the floor of overrides.
func (g *Git) run(ctx context.Context, dir string, fileTransport bool, args ...string) ([]byte, error) {
	cmd := g.command(ctx, dir, fileTransport, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func checkDir(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q", ErrBadPath, path)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %q", ErrBadPath, path)
	}
	return nil
}
