// Package gittest runs git and ssh-keygen for tests in isolation. A test that
// starts either with the developer's own environment can reach the system
// gitconfig's credential helper (osxkeychain on a Mac with Homebrew git), the SSH
// agent or the keychain, and with a changed HOME that ends in a "Keychain Not
// Found" dialog on the human's screen. Every test that spawns git or ssh-keygen
// uses this package, so none can forget the isolation (the same one hostgit uses).
package gittest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// Env is the whole environment of a git or ssh-keygen process: a minimal list,
// not os.Environ() with overrides. home is the HOME it sees, which must not be
// the human's; extra are added last (an author identity, for example).
func Env(home string, extra ...string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		// an explicit `git config --system` ignores GIT_CONFIG_NOSYSTEM, which is how
		// Homebrew's osxkeychain helper was reached: point the system file nowhere too
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/usr/bin/true",
		"SSH_ASKPASS=/usr/bin/true",
		"SSH_AUTH_SOCK=",
		"LC_ALL=C",
	}
	return append(env, extra...)
}

// Identity is an author and committer for commits a test makes.
var Identity = []string{
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.test",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.test",
}

// Git returns a git command that runs in dir with the isolated environment and
// no credential helper. home may be empty, which gives the process a private
// empty directory of its own that is never the human's.
func Git(ctx context.Context, home, dir string, extra []string, args ...string) *exec.Cmd {
	if home == "" {
		home = emptyHome()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "credential.helper="}, args...)...) //nolint:gosec // a fixed binary; the arguments are a test's own
	cmd.Dir = dir
	cmd.Env = Env(home, extra...)
	return cmd
}

// SSHKeygen returns an ssh-keygen command in the same isolation: no agent, no
// askpass, a private HOME.
func SSHKeygen(ctx context.Context, home string, args ...string) *exec.Cmd {
	if home == "" {
		home = emptyHome()
	}
	cmd := exec.CommandContext(ctx, "ssh-keygen", args...) //nolint:gosec // a fixed binary; the arguments are a test's own
	cmd.Env = Env(home)
	return cmd
}

// emptyHome makes a directory that holds nothing, so no tool finds a keychain,
// a key or a configuration there. It is removed with the process's temporary
// files by the operating system; a test that wants it cleaned passes its own.
func emptyHome() string {
	dir, err := os.MkdirTemp("", "gittest-home-")
	if err != nil {
		return filepath.Join(os.TempDir(), "gittest-no-home")
	}
	return dir
}
