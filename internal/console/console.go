// Package console is the console environment of design D43: an environment
// without an agent, for the human's shell work across workspaces, so daily work
// never needs a login on the host. This file holds what ships in the binary: the
// image definitions for the two first-class bases, and the git wrapper that makes
// git safe to run in a workspace whose .git an agent wrote.
package console

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/wstein/workharbor/internal/baseimage"
)

//go:embed whr-git whr-sshd sshd_config Containerfile.fedora Containerfile.ubuntu
var files embed.FS

// GitWrapper is the script installed as /usr/local/bin/git in the console image.
// It runs the real git with hooks and fsmonitor off and every setting that runs
// a command overridden by name (see the script's own header).
func GitWrapper() []byte {
	b, err := files.ReadFile("whr-git")
	if err != nil {
		panic("console: the embedded git wrapper is missing: " + err.Error())
	}
	return b
}

// SSHD is the launcher of the console's sshd (installed as /usr/local/bin/whr-sshd).
func SSHD() []byte { return mustRead("whr-sshd") }

// SSHDConfig is the console's sshd configuration (installed as /etc/whr/sshd_config).
func SSHDConfig() []byte { return mustRead("sshd_config") }

func mustRead(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("console: the embedded " + name + " is missing: " + err.Error())
	}
	return b
}

// Containerfile returns the console image definition of a base.
func Containerfile(d baseimage.Distro) ([]byte, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("%w: %q", baseimage.ErrUnknownDistro, string(d))
	}
	return files.ReadFile("Containerfile." + string(d))
}

// Tag is the stable name of the console image of a base: a digest of the
// Containerfile (which holds the pinned digest of the stock image), of the git
// wrapper and of the sshd launcher and configuration, so it stays the same until either changes.
func Tag(d baseimage.Distro) (string, error) {
	cf, err := Containerfile(d)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(cf)
	h.Write([]byte{0})
	h.Write(GitWrapper())
	h.Write([]byte{0})
	h.Write(SSHD())
	h.Write([]byte{0})
	h.Write(SSHDConfig())
	return "whr-console/" + string(d) + ":" + hex.EncodeToString(h.Sum(nil))[:12], nil
}

// Ensure makes sure the console image of d exists and returns its tag, building
// it through the runtime's builder only when it is not there yet.
func Ensure(ctx context.Context, b baseimage.Builder, d baseimage.Distro, workDir string) (tag string, built bool, err error) {
	if tag, err = Tag(d); err != nil {
		return "", false, err
	}
	cf, err := Containerfile(d)
	if err != nil {
		return "", false, err
	}
	built, err = baseimage.EnsureImage(ctx, b, tag, cf, map[string][]byte{"whr-git": GitWrapper(), "whr-sshd": SSHD(), "sshd_config": SSHDConfig()}, filepath.Join(workDir, "console-"+string(d)))
	return tag, built, err
}
