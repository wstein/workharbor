//go:build unix

package answers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/version"
)

// statInfo is what the owner check needs from fstat; tests inject their own.
type statInfo struct {
	Mode fs.FileMode
	UID  int
}

var fstat = func(f *os.File) (statInfo, error) {
	fi, err := f.Stat()
	if err != nil {
		return statInfo{}, err
	}
	return infoOf(fi), nil
}

func infoOf(fi fs.FileInfo) statInfo {
	si := statInfo{Mode: fi.Mode(), UID: -1}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		si.UID = int(st.Uid)
	}
	return si
}

// identity is the build identity answers are bound to; tests replace it.
var identity = func() (string, error) { return identityOf(version.Get()) }

// identityOf is the binding identity of a build. A dirty build, and a build
// that does not know its commit, have none: the same text could stand for
// different code.
func identityOf(info version.Info) (string, error) {
	if info.Dirty || info.Commit == "" || info.Commit == "unknown" || strings.HasSuffix(info.Version, "gunknown") {
		return "", ErrNoBuildIdentity
	}
	return info.Version + "@" + info.Commit, nil
}

// Identity returns the identity of this build, "<version>@<commit>", or
// ErrNoBuildIdentity for a dirty build.
func Identity() (string, error) { return identity() }

// Load reads and decodes the answer file at path; see LoadRaw.
func Load(path string, uid int) (f File, warnings []string, err error) {
	f, _, warnings, err = LoadRaw(path, uid)
	return f, warnings, err
}

// LoadRaw reads and decodes the answer file at path and also returns the bytes
// it decoded, so a digest of the file is the digest of what was used. The file is opened without
// following a symbolic link and judged by fstat of the open descriptor: it must
// be a regular file owned by uid, not writable by group or others, and its path
// must not lie in a git working tree. A file readable by group or others loads
// with a warning. The caller compares File.Whr with Identity.
func LoadRaw(path string, uid int) (f File, data []byte, warnings []string, err error) {
	if err := checkNotInGitTree(path); err != nil {
		return File{}, nil, nil, err
	}
	fh, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // the path is the one the human named; it is judged by fstat
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return File{}, nil, nil, fmt.Errorf("%w: %s is a symbolic link", ErrUnsafeFile, path)
		}
		return File{}, nil, nil, err
	}
	defer func() { _ = fh.Close() }()
	si, err := fstat(fh)
	if err != nil {
		return File{}, nil, nil, err
	}
	switch {
	case !si.Mode.IsRegular():
		return File{}, nil, nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafeFile, path)
	case si.UID != uid:
		return File{}, nil, nil, fmt.Errorf("%w: %s belongs to another user", ErrUnsafeFile, path)
	case si.Mode.Perm()&0o022 != 0:
		return File{}, nil, nil, fmt.Errorf("%w: %s has mode %04o; chmod 600", ErrWritableByOthers, path, si.Mode.Perm())
	}
	if si.Mode.Perm()&0o044 != 0 {
		warnings = append(warnings, fmt.Sprintf("%s is readable by others (mode %04o); chmod 600", path, si.Mode.Perm()))
	}
	data, err = io.ReadAll(io.LimitReader(fh, MaxBytes+1))
	if err != nil {
		return File{}, nil, nil, err
	}
	f, err = Decode(data)
	if err != nil {
		return File{}, nil, nil, err
	}
	return f, data, warnings, nil
}

// Save writes f to path atomically. Every entry is first checked against
// checks: it must name an eligible step with its current digest (ErrIneligible).
// The bytes go to a 0600 temporary file in the same directory (created
// exclusively, mode forced regardless of umask), are synced and renamed over the
// target. The build identity, schema and version are set here; a dirty build
// refuses. See SaveAs for a given identity. The
// parent must exist, except ~/.config/whr, which is created 0700. An existing
// target must be a regular file of this user, not a link. A path inside a git
// working tree is refused.
func Save(path string, f File, checks []doctor.Check) error {
	id, err := identity()
	if err != nil {
		return err
	}
	return SaveAs(id, path, f, checks)
}

// SaveAs is Save for the build identity id, which the caller got from Identity
// (or a test passes in): the file binds its answers to that build.
func SaveAs(id, path string, f File, checks []doctor.Check) error {
	f.Schema, f.V, f.Whr, f.Phase = Schema, Version, id, PhaseUser
	data, err := Encode(f)
	if err != nil {
		return err
	}
	if err := checkEntries(f, checks); err != nil {
		return err
	}
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := checkNotInGitTree(path); err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if filepath.Base(dir) != "whr" || filepath.Base(filepath.Dir(dir)) != ".config" {
			return fmt.Errorf("%w: %s", ErrNoDirectory, dir)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if fi, err := os.Lstat(path); err == nil {
		if !fi.Mode().IsRegular() || infoOf(fi).UID != os.Getuid() {
			return fmt.Errorf("%w: %s", ErrUnsafeFile, path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+filepath.Base(path)+"."+hex.EncodeToString(rnd[:])+".tmp")
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // a fresh name inside the chosen directory, created exclusively
	if err != nil {
		return err
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if err := fh.Chmod(0o600); err != nil {
		_ = fh.Close()
		cleanup()
		return err
	}
	if _, err := fh.Write(data); err != nil {
		_ = fh.Close()
		cleanup()
		return err
	}
	if err := fh.Sync(); err != nil {
		_ = fh.Close()
		cleanup()
		return err
	}
	if err := fh.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return err
	}
	if d, err := os.Open(dir); err == nil { //nolint:gosec // the directory of the file being saved
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// checkNotInGitTree refuses a path whose directory, after symbolic links, has a
// .git entry in it or above it: an answer file must not be committed by chance.
func checkNotInGitTree(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	// Resolve the nearest existing ancestor, so a link into a repository counts.
	probe := dir
	for {
		if r, err := filepath.EvalSymlinks(probe); err == nil {
			dir = filepath.Join(r, filepath.Clean(dir[len(probe):]))
			break
		}
		if probe == filepath.Dir(probe) {
			break
		}
		probe = filepath.Dir(probe)
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return fmt.Errorf("%w: %s is inside %s", ErrInGitTree, path, d)
		}
		if d == filepath.Dir(d) {
			return nil
		}
	}
}
