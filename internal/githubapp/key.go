package githubapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// KeyFileName is the name the App's private key is stored under.
func KeyFileName(appID int64) string { return "github-app-" + strconv.FormatInt(appID, 10) + ".pem" }

// WriteKey writes the key straight to a 0600 file in dir with an exclusive
// create: it never overwrites an existing file (a second App would otherwise
// replace the first one's key), and it does not follow a symbolic link. The
// directory is made 0700 if it does not exist. It returns the path.
func WriteKey(dir string, appID int64, pem []byte) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("the key directory %q must be an absolute path", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, KeyFileName(appID))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // the operator names the directory; the name is built from a number
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%s already exists: not overwritten", path)
		}
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(pem); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}
