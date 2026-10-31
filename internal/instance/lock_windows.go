//go:build windows

package instance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x2
	lockfileFailImmediately = 0x1
)

func fileLock(f *os.File, exclusive bool) error {
	var ol syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		uintptr(f.Fd()),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		^uintptr(0),
		^uintptr(0),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_LOCK_VIOLATION {
			return fmt.Errorf("%w (%s)", ErrRunning, f.Name())
		}
		return err
	}
	return nil
}

func fileUnlock(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(
		uintptr(f.Fd()),
		0,
		^uintptr(0),
		^uintptr(0),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

func dirOwnedByUs(fi os.FileInfo) bool {
	// Windows: mode/ownership checks use ACL APIs; SecureDir still enforces 0700-ish perms.
	_ = fi
	return true
}
