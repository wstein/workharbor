// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitArgs is the only Git invocation of the local checks.
func GitArgs() []string { return []string{"--version"} }

// GitEnv is the freshly built environment of that invocation: no inherited
// GIT_* variables, no system or global configuration, no prompt.
func GitEnv() []string {
	return []string{
		"LC_ALL=C", "LANG=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "HOME=" + os.DevNull,
	}
}

const gitOutputMax = 4096

func findGit() (string, error) { return exec.LookPath("git") }

// runGitVersion runs `git --version` with empty stdin, a bounded output and a
// neutral working directory; ctx bounds the runtime.
func runGitVersion(ctx context.Context, gitPath string) (string, error) {
	cmd := exec.CommandContext(ctx, gitPath, GitArgs()...)
	cmd.Env = GitEnv()
	cmd.Dir = string(os.PathSeparator)
	cmd.WaitDelay = 500 * time.Millisecond
	var out limited
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(out.b.String(), "\n")
	return strings.TrimSpace(line), nil
}

// limited keeps the first gitOutputMax bytes and discards the rest.
type limited struct{ b bytes.Buffer }

func (l *limited) Write(p []byte) (int, error) {
	if room := gitOutputMax - l.b.Len(); room > 0 {
		l.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

var gitVersionRE = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:\.(\d+))?`)

// parseGit returns the numeric major, minor and patch of a `git --version` line.
func parseGit(line string) ([3]int, bool) {
	m := gitVersionRE.FindStringSubmatch(line)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func atLeast(v, min [3]int) bool {
	for i := range v {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

var minGit = func() [3]int { v, _ := parseGit("git version " + MinGitVersion); return v }()

// git reports git.version.
func (r *run) git() Check {
	const id = "git.version"
	path, err := r.p.FindGit()
	if err != nil {
		if errors.Is(err, errNoPort) {
			return unknown(id, reasonNotMeasurable, "Git could not be looked up.", "Rerun whr doctor from a normal shell.", nil)
		}
		return failed(id, "Git was not found on PATH.", "Install Git "+MinGitVersion+" or newer with your package manager and rerun whr doctor.",
			map[string]any{"minimum": MinGitVersion})
	}
	if r.p.OS == "darwin" && r.macShim(path) {
		return failed(id, "The Git on PATH is the macOS shim that would open the Command Line Tools installer; it was not run.",
			"Install Git "+MinGitVersion+" or newer (for example with Homebrew) or finish the Command Line Tools installation yourself, then rerun whr doctor; whr never starts the installer.",
			map[string]any{"path": path, "minimum": MinGitVersion, "shim": true})
	}
	if r.b.remaining() == 0 {
		return exhausted(id)
	}
	cctx, cancel := context.WithTimeout(r.ctx, r.b.limit(2*time.Second))
	defer cancel()
	line, err := r.p.GitVersion(cctx, path)
	if err != nil {
		return unknown(id, reasonNotMeasurable, "git --version did not complete.", "Check that the git on PATH runs, then rerun whr doctor.", map[string]any{"path": path})
	}
	v, ok := parseGit(line)
	if !ok {
		return unknown(id, reasonNotMeasurable, "git --version printed an unrecognized line.", "Check that the git on PATH is a regular Git installation.", map[string]any{"path": path})
	}
	ev := map[string]any{"path": path, "version": line, "minimum": MinGitVersion}
	if !atLeast(v, minGit) {
		return failed(id, "Git is older than "+MinGitVersion+".", "Upgrade Git to "+MinGitVersion+" or newer and rerun whr doctor.", ev)
	}
	return passed(id, "Git meets the minimum version.", ev)
}

// macShim reports whether git is the /usr/bin/git stub without any developer
// directory behind it (the xcode-select -p test, by path only).
func (r *run) macShim(path string) bool {
	if path != "/usr/bin/git" {
		return false
	}
	cands := []string{"/Library/Developer/CommandLineTools/usr/bin/git", "/Applications/Xcode.app/Contents/Developer/usr/bin/git"}
	if d := r.p.Getenv("DEVELOPER_DIR"); d != "" {
		cands = append(cands, strings.TrimRight(d, "/")+"/usr/bin/git")
	}
	for _, c := range cands {
		if _, err := r.p.Lstat(c); err == nil {
			return false
		}
	}
	return true
}
