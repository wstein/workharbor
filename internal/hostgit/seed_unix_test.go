//go:build unix

package hostgit

import (
	"os"
	"syscall"
)

func linkCount(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink) //nolint:unconvert // Nlink is uint16 on darwin and uint64 on linux
	}
	return 1
}
