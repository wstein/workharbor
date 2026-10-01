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
