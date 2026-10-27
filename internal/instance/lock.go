// Package instance enforces the spec's single-instance rule with an
// exclusive, non-blocking flock on a lock file. The kernel releases it when
// the process exits, so a crash never leaves a stale lock.
package instance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrRunning = errors.New("another botvault instance holds the lock")

type Lock struct{ f *os.File }

func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s)", ErrRunning, path)
		}
		return nil, err
	}
	f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return &Lock{f}, nil
}

func (l *Lock) Release() error {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}
