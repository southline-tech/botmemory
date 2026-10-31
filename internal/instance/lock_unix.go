//go:build !windows

package instance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func fileLock(f *os.File, exclusive bool) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return fmt.Errorf("%w (%s)", ErrRunning, f.Name())
	}
	return err
}

func fileUnlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func dirOwnedByUs(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
