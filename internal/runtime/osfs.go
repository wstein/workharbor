package runtime

import (
	"io/fs"
	"os"
	"path/filepath"
)

// OSFS is the real filesystem.
type OSFS struct{}

// EvalSymlinks implements FS.
func (OSFS) EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }

// Stat implements FS.
func (OSFS) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }

// SameFile implements FS.
func (OSFS) SameFile(a, b fs.FileInfo) bool { return os.SameFile(a, b) }

// ReadDir implements FS.
func (OSFS) ReadDir(path string) ([]fs.DirEntry, error) { return os.ReadDir(path) }
