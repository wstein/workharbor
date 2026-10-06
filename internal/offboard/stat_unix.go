//go:build unix

package offboard

import (
	"os"
	"syscall"
)

// OSStat is os.Lstat with the owner of the path.
func OSStat(path string) (HomeInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return HomeInfo{}, err
	}
	h := HomeInfo{Symlink: fi.Mode()&os.ModeSymlink != 0, Dir: fi.IsDir()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		h.UID, h.UIDKnown = int(st.Uid), true
	}
	return h, nil
}
