//go:build unix

package protocol

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes the run's exclusive lock without waiting. The lock belongs to
// the open file description, so a second Open of the same file conflicts even
// inside one process, and it goes away when the descriptor closes or the
// process dies.
func tryLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // a file descriptor fits an int
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrConflict
	}
	return err
}

func unlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // a file descriptor fits an int
}
