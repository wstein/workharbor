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
	"strconv"
	"strings"
)

// Env is the whole environment of a git or ssh-keygen process: a minimal list,
// not os.Environ() with overrides. home is the HOME it sees, which must not be
// the human's; extra are added last (an author identity, for example).
//
// Env panics (this is a test helper: a wrong call must fail loudly, never run
// git unguarded) when extra
//   - sets HOME, XDG_CONFIG_HOME, GIT_CONFIG_SYSTEM, GIT_CONFIG_NOSYSTEM,
//     GIT_CONFIG_PARAMETERS, GIT_TERMINAL_PROMPT, GIT_ASKPASS, SSH_ASKPASS or
//     SSH_AUTH_SOCK, or a GIT_CONFIG_GLOBAL outside home;
//   - has a GIT_CONFIG_COUNT that strconv.Atoi rejects or that is negative
//     (git accepts "" and " 1", so it cannot be left to reject them), or a
//     count without its KEY_n or VALUE_n;
//   - passes a config pair (GIT_CONFIG_KEY_n/VALUE_n) other than
//     credential.helper with an empty value, user.name or user.email (keys
//     compared case-insensitively). That is an allowlist: any other key can
//     load a file (include.path, includeIf.*.path) or run a program
//     (core.sshCommand, core.hooksPath, alias.*, filter.*, ...), and these are
//     the only pairs callers need (the helper switched off, an identity);
//   - has an entry without "=".
//
// Not enforced: environment variables other than those listed. A caller may
// still pass GIT_SSH_COMMAND, GIT_EXTERNAL_DIFF, GIT_PAGER, GIT_EDITOR, PATH,
// LD_PRELOAD and the like (the console tests pass EDITOR and VISUAL), and a
// GIT_CONFIG_GLOBAL inside home may name a file that itself holds any
// configuration, includes included. Env guards the human's keychain and
// identity, not against a test that sets out to run a program.
//
// git reads GIT_CONFIG_COUNT and its KEY_n/VALUE_n as one list, and a later
// duplicate replaces an earlier one, so a raw GIT_CONFIG_COUNT in extra would
// silently drop the user.useConfigOnly guard. Env therefore composes: the guard
// is entry 0 and the caller's own pairs follow it, renumbered.
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
	// never guess an identity from the login name and hostname: a Mac derives
	// one, a CI runner does not, so a test that forgot its identity passed here
	// and failed there (#198); now it fails everywhere
	pairs := []string{"user.useConfigOnly", "true"}
	extraPairs, rest := configPairs(home, extra)
	pairs = append(pairs, extraPairs...)
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(pairs)/2))
	for i := 0; i < len(pairs); i += 2 {
		n := strconv.Itoa(i / 2)
		env = append(env, "GIT_CONFIG_KEY_"+n+"="+pairs[i], "GIT_CONFIG_VALUE_"+n+"="+pairs[i+1])
	}
	return append(env, rest...)
}

// reserved are the variables Env sets itself and a caller may not change.
var reserved = map[string]bool{
	"HOME": true, "XDG_CONFIG_HOME": true, "GIT_CONFIG_SYSTEM": true, "GIT_CONFIG_NOSYSTEM": true,
	"GIT_CONFIG_PARAMETERS": true, "GIT_TERMINAL_PROMPT": true, "GIT_ASKPASS": true,
	"SSH_ASKPASS": true, "SSH_AUTH_SOCK": true,
}

// configPairs takes the GIT_CONFIG_COUNT/KEY_n/VALUE_n variables out of extra,
// returning their key and value pairs in order and the other variables in rest.
// It panics on anything that could weaken the isolation (see Env).
func configPairs(home string, extra []string) (pairs, rest []string) {
	vars := map[string]string{}
	count := 0
	for _, kv := range extra {
		name, val, ok := strings.Cut(kv, "=")
		if !ok {
			panic("gittest: environment entry without \"=\": " + kv)
		}
		switch {
		case name == "GIT_CONFIG_COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 || strconv.Itoa(n) != val {
				panic("gittest: GIT_CONFIG_COUNT must be a plain non-negative number, got " + strconv.Quote(val))
			}
			count = n
		case name == "GIT_CONFIG_GLOBAL":
			rel, err := filepath.Rel(home, val)
			if err != nil || !filepath.IsAbs(val) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				panic("gittest: GIT_CONFIG_GLOBAL must be a file inside home, got " + val)
			}
			rest = append(rest, kv)
		case strings.HasPrefix(name, "GIT_CONFIG_KEY_"), strings.HasPrefix(name, "GIT_CONFIG_VALUE_"):
			vars[name] = val
		case reserved[name]:
			panic("gittest: " + name + " is set by Env and cannot be changed by a caller")
		default:
			rest = append(rest, kv)
		}
	}
	for i := 0; i < count; i++ {
		n := strconv.Itoa(i)
		key, hasKey := vars["GIT_CONFIG_KEY_"+n]
		val, hasVal := vars["GIT_CONFIG_VALUE_"+n]
		if !hasKey || !hasVal || key == "" {
			panic("gittest: GIT_CONFIG_COUNT counts entry " + n + " but its KEY or VALUE is missing")
		}
		// an allowlist, not a denylist: see Env
		switch strings.ToLower(key) {
		case "user.name", "user.email":
		case "credential.helper":
			if val != "" {
				panic("gittest: a caller may only pass credential.helper with an empty value, got " + strconv.Quote(val))
			}
		default:
			panic("gittest: a caller may not pass the config key " + key + " (only credential.helper=\"\", user.name, user.email)")
		}
		pairs = append(pairs, key, val)
	}
	return pairs, rest
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
