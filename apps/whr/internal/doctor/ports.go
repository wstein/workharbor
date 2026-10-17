// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/user"
	"runtime"
	"time"
)

// Outcome is what the validator port reports for config.valid.
type Outcome struct {
	// Errors counts error findings; Warnings counts the rest.
	Errors, Warnings int
}

// Validator validates the workspace in dir with the code of `whr validate`.
// It must honor ctx, whose deadline is the remainder of the shared budget.
type Validator func(ctx context.Context, dir string) (Outcome, error)

// Ports is everything doctor reads from the machine. A nil port is treated as
// unavailable, so the dependent check is unknown or skipped, never pass.
type Ports struct {
	OS, Arch string
	Getenv   func(string) string
	Home     func() (string, error)
	User     func() string
	Host     func() string
	Getwd    func() (string, error)
	Now      func() time.Time
	// Lstat reports whether a path exists without following a link.
	Lstat func(path string) (fs.FileInfo, error)
	// DialUnix connects to a unix socket and closes it; it sends nothing.
	DialUnix func(ctx context.Context, path string) error
	// ReadFile reads a regular file of at most max bytes.
	ReadFile func(path string, max int64) ([]byte, error)
	// CanRead reports whether a directory can be listed.
	CanRead func(dir string) error
	// FindGit locates git without running it.
	FindGit func() (string, error)
	// GitVersion runs the fixed `git --version` and returns its first line.
	GitVersion func(ctx context.Context, gitPath string) (string, error)
	// FreeBytes is the space available to an unprivileged user on the filesystem of path.
	FreeBytes func(path string) (uint64, error)
	// Validator is nil until the real validator is wired (T034).
	Validator Validator
}

var errNoPort = errors.New("measurement unavailable")

func (p Ports) withDefaults() Ports {
	if p.Getenv == nil {
		p.Getenv = func(string) string { return "" }
	}
	if p.Home == nil {
		p.Home = func() (string, error) { return "", errNoPort }
	}
	if p.User == nil {
		p.User = func() string { return "" }
	}
	if p.Host == nil {
		p.Host = func() string { return "" }
	}
	if p.Getwd == nil {
		p.Getwd = func() (string, error) { return "", errNoPort }
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Lstat == nil {
		p.Lstat = func(string) (fs.FileInfo, error) { return nil, errNoPort }
	}
	if p.DialUnix == nil {
		p.DialUnix = func(context.Context, string) error { return errNoPort }
	}
	if p.ReadFile == nil {
		p.ReadFile = func(string, int64) ([]byte, error) { return nil, errNoPort }
	}
	if p.CanRead == nil {
		p.CanRead = func(string) error { return errNoPort }
	}
	if p.FindGit == nil {
		p.FindGit = func() (string, error) { return "", errNoPort }
	}
	if p.GitVersion == nil {
		p.GitVersion = func(context.Context, string) (string, error) { return "", errNoPort }
	}
	if p.FreeBytes == nil {
		p.FreeBytes = func(string) (uint64, error) { return 0, errNoPort }
	}
	return p
}

// SystemPorts returns the ports of this machine. validator may be nil.
func SystemPorts(validator Validator) Ports {
	return Ports{
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		Getenv: os.Getenv,
		Home:   os.UserHomeDir,
		User: func() string {
			if u, err := user.Current(); err == nil {
				return u.Username
			}
			return ""
		},
		Host:  func() string { h, _ := os.Hostname(); return h },
		Getwd: os.Getwd,
		Now:   time.Now,
		Lstat: os.Lstat,
		DialUnix: func(ctx context.Context, path string) error {
			var d net.Dialer
			c, err := d.DialContext(ctx, "unix", path)
			if err != nil {
				return err
			}
			return c.Close()
		},
		ReadFile:   readBounded,
		CanRead:    canList,
		FindGit:    findGit,
		GitVersion: runGitVersion,
		FreeBytes:  freeBytes,
		Validator:  validator,
	}
}

// readBounded reads a regular file without following a final link and never
// blocks on a FIFO or device.
func readBounded(path string, max int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("file exceeds the read bound")
	}
	return b, nil
}

func canList(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
