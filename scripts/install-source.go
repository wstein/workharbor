//go:build ignore

// install-source is the read-only preflight for make install, not a managed
// installation or a proof of independent review (D24, D34, issue #299).
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) != 2 && len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: install-source <development prefix> [<staging DESTDIR>]")
		os.Exit(2)
	}
	destdir := ""
	if len(os.Args) == 3 {
		destdir = os.Args[2]
	}
	if err := check(os.Args[1], destdir); err != nil {
		fmt.Fprintln(os.Stderr, "refusing source install:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "development installation: user-writable supervisor from current local main; obtain independent review of this exact commit before installing; use setup --dev (never the managed dogfood or reference host)")
}

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "credential.helper=", "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/usr/bin/true", "SSH_ASKPASS=/usr/bin/true", "SSH_AUTH_SOCK=", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1"}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func within(path, root string) bool {
	ri, err := os.Stat(root)
	if err != nil {
		return false
	}
	for p := path; ; p = filepath.Dir(p) {
		if pi, err := os.Stat(p); err == nil && os.SameFile(pi, ri) {
			return true
		}
		if p == filepath.Dir(p) {
			return false
		}
	}
}

// check validates prefix, the path the installed files are meant for. With a
// non-empty destdir the files are written under destdir+prefix instead, so the
// lexical prefix rules still apply to prefix, and the filesystem rules (exists,
// owned, writable, outside Git) apply to the staged location.
func check(prefix, destdir string) error {
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		return fmt.Errorf("root must use the signed managed install-release path")
	}
	if !filepath.IsAbs(prefix) || filepath.Clean(prefix) != prefix || strings.ContainsAny(prefix, "\n\r\t") {
		return fmt.Errorf("development prefix must be an absolute clean path")
	}
	if destdir != "" {
		if !filepath.IsAbs(destdir) || filepath.Clean(destdir) != destdir || destdir == string(filepath.Separator) || strings.ContainsAny(destdir, "\n\r\t") {
			return fmt.Errorf("DESTDIR must be an absolute clean path other than / (no trailing slash)")
		}
		di, err := os.Stat(destdir)
		if err != nil || !di.IsDir() {
			return fmt.Errorf("DESTDIR must be an existing directory")
		}
	}
	for _, managed := range []string{"/opt/whr", "/opt/homebrew", "/usr/local"} {
		if prefix == managed || strings.HasPrefix(prefix, managed+string(filepath.Separator)) || within(prefix, managed) {
			return fmt.Errorf("managed prefix requires signed install-release")
		}
	}
	stage, staged := prefix, false
	if destdir != "" {
		stage = destdir + prefix
		if _, err := os.Lstat(stage); os.IsNotExist(err) {
			// mkdir -p creates the staged prefix; check the DESTDIR it lands in.
			stage, staged = destdir, true
		}
	}
	resolved, err := filepath.EvalSymlinks(stage)
	if err != nil {
		return fmt.Errorf("development prefix must already exist: %w", err)
	}
	for _, managed := range []string{"/opt/whr", "/opt/homebrew", "/usr/local"} {
		if within(resolved, managed) {
			return fmt.Errorf("managed prefix requires signed install-release")
		}
	}
	home := os.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return fmt.Errorf("cannot verify the account home")
	}
	if hi, err := os.Stat(home); err != nil || !hi.IsDir() {
		return fmt.Errorf("cannot inspect the account home")
	}
	if within(home, resolved) || resolved == string(filepath.Separator) {
		return fmt.Errorf("development prefix must not be root, HOME or an ancestor of HOME")
	}
	pi, err := os.Stat(resolved)
	if err != nil || !pi.IsDir() {
		return fmt.Errorf("development prefix must be a directory")
	}
	if err := owned(resolved, pi); err != nil {
		return err
	}
	if err := syscall.Access(resolved, 2); err != nil {
		return fmt.Errorf("development prefix is not writable: %w", err)
	}
	// Local core.worktree must not redirect checks to a different tree than
	// the physical working directory from which Go builds the source.
	root, err := git(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ri, err := os.Stat(root)
	if err != nil {
		return err
	}
	ci, err := os.Stat(cwd)
	if err != nil {
		return err
	}
	if !os.SameFile(ri, ci) {
		return fmt.Errorf("Git working tree must equal the physical source checkout")
	}
	status, err := git(".", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("dirty tree: commit or stash first")
	}
	head, err := git(".", "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return err
	}
	main, err := git(".", "rev-parse", "--verify", "refs/heads/main^{commit}")
	if err != nil || head != main {
		return fmt.Errorf("HEAD must equal the current local main commit (older, topic and ahead commits are refused)")
	}
	for _, query := range [][]string{{"rev-parse", "--show-toplevel"}, {"rev-parse", "--path-format=absolute", "--git-common-dir"}, {"rev-parse", "--absolute-git-dir"}} {
		root, err := git(".", query...)
		if err != nil {
			return err
		}
		if within(resolved, root) {
			return fmt.Errorf("development prefix must be outside the source checkout and Git metadata")
		}
	}
	// Detect other checkouts and bare Git repositories without running their
	// configuration: walk directory entries instead of executing git there.
	for p := resolved; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return fmt.Errorf("development prefix must be outside Git working trees")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect prefix Git ancestry: %w", err)
		}
		if _, err := os.Lstat(filepath.Join(p, "HEAD")); err == nil {
			if st, err := os.Stat(filepath.Join(p, "objects")); err == nil && st.IsDir() {
				return fmt.Errorf("development prefix must be outside Git metadata")
			}
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if staged {
		return nil
	}
	for _, rel := range []string{"bin", "libexec", "libexec/whr", "bin/whr", "libexec/whr/whr-shim-linux-arm64", "libexec/whr/whr-proxy-linux-arm64", "libexec/whr/VERSION"} {
		path := filepath.Join(resolved, rel)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("install destination %s is a symlink", rel)
		}
		if err := owned(path, info); err != nil {
			return err
		}
		directory := rel == "bin" || rel == "libexec" || rel == "libexec/whr"
		if directory && !info.IsDir() {
			return fmt.Errorf("install destination %s must be a directory", rel)
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !directory && (!ok || st.Nlink != 1 || !info.Mode().IsRegular()) {
			return fmt.Errorf("install destination %s must be a regular single-link file", rel)
		}
	}
	return nil
}

func owned(path string, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("development destination %s must be user-owned and closed to group and other writers", path)
	}
	return nil
}
