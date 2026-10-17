// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Errors for the directory itself; they decide the exit code, not a finding.
var (
	ErrNotFound     = errors.New("the directory does not exist")
	ErrNotDirectory = errors.New("the path is not a directory")
)

type accessKind int

const (
	accMissing accessKind = iota + 1
	accUnsafe
	accTooLarge
)

// accessError says why a path could not be used. It never carries a link
// target or file content.
type accessError struct {
	kind accessKind
	why  string
	fix  string // remediation, when the default does not fit
}

func (e *accessError) Error() string { return e.why }

func unsafeErr(format string, args ...any) *accessError {
	return &accessError{kind: accUnsafe, why: fmt.Sprintf(format, args...)}
}

// dir is a validated directory held open for no-follow access. Every path below
// it is walked one component at a time with lstat and an identity check, never
// through a symbolic link, and special files are never opened (FR-055). The
// directory itself and everything below it is held to no-follow; ancestors of
// the given path are followed (macOS /tmp is a link).
type dir struct {
	root *os.Root
	abs  string
}

// openDir opens path as the root of the walk.
func openDir(path string) (*dir, error) {
	if hasDotDot(path) && dotDotThroughLink(path) {
		return nil, &accessError{accUnsafe, "is a symbolic link", "pass the real directory, not a link"}
	}
	path = walkPath(path)
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return nil, ErrNotFound
	case err != nil:
		return nil, &accessError{accUnsafe, "cannot be inspected", "check that the directory and its parents can be read"}
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, &accessError{accUnsafe, "is a symbolic link", "pass the real directory, not a link"}
	case !info.IsDir():
		return nil, ErrNotDirectory
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, &accessError{accUnsafe, "cannot be opened", "check that the directory can be read"}
	}
	if now, err := root.Stat("."); err != nil || !os.SameFile(info, now) {
		root.Close()
		return nil, &accessError{accUnsafe, "changed while it was opened", "run validate again"}
	}
	abs, err := absPath(path)
	if err != nil {
		root.Close()
		return nil, &accessError{accUnsafe, "path cannot be resolved", "pass an absolute or relative path that exists"}
	}
	return &dir{root: root, abs: abs}, nil
}

// dotDotThroughLink walks a path with .. components the way the kernel does and
// reports a .. that undoes a component other than a link after the walk went
// through a link: rlink/projects/.. is the target of rlink, which is refused like
// rlink itself, and so is any longer chain that gets back to a link target.
// plink/.. alone (the parent of the link's target) is the physical reading the
// shell gives and stays valid. Every prefix is resolved physically, so earlier
// .. components cannot hide a link from the check. A link among the ancestors of
// an absolute path (macOS /tmp) counts as a link too: a .. after it is refused.
// A component that does not exist ends the check; the later lstat reports it.
func dotDotThroughLink(path string) bool {
	cur := "."
	if strings.HasPrefix(path, "/") {
		cur = "/"
	}
	sawLink, lastLink := false, false
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "", ".":
		case "..":
			if sawLink && !lastLink {
				return true
			}
			// cur is already physical, so the kernel's reading of cur/.. is its parent.
			parent, err := filepath.EvalSymlinks(cur + "/..")
			if err != nil {
				return false
			}
			cur, lastLink = parent, false
		default:
			next := cur + "/" + part
			info, err := os.Lstat(next)
			if err != nil {
				return false
			}
			lastLink = info.Mode()&fs.ModeSymlink != 0
			sawLink = sawLink || lastLink
			if lastLink {
				resolved, rerr := filepath.EvalSymlinks(next)
				if rerr != nil {
					return false
				}
				next = resolved
			}
			cur = next
		}
	}
	return false
}

// hasDotDot reports a .. component, which the kernel resolves against the
// physical parent of a link target and a lexical Clean would not.
func hasDotDot(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// walkPath is the spelling the walk starts from. A trailing slash or a
// trailing /. would make lstat follow a link in the last component, so they are
// removed. Clean does that, but it also folds link/.. into the directory that
// holds the link, whereas the kernel (and the shell) read it as the parent of
// the link's target; a path with a .. component is therefore left to the kernel.
func walkPath(path string) string {
	if !hasDotDot(path) {
		return filepath.Clean(path)
	}
	for len(path) > 1 && (strings.HasSuffix(path, "/") || strings.HasSuffix(path, "/.")) {
		path = strings.TrimSuffix(strings.TrimSuffix(path, "."), "/")
	}
	return path
}

// absPath is the absolute physical spelling used for Git's directories.
func absPath(path string) (string, error) {
	if hasDotDot(path) {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		path = resolved
	}
	return filepath.Abs(path)
}

// stillAtAbs reports whether the absolute path Git is given still names the
// directory held open: not a link, and the same file as the root. It guards the
// window between resolving the path and handing it to Git, and the Git run.
func (d *dir) stillAtAbs() bool {
	info, err := os.Lstat(d.abs)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		return false
	}
	now, err := d.root.Stat(".")
	return err == nil && os.SameFile(info, now)
}

func (d *dir) Close() { d.root.Close() }

