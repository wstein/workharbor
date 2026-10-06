//go:build !unix

package offboard

import "os"

// OSStat is os.Lstat without an owner, which this system cannot say.
func OSStat(path string) (HomeInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return HomeInfo{}, err
	}
	return HomeInfo{Symlink: fi.Mode()&os.ModeSymlink != 0, Dir: fi.IsDir()}, nil
}
