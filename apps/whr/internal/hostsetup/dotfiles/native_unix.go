// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package dotfiles

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Open wires the engine to an explicitly supplied home directory. It never reads
// HOME or creates a run lock/protocol. The caller must close the returned handle.
// The root itself must be a real directory; its trusted absolute ancestors may
// contain platform links such as macOS /tmp. Paths beneath it never follow links.
func Open(home string, clock Clock) (*Engine, func() error, error) {
	if clock == nil {
		return nil, nil, fs.ErrInvalid
	}
	f, err := openNative(home)
	if err != nil {
		return nil, nil, err
	}
	return New(f, clock), f.Close, nil
}

type nativeFS struct {
	mu   sync.Mutex
	root int
	// Test-only synchronization seams run before the corresponding final check.
	// Production leaves them nil. They do not widen the public FS API.
	beforeParentOpen, beforeLeafOpen func(string)
	beforeKernelRename               func(string, string)
	beforeRename                     func(string, string)
	beforeChmod                      func(string)
}

func openNative(home string) (*nativeFS, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return nil, fs.ErrInvalid
	}
	var before unix.Stat_t
	if err := unix.Lstat(home, &before); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, fs.ErrPermission
	}
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	now, err := descriptor(fd)
	if err != nil || now.ID != nativeInfo(&before).ID {
		unix.Close(fd)
		return nil, fs.ErrPermission
	}
	return &nativeFS{root: fd}, nil
}
func (f *nativeFS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.root < 0 {
		return nil
	}
	fd := f.root
	f.root = -1
	return unix.Close(fd)
}
func nativeInfo(s *unix.Stat_t) Info {
	mode := fs.FileMode(s.Mode & 0777)
	switch s.Mode & unix.S_IFMT {
	case unix.S_IFREG:
	case unix.S_IFDIR:
		mode |= fs.ModeDir
	case unix.S_IFLNK:
		mode |= fs.ModeSymlink
	case unix.S_IFIFO:
		mode |= fs.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= fs.ModeSocket
	case unix.S_IFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFBLK:
		mode |= fs.ModeDevice
	default:
		mode |= fs.ModeIrregular
	}
	if s.Mode&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if s.Mode&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if s.Mode&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return Info{ID: fmt.Sprintf("%d:%d", s.Dev, s.Ino), Mode: mode, Size: s.Size}
}
func descriptor(fd int) (Info, error) {
	var s unix.Stat_t
	err := unix.Fstat(fd, &s)
	if err != nil {
		return Info{}, err
	}
	return nativeInfo(&s), nil
}
func inspectAt(fd int, name string) (Info, error) {
	var s unix.Stat_t
	err := unix.Fstatat(fd, name, &s, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		return Info{}, err
	}
	return nativeInfo(&s), nil
}
func openAt(fd int, name string, flags int, mode uint32) (int, error) {
	for {
		n, err := unix.Openat(fd, name, flags, mode)
		if err != unix.EINTR {
			return n, err
		}
	}
}
func relative(p string) bool { return fs.ValidPath(p) && !strings.ContainsRune(p, 0) }

