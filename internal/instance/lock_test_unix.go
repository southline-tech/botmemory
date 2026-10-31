//go:build !windows

package instance

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

// R4-2: a FIFO at the lock path is refused without blocking.
func TestLockRefusesFIFO(t *testing.T) {
	p := t.TempDir() + "/fifo.lock"
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		l, err := Acquire(p)
		if l != nil {
			l.Release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotLockFile) {
			t.Fatalf("FIFO accepted: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire hung on a FIFO")
	}
}
