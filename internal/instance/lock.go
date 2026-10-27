// Package instance enforces the spec's single-instance rule with an
// exclusive, non-blocking flock on a lock file. The kernel releases it when
// the process exits, so a crash never leaves a stale lock.
package instance

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var ErrRunning = errors.New("another botvault instance holds the lock")

// ErrNotLockFile: the lock path names an existing, non-empty file whose
// contents are not a botvault lock (a PID line). It is never truncated.
var ErrNotLockFile = errors.New("refusing to use an existing non-lock file as a lock")

// ErrUnsafeDir: a lock directory is not a real directory owned by us with
// no group/other permissions.
var ErrUnsafeDir = errors.New("unsafe lock directory")

var pidLine = regexp.MustCompile(`^[0-9]{1,10}\n?$`)

// SecureDir creates dir (mode 0700) if missing and checks that it is a real
// directory (not a symlink), owned by the current user, with no group or
// other permission bits.
func SecureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("lock dir %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("lock dir %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeDir, dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%w: %s is not owned by uid %d", ErrUnsafeDir, dir, os.Getuid())
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s has mode %v (want 0700)", ErrUnsafeDir, dir, fi.Mode().Perm())
	}
	return nil
}

// UserDir is the per-user botvault lock directory: $XDG_RUNTIME_DIR/botvault
// if set, else os.UserCacheDir()/botvault. It is created 0700 and checked by
// SecureDir.
func UserDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("no per-user lock dir: %w", err)
		}
		base = c
	}
	d := filepath.Join(base, "botvault")
	if err := SecureDir(d); err != nil {
		return "", err
	}
	return d, nil
}

type Lock struct{ f *os.File }

func Acquire(path string) (*Lock, error) {
	// O_NOFOLLOW: a planted symlink must not redirect our write to another
	// file. No O_TRUNC: an existing file is only rewritten once it is known
	// to be a regular file that is empty or holds a PID line.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock file %s: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrNotLockFile, path)
	}
	if fi.Size() > 0 {
		buf, err := io.ReadAll(io.LimitReader(f, 64))
		if err != nil || fi.Size() > 64 || !pidLine.Match(buf) {
			f.Close()
			return nil, fmt.Errorf("%w: %s", ErrNotLockFile, path)
		}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s)", ErrRunning, path)
		}
		return nil, err
	}
	f.Truncate(0)
	f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return &Lock{f}, nil
}

func (l *Lock) Release() error {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}