// walk holds every opened parent, checking lstat/open identity at each descent.
// A held parent prevents path substitution from redirecting a later leaf open.
func (f *nativeFS) walk(p string) (int, string, func(), error) {
	if f.root < 0 {
		return -1, "", func() {}, fs.ErrClosed
	}
	if !relative(p) {
		return -1, "", func() {}, fs.ErrInvalid
	}
	cur := f.root
	var opened []int
	done := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = unix.Close(opened[i])
		}
	}
	parts := strings.Split(p, "/")
	prefix := ""
	for _, name := range parts[:len(parts)-1] {
		if prefix != "" {
			prefix += "/"
		}
		prefix += name
		before, err := inspectAt(cur, name)
		if err != nil {
			done()
			return -1, "", func() {}, err
		}
		if !before.Mode.IsDir() {
			done()
			return -1, "", func() {}, fs.ErrPermission
		}
		if f.beforeParentOpen != nil {
			f.beforeParentOpen(prefix)
		}
		next, err := openAt(cur, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			done()
			return -1, "", func() {}, err
		}
		opened = append(opened, next)
		now, err := descriptor(next)
		if err != nil || before.ID != now.ID || !now.Mode.IsDir() {
			done()
			return -1, "", func() {}, fs.ErrPermission
		}
		cur = next
	}
	return cur, parts[len(parts)-1], done, nil
}
func (f *nativeFS) Stat(p string) (Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fd, leaf, done, err := f.walk(p)
	if err != nil {
		return Info{}, err
	}
	defer done()
	return inspectAt(fd, leaf)
}
func (f *nativeFS) Read(p, id string, limit int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit < 0 || limit > maxManifestBytes {
		return nil, fs.ErrInvalid
	}
	parent, leaf, done, err := f.walk(p)
	if err != nil {
		return nil, err
	}
	defer done()
	before, err := inspectAt(parent, leaf)
	if err != nil || !before.Mode.IsRegular() || before.ID != id || before.Size > limit {
		return nil, fs.ErrPermission
	}
	if f.beforeLeafOpen != nil {
		f.beforeLeafOpen(p)
	}
	fd, err := openAt(parent, leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			_ = unix.Close(fd)
		}
	}()
	now, err := descriptor(fd)
	if err != nil || !now.Mode.IsRegular() || !same(before, now) {
		return nil, fs.ErrPermission
	}
	// Like the existing006 reader, avoid pollable O_NONBLOCK descriptors on BSD.
	if err = unix.SetNonblock(fd, false); err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), leaf)
	if file == nil {
		return nil, fs.ErrInvalid
	}
	owned = false
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := descriptor(fd)
	if err != nil || !same(now, after) || int64(len(b)) > limit || int64(len(b)) != after.Size {
		return nil, fs.ErrPermission
	}
	return b, nil
}
func (f *nativeFS) Mkdir(p string, mode fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p == "." || mode&^fs.ModePerm != 0 {
		return fs.ErrInvalid
	}
	parent, leaf, done, err := f.walk(p)
	if err != nil {
		return err
	}
	defer done()
	if err = unix.Mkdirat(parent, leaf, uint32(mode)); err != nil {
		return err
	}
	before, err := inspectAt(parent, leaf)
	if err != nil || !before.Mode.IsDir() {
		return fs.ErrPermission
	}
	// No-follow chmod is required; unsupported kernels refuse instead of using a
	// path-based chmod that could follow a swapped link. This also removes umask.
	if f.beforeChmod != nil {
		f.beforeChmod(p)
	}
	if err = unix.Fchmodat(parent, leaf, uint32(mode), unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	fd, err := openAt(parent, leaf, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	now, err := descriptor(fd)
	if err != nil || now.ID != before.ID || now.Mode.Perm() != mode {
		return fs.ErrPermission
	}
	return unix.Fsync(parent)
}
func (f *nativeFS) Create(p string, b []byte, mode fs.FileMode) (Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p == "." || mode&^fs.ModePerm != 0 {
		return Info{}, fs.ErrInvalid
	}
	parent, leaf, done, err := f.walk(p)
	if err != nil {
		return Info{}, err
	}
	defer done()
	fd, err := openAt(parent, leaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return Info{}, err
	}
	// Identity comes from the exclusively created descriptor, never a path stat.
	made, err := descriptor(fd)
	if err != nil {
		unix.Close(fd)
		return Info{}, err
	}
	file := os.NewFile(uintptr(fd), leaf)
	if file == nil {
		unix.Close(fd)
		return made, fs.ErrInvalid
	}
	if err = unix.Fchmod(fd, uint32(mode)); err == nil {
		var n int
		n, err = file.Write(b)
		if err == nil && n != len(b) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = file.Sync()
	}
	now, statErr := descriptor(fd)
	if statErr == nil {
		made = now
	} else if err == nil {
		err = statErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = unix.Fsync(parent)
	}
	return made, err
}

// Rename checks identities immediately before the syscall. There is no portable
// compare-inode-and-replace syscall for an existing target: this follows spec D8's
// mistake/surprise model, not a claim against adversarial post-check substitution.
// Absent destinations use a kernel no-replace operation, never a check/rename gap.
func (f *nativeFS) Rename(from, to, fromID, toID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if from == "." || to == "." || fromID == "" {
		return fs.ErrInvalid
	}
	a, source, closeA, err := f.walk(from)
	if err != nil {
		return err
	}
	defer closeA()
	b, target, closeB, err := f.walk(to)
	if err != nil {
		return err
	}
	defer closeB()
	if f.beforeRename != nil {
		f.beforeRename(from, to)
	}
	si, err := inspectAt(a, source)
	if err != nil || !si.Mode.IsRegular() || si.ID != fromID {
		return fs.ErrPermission
	}
	ti, targetErr := inspectAt(b, target)
	if toID == "" {
		if !errors.Is(targetErr, fs.ErrNotExist) {
			return fs.ErrExist
		}
	} else if targetErr != nil || !ti.Mode.IsRegular() || ti.ID != toID {
		return fs.ErrPermission
	}
	if f.beforeKernelRename != nil {
		f.beforeKernelRename(from, to)
	}
	if toID == "" {
		err = renameAbsent(a, source, b, target)
	} else {
		err = unix.Renameat(a, source, b, target)
	}
	if err != nil {
		return err
	}
	if err = unix.Fsync(b); err != nil {
		return err
	}
	return unix.Fsync(a)
}
func (f *nativeFS) Remove(p, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p == "." || id == "" {
		return fs.ErrInvalid
	}
	parent, leaf, done, err := f.walk(p)
	if err != nil {
		return err
	}
	defer done()
	info, err := inspectAt(parent, leaf)
	if err != nil {
		return err
	}
	if !info.Mode.IsRegular() || info.ID != id {
		return fs.ErrPermission
	}
	if err = unix.Unlinkat(parent, leaf, 0); err != nil {
		return err
	}
	return unix.Fsync(parent)
}
