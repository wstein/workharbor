package hostgit

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxAlternates bounds how much of an alternates file is read.
const maxAlternates = 64 << 10

// verifyCheckout checks an agent's checkout before git touches it and returns
// the resolved checkout and its verified .git directory. It refuses:
//
//   - a checkout that does not resolve to a place under the workspace root;
//   - a .git that is not a real directory: a "gitdir:" file or a symlink sends
//     git to another repository on the host;
//   - objects or refs that are symlinks, and a commondir file;
//   - objects/info/alternates that is not absent or a plain file naming only
//     the listed caches, and any http-alternates.
//
// The agent's own files are what is checked, so the run must have stopped: a
// running agent could change them between this check and the fetch.
func (g *Git) verifyCheckout(path string) (checkout, gitDir string, err error) {
	if g.root == "" {
		return "", "", ErrNoRoot
	}
	checkout, err = resolveDir(path)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(g.root, checkout)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: %q resolves to %q", ErrOutsideRoot, path, checkout)
	}

	gitDir = filepath.Join(checkout, ".git")
	for _, sub := range []string{"", "objects", "refs"} {
		p := filepath.Join(gitDir, sub)
		info, err := os.Lstat(p)
		if err != nil {
			return "", "", fmt.Errorf("%w: %s: %v", ErrCheckout, p, err) //nolint:errorlint // the cause is not needed by callers
		}
		if !info.IsDir() { // Lstat: a symlink is not a directory here
			return "", "", fmt.Errorf("%w: %s is a %s, not a real directory", ErrCheckout, p, kind(info.Mode()))
		}
	}
	if _, err := os.Lstat(filepath.Join(gitDir, "commondir")); err == nil {
		return "", "", fmt.Errorf("%w: %s/commondir links this checkout to another repository", ErrCheckout, gitDir)
	}
	if err := g.checkAlternates(gitDir); err != nil {
		return "", "", err
	}
	return checkout, gitDir, nil
}

func kind(m os.FileMode) string {
	switch {
	case m&os.ModeSymlink != 0:
		return "symlink"
	case m.IsRegular():
		return "file"
	default:
		return "special file"
	}
}

// checkAlternates allows an absent alternates file, or one that names only the
// listed caches.
func (g *Git) checkAlternates(gitDir string) error {
	info := filepath.Join(gitDir, "objects", "info")
	if _, err := os.Lstat(filepath.Join(info, "http-alternates")); err == nil {
		return fmt.Errorf("%w: http-alternates exists", ErrAlternates)
	}
	file := filepath.Join(info, "alternates")
	fi, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAlternates, err) //nolint:errorlint // the cause is not needed by callers
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: alternates is a %s, not a plain file", ErrAlternates, kind(fi.Mode()))
	}
	if fi.Size() > maxAlternates {
		return fmt.Errorf("%w: alternates is too large", ErrAlternates)
	}
	data, err := os.ReadFile(file) //nolint:gosec // the path was built from a verified checkout and checked with Lstat
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAlternates, err) //nolint:errorlint // the cause is not needed by callers
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, `"`) {
			return fmt.Errorf("%w: quoted alternates entries are not accepted", ErrAlternates)
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(filepath.Join(gitDir, "objects"), line) // relative to the objects directory
		}
		resolved, err := filepath.EvalSymlinks(line)
		if err != nil || !g.cache(resolved) {
			return fmt.Errorf("%w: %q", ErrAlternates, line)
		}
	}
	return nil
}

// cache reports whether a resolved path is one of the listed read-only caches.
func (g *Git) cache(resolved string) bool {
	for _, c := range g.alternates {
		if resolved == c {
			return true
		}
	}
	return false
}