// walk descends through the directory components of rel and returns the
// directory holding its last component, plus a function that closes what it
// opened. A symbolic link, a non-directory or a component that changed identity
// between lstat and open is unsafe.
func (d *dir) walk(rel string) (*os.Root, string, func(), error) {
	parts := strings.Split(rel, "/")
	cur := d.root
	var opened []*os.Root
	closeAll := func() {
		for _, r := range opened {
			r.Close()
		}
	}
	for _, name := range parts[:len(parts)-1] {
		info, err := cur.Lstat(name)
		if err != nil {
			closeAll()
			return nil, "", nil, classify(err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			closeAll()
			return nil, "", nil, unsafeErr("a path component is a symbolic link")
		}
		if !info.IsDir() {
			closeAll()
			return nil, "", nil, unsafeErr("a path component is not a directory")
		}
		next, err := cur.OpenRoot(name)
		if err != nil {
			closeAll()
			return nil, "", nil, unsafeErr("a path component cannot be opened")
		}
		opened = append(opened, next)
		if now, err := next.Stat("."); err != nil || !os.SameFile(info, now) {
			closeAll()
			return nil, "", nil, unsafeErr("a path component changed while it was opened")
		}
		cur = next
	}
	return cur, parts[len(parts)-1], closeAll, nil
}

func classify(err error) *accessError {
	if errors.Is(err, fs.ErrNotExist) {
		return &accessError{kind: accMissing, why: "does not exist"}
	}
	return unsafeErr("cannot be inspected")
}

// lstat inspects rel without following any link in it.
func (d *dir) lstat(rel string) (fs.FileInfo, *accessError) {
	cur, leaf, done, err := d.walk(rel)
	if err != nil {
		return nil, err.(*accessError)
	}
	defer done()
	info, lerr := cur.Lstat(leaf)
	if lerr != nil {
		return nil, classify(lerr)
	}
	return info, nil
}

// readDir lists the directory rel without following links: rel itself and
// every component above it must be real directories. The entries are lstat
// based, so a link is reported as a link and never followed.
func (d *dir) readDir(rel string) ([]fs.DirEntry, *accessError) {
	cur, leaf, done, err := d.walk(rel)
	if err != nil {
		return nil, err.(*accessError)
	}
	defer done()
	info, lerr := cur.Lstat(leaf)
	if lerr != nil {
		return nil, classify(lerr)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, unsafeErr("is a symbolic link")
	}
	if !info.IsDir() {
		return nil, unsafeErr("is %s, not a directory", kindName(info.Mode()))
	}
	sub, oerr := cur.OpenRoot(leaf)
	if oerr != nil {
		return nil, unsafeErr("cannot be opened")
	}
	defer sub.Close()
	if now, serr := sub.Stat("."); serr != nil || !os.SameFile(info, now) {
		return nil, unsafeErr("changed while it was opened")
	}
	f, oerr := sub.Open(".")
	if oerr != nil {
		return nil, unsafeErr("cannot be opened")
	}
	defer f.Close()
	entries, rerr := f.ReadDir(-1)
	if rerr != nil {
		return nil, unsafeErr("cannot be read")
	}
	return entries, nil
}

// kindName names a file type for messages without revealing anything else.
func kindName(m fs.FileMode) string {
	switch {
	case m&fs.ModeSymlink != 0:
		return "a symbolic link"
	case m.IsDir():
		return "a directory"
	case m&fs.ModeNamedPipe != 0:
		return "a FIFO"
	case m&fs.ModeSocket != 0:
		return "a socket"
	case m&fs.ModeDevice != 0:
		return "a device"
	}
	return "not a regular file"
}

// readFile reads the regular file rel, at most limit bytes: a larger file is
// rejected by its size without being read. The file is opened with
// O_NOFOLLOW|O_NONBLOCK only after lstat showed a regular file, and the opened
// descriptor must be that same file.
func (d *dir) readFile(rel string, limit int64) ([]byte, *accessError) {
	cur, leaf, done, err := d.walk(rel)
	if err != nil {
		return nil, err.(*accessError)
	}
	defer done()
	info, lerr := cur.Lstat(leaf)
	if lerr != nil {
		return nil, classify(lerr)
	}
	if !info.Mode().IsRegular() {
		return nil, unsafeErr("is %s, not a regular file", kindName(info.Mode()))
	}
	if info.Size() > limit {
		return nil, &accessError{kind: accTooLarge, why: "is larger than its limit"}
	}
	f, oerr := cur.OpenFile(leaf, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if oerr != nil {
		if errors.Is(oerr, fs.ErrNotExist) {
			return nil, &accessError{kind: accMissing, why: "does not exist"}
		}
		return nil, unsafeErr("cannot be opened")
	}
	defer f.Close()
	fi, serr := f.Stat()
	if serr != nil || !fi.Mode().IsRegular() || !os.SameFile(info, fi) {
		return nil, unsafeErr("changed while it was opened")
	}
	data, rerr := io.ReadAll(io.LimitReader(f, limit+1))
	if rerr != nil {
		return nil, unsafeErr("cannot be read")
	}
	if int64(len(data)) > limit {
		return nil, &accessError{kind: accTooLarge, why: "is larger than its limit"}
	}
	return data, nil
}
