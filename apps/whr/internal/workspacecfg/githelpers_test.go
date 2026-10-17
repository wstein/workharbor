// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

// hermeticEnv is the environment of every fixture Git command: no user or
// system configuration, fixed identity and dates, no prompts.
func hermeticEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z", "LC_ALL=C",
	}
}

// runGit runs a fixture Git command and returns its standard output.
func runGit(t *testing.T, dir string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = hermeticEnv(t.TempDir())
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return out.String()
}

const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

const gitmodulesApp = "[submodule \"app\"]\n\tpath = projects/app\n\turl = https://f.example/o/app.git\n"

// repoDir turns a valid configuration directory into a Git repository with an
// empty template (no sample hooks). With commit it commits everything.
func repoDir(t *testing.T, commit bool) string {
	t.Helper()
	dir := validDir(t)
	runGit(t, dir, "", "init", "-q", "-b", "main", "--template=")
	if commit {
		runGit(t, dir, "", "add", "-A")
		runGit(t, dir, "", "commit", "-q", "-m", "init")
	}
	return dir
}

// addGitlink stages a gitlink without any clone.
func addGitlink(t *testing.T, dir, path, objectID string) {
	t.Helper()
	runGit(t, dir, "", "update-index", "--add", "--cacheinfo", "160000,"+objectID+","+path)
}

// pinnedRepo is a committed repository with one pinned submodule.
func pinnedRepo(t *testing.T) string {
	t.Helper()
	dir := repoDir(t, false)
	write(t, dir, ".gitmodules", gitmodulesApp)
	runGit(t, dir, "", "add", "-A")
	addGitlink(t, dir, "projects/app", sha)
	runGit(t, dir, "", "commit", "-q", "-m", "pin")
	return dir
}

// fakeGit records every call and answers through handler.
type fakeGit struct {
	mu      sync.Mutex
	calls   []workspacecfg.GitCmd
	handler func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error)
}

func (f *fakeGit) Run(_ context.Context, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	return f.handler(subcommand(c.Args), c)
}

func (f *fakeGit) subs() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, subcommand(c.Args))
	}
	return out
}

// subcommand finds the Git subcommand in an argument vector with global options.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-c":
			i++
		case args[i] == "--version":
			return "--version"
		case strings.HasPrefix(args[i], "-"):
		default:
			return args[i]
		}
	}
	return ""
}

func ok(stdout string) (workspacecfg.GitOut, error) {
	return workspacecfg.GitOut{Stdout: []byte(stdout)}, nil
}

// minimalGitDir adds the files the lstat pre-checks require to a configuration
// directory, without a real repository.
func minimalGitDir(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	write(t, dir, ".git/config", "")
	write(t, dir, ".git/objects/info/.keep", "")
}

func fixedGit(path string) func() (string, error) { return func() (string, error) { return path, nil } }

// healthyHandler answers the seven vectors of an empty repository with a HEAD.
func healthyHandler(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	switch sub {
	case "--version":
		return ok("git version 2.43.0\n")
	case "rev-parse":
		return ok(sha + "\n")
	case "ls-files", "ls-tree":
		return ok("")
	}
	return workspacecfg.GitOut{Exit: 128}, nil
}
