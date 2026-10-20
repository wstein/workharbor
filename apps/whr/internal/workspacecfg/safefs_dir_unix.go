// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package workspacecfg

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// directoryHandle owns one descriptor; it never reopens its original pathname.
type directoryHandle struct{ file *os.File }

const directoryOpenFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

func validComponent(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

func withFileFD(file *os.File, action func(int) error) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var operationErr error
	if err := raw.Control(func(fd uintptr) { operationErr = action(int(fd)) }); err != nil {
		return err
	}
	return operationErr
}

func ownedDirectory(fd int, name string) (*directoryHandle, error) {
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return nil, os.ErrInvalid
	}
	return &directoryHandle{file: file}, nil
}

func openRootDirectory(path string) (*directoryHandle, error) {
	fd := -1
	var err error
	for {
		fd, err = unix.Open(path, directoryOpenFlags, 0)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return nil, err
	}
	return ownedDirectory(fd, path)
}

func (h *directoryHandle) Close() { _ = h.file.Close() }

func (h *directoryHandle) openDirectory(name string) (*directoryHandle, error) {
	if !validComponent(name) {
		return nil, os.ErrPermission
	}
	fd := -1
	err := withFileFD(h.file, func(parent int) error {
		var err error
		for {
			fd, err = unix.Openat(parent, name, directoryOpenFlags, 0)
			if err != unix.EINTR {
				return err
			}
		}
	})
	if err != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return nil, err
	}
	return ownedDirectory(fd, name)
}

// nativeFileInfo keeps the lstat/fstat snapshot used by identity and type checks.
// It also provides the existing FileInfo shape to the validator's callers.
type nativeFileInfo struct {
	name string
	stat unix.Stat_t
}

func (n *nativeFileInfo) Name() string { return n.name }
func (n *nativeFileInfo) Size() int64  { return n.stat.Size }
func (n *nativeFileInfo) IsDir() bool  { return n.Mode().IsDir() }
func (n *nativeFileInfo) Sys() any     { return &n.stat }
func (n *nativeFileInfo) ModTime() time.Time {
	return time.Unix(int64(n.stat.Mtim.Sec), int64(n.stat.Mtim.Nsec))
}
func (n *nativeFileInfo) Mode() fs.FileMode {
	raw := uint32(n.stat.Mode)
	mode := fs.FileMode(raw & 0777)
	switch raw & unix.S_IFMT {
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
	case unix.S_IFREG:
	default:
		mode |= fs.ModeIrregular
	}
	if raw&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if raw&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if raw&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}
func sameNativeFile(a, b *nativeFileInfo) bool {
	return uint64(a.stat.Dev) == uint64(b.stat.Dev) && uint64(a.stat.Ino) == uint64(b.stat.Ino)
}

func lstatNativePath(path string) (*nativeFileInfo, error) {
	info := &nativeFileInfo{name: filepath.Base(path)}
	for {
		err := unix.Lstat(path, &info.stat)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		return info, nil
	}
}
func statNativeFile(file *os.File) (*nativeFileInfo, error) {
	info := &nativeFileInfo{name: filepath.Base(file.Name())}
	err := withFileFD(file, func(fd int) error {
		for {
			err := unix.Fstat(fd, &info.stat)
			if err != unix.EINTR {
				return err
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}
func (h *directoryHandle) stat() (*nativeFileInfo, error) { return statNativeFile(h.file) }
func (h *directoryHandle) Lstat(name string) (*nativeFileInfo, error) {
	if !validComponent(name) {
		return nil, os.ErrPermission
	}
	info := &nativeFileInfo{name: name}
	err := withFileFD(h.file, func(fd int) error {
		for {
			err := unix.Fstatat(fd, name, &info.stat, unix.AT_SYMLINK_NOFOLLOW)
			if err != unix.EINTR {
				return err
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

type nativeDirEntry struct{ info *nativeFileInfo }

func (e nativeDirEntry) Name() string               { return e.info.Name() }
func (e nativeDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e nativeDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e nativeDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

// Snapshot metadata while the descriptor is held, as os.Root's ReadDir did.
// Never retain os.NewFile's potentially lazy pathname-based DirEntry.Info.
func (h *directoryHandle) readEntries(n int) ([]fs.DirEntry, error) {
	entries, readErr := h.file.ReadDir(n)
	out := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := h.Lstat(entry.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, nativeDirEntry{info: info})
	}
	return out, readErr
}
